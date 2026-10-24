package identity

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// fake serves GET /nodes/{id} and GET /nodes/{root}/members/{member} from maps.
type fake struct {
	nodes   map[string]map[string]any
	members map[string]map[string]any // root/member → member
	calls   atomic.Int64
	status  int // when set, every answer
}

func (f *fake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.calls.Add(1)
	if r.Header.Get("X-Request-ID") != "req-1" {
		http.Error(w, "no request id", http.StatusTeapot)
		return
	}
	if f.status != 0 {
		w.WriteHeader(f.status)
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/nodes/"), "/")
	var v map[string]any
	switch len(parts) {
	case 1:
		v = f.nodes[parts[0]]
	case 3:
		v = f.members[parts[0]+"/"+parts[2]]
	}
	if v == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	_ = json.NewEncoder(w).Encode(v)
}

func node(id, kind string, parent any) map[string]any {
	return map[string]any{"id": id, "kind": kind, "parent_id": parent, "root_id": "n_org", "status": "active"}
}

func newFake() *fake {
	return &fake{
		nodes: map[string]map[string]any{
			"n_org":    node("n_org", "organization", nil),
			"n_proj":   node("n_proj", "project", "n_org"),
			"n_env":    node("n_env", "environment", "n_proj"),
			"n_orphan": node("n_orphan", "project", "n_gone"),
			"n_liar":   node("n_other", "project", nil),
		},
		members: map[string]map[string]any{
			"n_org/m_dana":    {"id": "m_dana", "principal": "person", "status": "active"},
			"n_org/m_ci":      {"id": "m_ci", "principal": "service_account", "status": "active"},
			"n_org/m_gone":    {"id": "m_gone", "principal": "person", "status": "removed"},
			"n_org/m_strange": {"id": "m_strange", "principal": "operator", "status": "active"},
		},
	}
}

func TestChain(t *testing.T) {
	f := newFake()
	srv := httptest.NewServer(f)
	defer srv.Close()
	c := New(srv.URL+"/", nil)
	ctx := context.Background()

	chain, err := c.Chain(ctx, "n_env", "req-1")
	if err != nil {
		t.Fatal(err)
	}
	want := []Node{{"organization", "n_org"}, {"project", "n_proj"}, {"environment", "n_env"}}
	if len(chain) != len(want) {
		t.Fatalf("chain = %v", chain)
	}
	for i := range want {
		if chain[i] != want[i] {
			t.Fatalf("chain = %v, want %v", chain, want)
		}
	}

	// Cached: nodes never move.
	calls := f.calls.Load()
	if _, err := c.Chain(ctx, "n_proj", "req-1"); err != nil {
		t.Fatal(err)
	}
	if f.calls.Load() != calls {
		t.Fatalf("chain of a cached node called identity %d times", f.calls.Load()-calls)
	}

	if _, err := c.Chain(ctx, "n_missing", "req-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing node: %v", err)
	}
	if _, err := c.Chain(ctx, "n_orphan", "req-1"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("missing ancestor: %v", err)
	}
	if _, err := c.Chain(ctx, "n_liar", "req-1"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("answer for another node: %v", err)
	}
	// Not-found isn't cached.
	f.nodes["n_missing"] = node("n_missing", "organization", nil)
	if _, err := c.Chain(ctx, "n_missing", "req-1"); err != nil {
		t.Fatalf("created after a miss: %v", err)
	}
}

func TestChainLoop(t *testing.T) {
	f := newFake()
	f.nodes["n_a"] = node("n_a", "folder", "n_b")
	f.nodes["n_b"] = node("n_b", "folder", "n_a")
	srv := httptest.NewServer(f)
	defer srv.Close()
	if _, err := New(srv.URL, nil).Chain(context.Background(), "n_a", "req-1"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("loop: %v", err)
	}
}

func TestMember(t *testing.T) {
	f := newFake()
	srv := httptest.NewServer(f)
	defer srv.Close()
	c := New(srv.URL, nil)
	ctx := context.Background()

	for member, want := range map[string]string{"m_dana": "person", "m_ci": "service_account"} {
		got, err := c.Member(ctx, "n_org", member, "req-1")
		if err != nil || got != want {
			t.Fatalf("%s: %q, %v", member, got, err)
		}
	}
	for _, member := range []string{"m_gone", "m_nobody"} {
		if _, err := c.Member(ctx, "n_org", member, "req-1"); !errors.Is(err, ErrNotActive) {
			t.Fatalf("%s: %v", member, err)
		}
	}
	if _, err := c.Member(ctx, "n_org", "m_strange", "req-1"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("unknown principal: %v", err)
	}
	// Not cached: removal is seen at once.
	f.members["n_org/m_dana"]["status"] = "removed"
	if _, err := c.Member(ctx, "n_org", "m_dana", "req-1"); !errors.Is(err, ErrNotActive) {
		t.Fatalf("removed: %v", err)
	}
}

func TestUnavailable(t *testing.T) {
	f := newFake()
	f.status = http.StatusInternalServerError
	srv := httptest.NewServer(f)
	c := New(srv.URL, nil)
	ctx := context.Background()
	if _, err := c.Chain(ctx, "n_org", "req-1"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("500: %v", err)
	}
	if _, err := c.Member(ctx, "n_org", "m_dana", "req-1"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("500: %v", err)
	}
	srv.Close()
	if _, err := c.Chain(ctx, "n_org", "req-1"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("down: %v", err)
	}
}
