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

// Ids as Plat5 has them: lowercase ULIDs.
const (
	orgID           = "01jbx9q3t5vk8m2n4p6r7s8t9w"
	projectID       = "01jbx9r2c4dk6m8n0p2q4r6s8t"
	envID           = "01jbx9y2m4n6p8q0r2s4t6v8w0"
	orphanID        = "01jbx9z5x7y9z1a3b5c7d9e1f3"
	missingParentID = "01jbxa0g2h4j6k8m0n2p4q6r8s"
	liarID          = "01jbxa1t3v5w7x9y1z3a5b7c9d"
	otherID         = "01jbxa2e4f6g8h0j2k4m6n8p0q"
	missingID       = "01jbx9v3f5g7h9j1k3m5n7p9q1"
	loopAID         = "01jbxa3q5r7s9t1v3w5x7y9z1a"
	loopBID         = "01jbxa4b3c5d7e9f1g3h5j7k9m"
	danaID          = "01jbyb0d1e2f3g4h5j6k7m8n9p"
	ciID            = "01jbyb1q2r3s4t5v6w7x8y9z0a"
	goneID          = "01jbyb2a3b4c5d6e7f8g9h0j1k"
	nobodyID        = "01jbyb4y5z6a7b8c9d0e1f2g3h"
	strangeID       = "01jbyb6w7x8y9z0a1b2c3d4e5f"
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
	return map[string]any{"id": id, "kind": kind, "parent_id": parent, "root_id": orgID, "status": "active"}
}

func newFake() *fake {
	return &fake{
		nodes: map[string]map[string]any{
			orgID:     node(orgID, "organization", nil),
			projectID: node(projectID, "project", orgID),
			envID:     node(envID, "environment", projectID),
			orphanID:  node(orphanID, "project", missingParentID),
			liarID:    node(otherID, "project", nil),
		},
		members: map[string]map[string]any{
			orgID + "/" + danaID:    {"id": danaID, "principal": "person", "status": "active"},
			orgID + "/" + ciID:      {"id": ciID, "principal": "service_account", "status": "active"},
			orgID + "/" + goneID:    {"id": goneID, "principal": "person", "status": "removed"},
			orgID + "/" + strangeID: {"id": strangeID, "principal": "operator", "status": "active"},
		},
	}
}

func TestChain(t *testing.T) {
	f := newFake()
	srv := httptest.NewServer(f)
	defer srv.Close()
	c := New(srv.URL+"/", nil)
	ctx := context.Background()

	chain, err := c.Chain(ctx, envID, "req-1")
	if err != nil {
		t.Fatal(err)
	}
	want := []Node{{"organization", orgID}, {"project", projectID}, {"environment", envID}}
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
	if _, err := c.Chain(ctx, projectID, "req-1"); err != nil {
		t.Fatal(err)
	}
	if f.calls.Load() != calls {
		t.Fatalf("chain of a cached node called identity %d times", f.calls.Load()-calls)
	}

	if _, err := c.Chain(ctx, missingID, "req-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing node: %v", err)
	}
	if _, err := c.Chain(ctx, orphanID, "req-1"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("missing ancestor: %v", err)
	}
	if _, err := c.Chain(ctx, liarID, "req-1"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("answer for another node: %v", err)
	}
	// Not-found isn't cached.
	f.nodes[missingID] = node(missingID, "organization", nil)
	if _, err := c.Chain(ctx, missingID, "req-1"); err != nil {
		t.Fatalf("created after a miss: %v", err)
	}
}

func TestChainLoop(t *testing.T) {
	f := newFake()
	f.nodes[loopAID] = node(loopAID, "folder", loopBID)
	f.nodes[loopBID] = node(loopBID, "folder", loopAID)
	srv := httptest.NewServer(f)
	defer srv.Close()
	if _, err := New(srv.URL, nil).Chain(context.Background(), loopAID, "req-1"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("loop: %v", err)
	}
}

func TestMember(t *testing.T) {
	f := newFake()
	srv := httptest.NewServer(f)
	defer srv.Close()
	c := New(srv.URL, nil)
	ctx := context.Background()

	for member, want := range map[string]string{danaID: "person", ciID: "service_account"} {
		got, err := c.Member(ctx, orgID, member, "req-1")
		if err != nil || got != want {
			t.Fatalf("%s: %q, %v", member, got, err)
		}
	}
	for _, member := range []string{goneID, nobodyID} {
		if _, err := c.Member(ctx, orgID, member, "req-1"); !errors.Is(err, ErrNotActive) {
			t.Fatalf("%s: %v", member, err)
		}
	}
	if _, err := c.Member(ctx, orgID, strangeID, "req-1"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("unknown principal: %v", err)
	}
	// Not cached: removal is seen at once.
	f.members[orgID+"/"+danaID]["status"] = "removed"
	if _, err := c.Member(ctx, orgID, danaID, "req-1"); !errors.Is(err, ErrNotActive) {
		t.Fatalf("removed: %v", err)
	}
}

func TestUnavailable(t *testing.T) {
	f := newFake()
	f.status = http.StatusInternalServerError
	srv := httptest.NewServer(f)
	c := New(srv.URL, nil)
	ctx := context.Background()
	if _, err := c.Chain(ctx, orgID, "req-1"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("500: %v", err)
	}
	if _, err := c.Member(ctx, orgID, danaID, "req-1"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("500: %v", err)
	}
	srv.Close()
	if _, err := c.Chain(ctx, orgID, "req-1"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("down: %v", err)
	}
}
