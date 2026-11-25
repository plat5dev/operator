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

	"github.com/plat5dev/operator/internal/httpx"
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
		httpClient = &http.Client{Transport: httpx.NewTransport(0)}
	}
	return &Client{base: strings.TrimRight(baseURL, "/"), http: httpClient, nodes: map[string]place{}}
}

// Chain is the node and its ancestors, root first. ErrNotFound means the node itself
// isn't there. Cached nodes are walked in memory; a cold node fetches its full chain
// from GET /nodes/{id}'s ancestors in one call and caches every node in it.
func (c *Client) Chain(ctx context.Context, nodeID, requestID string) ([]Node, error) {
	if chain, ok := c.cachedChain(nodeID); ok {
		return chain, nil
	}

	var node struct {
		ID        string  `json:"id"`
		Kind      string  `json:"kind"`
		ParentID  *string `json:"parent_id"`
		Ancestors []struct {
			ID   string `json:"id"`
			Kind string `json:"kind"`
		} `json:"ancestors"`
	}
	status, err := c.get(ctx, "/nodes/"+url.PathEscape(nodeID), requestID, &node)
	switch {
	case err != nil:
		return nil, err
	case status == http.StatusNotFound:
		return nil, ErrNotFound
	case status != http.StatusOK:
		return nil, fmt.Errorf("%w: GET /nodes/%s answered %d", ErrUnavailable, nodeID, status)
	case node.ID != nodeID || !kindRe.MatchString(node.Kind):
		return nil, fmt.Errorf("%w: GET /nodes/%s answered a node that can't be", ErrUnavailable, nodeID)
	case node.ParentID == nil && len(node.Ancestors) != 0:
		return nil, fmt.Errorf("%w: GET /nodes/%s root node has ancestors", ErrUnavailable, nodeID)
	case node.ParentID != nil && (*node.ParentID == "" || len(node.Ancestors) == 0 || node.Ancestors[len(node.Ancestors)-1].ID != *node.ParentID):
		return nil, fmt.Errorf("%w: GET /nodes/%s answered inconsistent ancestors", ErrUnavailable, nodeID)
	case len(node.Ancestors)+1 > maxDepth:
		return nil, fmt.Errorf("%w: chain of %s deeper than %d", ErrUnavailable, nodeID, maxDepth)
	}

	chain := make([]Node, 0, len(node.Ancestors)+1)
	seen := make(map[string]struct{}, len(node.Ancestors)+1)
	for _, a := range node.Ancestors {
		if a.ID == "" || !kindRe.MatchString(a.Kind) {
			return nil, fmt.Errorf("%w: GET /nodes/%s answered an ancestor that can't be", ErrUnavailable, nodeID)
		}
		if _, dup := seen[a.ID]; dup {
			return nil, fmt.Errorf("%w: GET /nodes/%s answered duplicate ancestor %s", ErrUnavailable, nodeID, a.ID)
		}
		seen[a.ID] = struct{}{}
		chain = append(chain, Node{Kind: a.Kind, ID: a.ID})
	}
	if _, dup := seen[node.ID]; dup {
		return nil, fmt.Errorf("%w: GET /nodes/%s lists itself as an ancestor", ErrUnavailable, nodeID)
	}
	chain = append(chain, Node{Kind: node.Kind, ID: node.ID})

	c.cacheChain(chain)
	return chain, nil
}

func (c *Client) cachedChain(nodeID string) ([]Node, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var chain []Node
	for id := nodeID; id != ""; {
		if len(chain) == maxDepth {
			return nil, false
		}
		p, ok := c.nodes[id]
		if !ok {
			return nil, false
		}
		chain = append(chain, Node{Kind: p.kind, ID: id})
		id = p.parent
	}
	slices.Reverse(chain)
	return chain, true
}

func (c *Client) cacheChain(chain []Node) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.nodes)+len(chain) > cacheLimit {
		clear(c.nodes)
	}
	parent := ""
	for _, n := range chain {
		c.nodes[n.ID] = place{kind: n.Kind, parent: parent}
		parent = n.ID
	}
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
	case status == http.StatusNotFound:
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
