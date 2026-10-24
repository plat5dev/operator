// Package identity is the gateway's client of Plat5 identity's public service API: a
// node's chain, and a member in a tree. Contract: docs/reference/gateway.md#identity.
package identity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

var (
	// ErrNotFound is a route node identity doesn't have.
	ErrNotFound = errors.New("node not found")
	// ErrNotActive is a member that isn't active in the tree, or isn't a member at all.
	ErrNotActive = errors.New("member not active in the tree")
	// ErrUnavailable is identity failing: unreachable, slow, or an answer that can't be.
	ErrUnavailable = errors.New("identity unavailable")
)

const (
	callTimeout = 2 * time.Second
	// A full cache is cleared and refilled.
	cacheLimit = 100_000
	// Deeper than any tree of kinds; a longer walk is a loop.
	maxDepth = 32
	maxBody  = 64 << 10
)

// A kind name becomes a header name (X-Plat5-<Kind>-Id), so nothing else is accepted.
var kindRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

// Node is one node of a chain.
type Node struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

// place is what never changes about a node: its kind and parent.
type place struct {
	kind, parent string
}

type Client struct {
	base string
	http *http.Client

	mu    sync.RWMutex
	nodes map[string]place
}

// New is a client of identity's public base URL. httpClient is for tests; nil is a plain one.
func New(baseURL string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	return &Client{base: strings.TrimRight(baseURL, "/"), http: httpClient, nodes: map[string]place{}}
}

// Chain is the node and its ancestors, root first. ErrNotFound means the node itself
// isn't there; a missing ancestor is identity failing.
func (c *Client) Chain(ctx context.Context, nodeID, requestID string) ([]Node, error) {
	var chain []Node
	for id := nodeID; id != ""; {
		if len(chain) == maxDepth {
			return nil, fmt.Errorf("%w: chain of %s deeper than %d", ErrUnavailable, nodeID, maxDepth)
		}
		p, err := c.place(ctx, id, requestID)
		if errors.Is(err, ErrNotFound) && id != nodeID {
			return nil, fmt.Errorf("%w: ancestor %s of %s not found", ErrUnavailable, id, nodeID)
		}
		if err != nil {
			return nil, err
		}
		chain = append(chain, Node{Kind: p.kind, ID: id})
		id = p.parent
	}
	slices.Reverse(chain)
	return chain, nil
}

func (c *Client) place(ctx context.Context, id, requestID string) (place, error) {
	c.mu.RLock()
	p, ok := c.nodes[id]
	c.mu.RUnlock()
	if ok {
		return p, nil
	}

	var node struct {
		ID       string  `json:"id"`
		Kind     string  `json:"kind"`
		ParentID *string `json:"parent_id"`
	}
	status, err := c.get(ctx, "/nodes/"+url.PathEscape(id), requestID, &node)
	switch {
	case err != nil:
		return place{}, err
	case status == http.StatusNotFound, status == http.StatusBadRequest, status == http.StatusUnprocessableEntity:
		return place{}, ErrNotFound
	case status != http.StatusOK:
		return place{}, fmt.Errorf("%w: GET /nodes/%s answered %d", ErrUnavailable, id, status)
	case node.ID != id || !kindRe.MatchString(node.Kind) || (node.ParentID != nil && *node.ParentID == ""):
		return place{}, fmt.Errorf("%w: GET /nodes/%s answered a node that can't be", ErrUnavailable, id)
	}
	p = place{kind: node.Kind}
	if node.ParentID != nil {
		p.parent = *node.ParentID
	}

	c.mu.Lock()
	if len(c.nodes) >= cacheLimit {
		clear(c.nodes)
	}
	c.nodes[id] = p
	c.mu.Unlock()
	return p, nil
}

// Member is the principal of a member active in the tree rooted at rootID: person or
// service_account. Not cached: a removed member is refused on the next request.
func (c *Client) Member(ctx context.Context, rootID, memberID, requestID string) (string, error) {
	var m struct {
		ID        string `json:"id"`
		Principal string `json:"principal"`
		Status    string `json:"status"`
	}
	status, err := c.get(ctx, "/nodes/"+url.PathEscape(rootID)+"/members/"+url.PathEscape(memberID), requestID, &m)
	switch {
	case err != nil:
		return "", err
	case status == http.StatusNotFound, status == http.StatusBadRequest, status == http.StatusUnprocessableEntity:
		return "", ErrNotActive
	case status != http.StatusOK:
		return "", fmt.Errorf("%w: member lookup answered %d", ErrUnavailable, status)
	case m.ID != memberID || (m.Principal != "person" && m.Principal != "service_account"):
		return "", fmt.Errorf("%w: member lookup answered a member that can't be", ErrUnavailable)
	case m.Status != "active":
		return "", ErrNotActive
	}
	return m.Principal, nil
}

// get decodes a 200 body into dst and returns the status. Err is identity unreachable.
func (c *Client) get(ctx context.Context, path, requestID string, dst any) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return 0, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Request-ID", requestID)
	res, err := c.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, maxBody))
		return res.StatusCode, nil
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, maxBody)).Decode(dst); err != nil {
		return 0, fmt.Errorf("%w: GET %s: %v", ErrUnavailable, path, err)
	}
	return res.StatusCode, nil
}
