package routes

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/plat5dev/operator/internal/httpx"
)

// ControlPlaneSnapshot is the latest service + upstream-route state pulled from
// Plat5 control plane's GET /internal/gateway feed.
type ControlPlaneSnapshot struct {
	Revision int64
	Services map[string]*DiscoveredService
}

// DiscoveredService is one service from Plat5 control plane.
type DiscoveredService struct {
	Target  *url.URL
	Timeout time.Duration
	Routes  []*Route
}

// ControlPlanePoller long-polls GET {url}/internal/gateway (without ?replica=, so it
// never registers as a customer gateway replica) and pushes snapshots into onSnapshot.
type ControlPlanePoller struct {
	baseURL    string
	client     *http.Client
	log        *slog.Logger
	onSnapshot func(*ControlPlaneSnapshot)
}

func NewControlPlanePoller(baseURL string, client *http.Client, log *slog.Logger, onSnapshot func(*ControlPlaneSnapshot)) *ControlPlanePoller {
	if client == nil {
		// Long poll waits up to 25s on the server; allow 35s client timeout.
		client = &http.Client{Transport: httpx.NewTransport(0), Timeout: 35 * time.Second}
	}
	return &ControlPlanePoller{
		baseURL:    strings.TrimRight(baseURL, "/"),
		client:     client,
		log:        log,
		onSnapshot: onSnapshot,
	}
}

// Run polls until ctx is cancelled.
func (p *ControlPlanePoller) Run(ctx context.Context) {
	var after int64
	backoff := time.Second
	for {
		if ctx.Err() != nil {
			return
		}
		snap, status, err := p.pollOnce(ctx, after)
		switch {
		case ctx.Err() != nil:
			return
		case err != nil:
			p.log.Warn("control-plane feed poll failed", "err", err, "retry_in", backoff.String())
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			backoff = min(backoff*2, 15*time.Second)
		case status == http.StatusNotModified || status == http.StatusNoContent:
			backoff = time.Second
		case status == http.StatusNotFound:
			// Control plane has no applied revision yet.
			select {
			case <-ctx.Done():
				return
			case <-time.After(2 * time.Second):
			}
		case snap != nil:
			backoff = time.Second
			after = snap.Revision
			p.onSnapshot(snap)
			p.log.Info("control-plane snapshot loaded", "revision", snap.Revision, "services", len(snap.Services))
		}
	}
}

func (p *ControlPlanePoller) pollOnce(ctx context.Context, after int64) (*ControlPlaneSnapshot, int, error) {
	endpoint := p.baseURL + "/internal/gateway"
	if after > 0 {
		endpoint += "?after=" + strconv.FormatInt(after, 10)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", "application/json")
	res, err := p.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNoContent || res.StatusCode == http.StatusNotFound {
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
		return nil, res.StatusCode, nil
	}
	if res.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
		return nil, res.StatusCode, fmt.Errorf("status %d", res.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return nil, res.StatusCode, err
	}
	snap, err := ParseGatewayFeed(raw)
	if err != nil {
		return nil, res.StatusCode, err
	}
	return snap, res.StatusCode, nil
}

type gatewayFeed struct {
	Revision int64                          `json:"revision"`
	Services map[string]*gatewayFeedService `json:"services"`
}

type gatewayFeedService struct {
	URL      string              `json:"url"`
	Timeouts *gatewayFeedTimeout `json:"timeouts"`
	Routes   []gatewayFeedRoute  `json:"routes"`
}

type gatewayFeedTimeout struct {
	Response string `json:"response"`
}

type gatewayFeedRoute struct {
	Method      string          `json:"method"`
	Scope       string          `json:"scope"`
	Upstream    string          `json:"upstream"`
	Permissions json.RawMessage `json:"permissions"`
}

var dottedIDRe = regexp.MustCompile(`^\{([a-z][a-z0-9_]*)\.id\}$`)

// ParseGatewayFeed decodes a Plat5 control-plane GET /internal/gateway JSON body into a
// ControlPlaneSnapshot.
func ParseGatewayFeed(raw []byte) (*ControlPlaneSnapshot, error) {
	var feed gatewayFeed
	if err := json.Unmarshal(raw, &feed); err != nil {
		return nil, fmt.Errorf("parse gateway feed: %w", err)
	}
	snap := &ControlPlaneSnapshot{
		Revision: feed.Revision,
		Services: make(map[string]*DiscoveredService, len(feed.Services)),
	}
	for name, svc := range feed.Services {
		if svc == nil || !nameRe.MatchString(name) {
			continue
		}
		target, err := parseURL(svc.URL)
		if err != nil {
			continue
		}
		var timeout time.Duration
		if svc.Timeouts != nil && svc.Timeouts.Response != "" {
			if d, err := time.ParseDuration(svc.Timeouts.Response); err == nil && d > 0 {
				timeout = d
			}
		}
		ds := &DiscoveredService{Target: target, Timeout: timeout}
		// Deduplicate routes that map to the same (method, normalized upstream template).
		type key struct {
			method string
			path   string
		}
		byKey := map[key]*Route{}
		var keys []key
		for _, gr := range svc.Routes {
			if !allowed[gr.Method] {
				continue
			}
			tpl, nodeParam, ok := normalizeUpstreamTemplate(gr.Upstream, gr.Scope)
			if !ok {
				continue
			}
			perms := parsePermissions(gr.Permissions)
			k := key{method: gr.Method, path: tpl}
			if existing, exists := byKey[k]; exists {
				if existing.Node == "" && nodeParam != "" {
					existing.Node = nodeParam
				}
				existing.Permissions = mergeStrings(existing.Permissions, perms)
				continue
			}
			r := &Route{
				Path:        tpl,
				Methods:     []string{gr.Method},
				Node:        nodeParam,
				Permissions: perms,
			}
			if err := r.compile(); err != nil {
				continue
			}
			byKey[k] = r
			keys = append(keys, k)
		}
		for _, k := range keys {
			ds.Routes = append(ds.Routes, byKey[k])
		}
		snap.Services[name] = ds
	}
	return snap, nil
}

// normalizeUpstreamTemplate converts a Plat5 gateway upstream template such as
// "/projects/{project.id}/plans/{plan_id}" (with scope "project") into an operator route
// template "/projects/{project_id}/plans/{plan_id}" with node param "project_id".
func normalizeUpstreamTemplate(upstream, scope string) (template string, nodeParam string, ok bool) {
	if !strings.HasPrefix(upstream, "/") || upstream == "/" {
		return "", "", false
	}
	parts := strings.Split(upstream[1:], "/")
	out := make([]string, len(parts))
	for i, seg := range parts {
		if m := dottedIDRe.FindStringSubmatch(seg); m != nil {
			kind := m[1]
			param := kind + "_id"
			out[i] = "{" + param + "}"
			if scope != "" && scope != "user" && scope != "public" && kind == scope {
				nodeParam = param
			}
			continue
		}
		out[i] = seg
	}
	return "/" + strings.Join(out, "/"), nodeParam, true
}

func parsePermissions(raw json.RawMessage) []string {
	raw = bytesTrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var list []string
	if err := json.Unmarshal(raw, &list); err == nil {
		return list
	}
	var single string
	if err := json.Unmarshal(raw, &single); err == nil && single != "" {
		return []string{single}
	}
	return nil
}

func bytesTrimSpace(b []byte) []byte {
	return []byte(strings.TrimSpace(string(b)))
}

func mergeStrings(a, b []string) []string {
	if len(b) == 0 {
		return a
	}
	seen := make(map[string]bool, len(a)+len(b))
	out := make([]string, 0, len(a)+len(b))
	for _, s := range a {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	for _, s := range b {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}
