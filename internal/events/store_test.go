package events

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/plat5dev/operator/internal/db"
	"github.com/plat5dev/operator/internal/db/dbtest"
)

// writerStore is a migrated database used as the writer role, with no partitions yet.
func writerStore(t *testing.T) *Store {
	t.Helper()
	d := dbtest.New(t)
	ctx := context.Background()
	owner, err := db.Connect(ctx, d.OwnerURL)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	if err := db.Migrate(ctx, owner, d.WriterRole); err != nil {
		t.Fatal(err)
	}
	writer, err := db.Connect(ctx, d.WriterURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(writer.Close)
	return NewStore(writer)
}

func matched(requestID, operator, organization string, at time.Time) *Intent {
	up, route, email := "identity", "/organizations/{organization_id}/members", "a@x.test"
	return &Intent{
		RequestID:  requestID,
		OccurredAt: at.UTC().Truncate(time.Millisecond),
		Actor:      Actor{Issuer: "https://idp.test", OperatorID: operator, Email: &email},
		Upstream:   &up,
		Method:     "GET",
		Route:      &route,
		Params:     map[string]string{"organization_id": organization},
		IP:         "10.0.0.1",
	}
}

func TestStoreWrites(t *testing.T) {
	s := writerStore(t)
	ctx := context.Background()
	at := time.Now()

	// No partition exists yet: the first intent makes its month.
	created, err := s.CreateIntent(ctx, matched("r1", "op_1", "org_1", at))
	if err != nil || !created {
		t.Fatalf("created %v err %v", created, err)
	}
	if created, err := s.CreateIntent(ctx, matched("r1", "op_2", "org_2", at.Add(time.Second))); err != nil || created {
		t.Fatalf("retried intent: created %v err %v", created, err)
	}
	path := "/nope"
	unmatched := &Intent{RequestID: "r2", OccurredAt: at.UTC().Truncate(time.Millisecond),
		Actor: Actor{Issuer: "https://idp.test", OperatorID: "op_1"}, Method: "GET",
		Params: map[string]string{}, Path: &path, IP: "10.0.0.1"}
	if _, err := s.CreateIntent(ctx, unmatched); err != nil {
		t.Fatal(err)
	}

	status := 200
	applied, err := s.ApplyOutcome(ctx, "r1", &Outcome{Outcome: OutcomeResponded, Status: &status,
		ResponseBytes: 42, Details: json.RawMessage(`{"role":{"from":"a","to":"b"}}`)})
	if err != nil || !applied {
		t.Fatalf("applied %v err %v", applied, err)
	}
	if applied, err := s.ApplyOutcome(ctx, "r1", &Outcome{Outcome: OutcomeRejected, Status: &status}); err != nil || applied {
		t.Fatalf("second outcome: applied %v err %v", applied, err)
	}
	if _, err := s.ApplyOutcome(ctx, "r9", &Outcome{Outcome: OutcomeRejected, Status: &status}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("no intent: %v", err)
	}

	list, _, err := s.List(ctx, Filter{Limit: 10, RequestID: "r1"})
	if err != nil || len(list) != 1 {
		t.Fatalf("%d events, err %v", len(list), err)
	}
	e := list[0]
	if e.Actor.OperatorID != "op_1" || e.Outcome != OutcomeResponded || *e.Status != 200 || *e.ResponseBytes != 42 ||
		string(e.Details) != `{"role": {"to": "b", "from": "a"}}` || !e.OccurredAt.Equal(at.UTC().Truncate(time.Millisecond)) ||
		*e.Actor.Email != "a@x.test" || e.Actor.ClientID != nil || e.Path != nil {
		t.Fatalf("%+v details %s", e, e.Details)
	}
	if !idTime(e.ID).Equal(e.OccurredAt) {
		t.Fatalf("id time %v, occurred_at %v", idTime(e.ID), e.OccurredAt)
	}

	list, _, _ = s.List(ctx, Filter{Limit: 10, RequestID: "r2"})
	if len(list) != 1 || *list[0].Path != "/nope" || list[0].Route != nil || list[0].Outcome != OutcomePending ||
		list[0].Status != nil || list[0].ResponseBytes != nil {
		t.Fatalf("%+v", list[0])
	}
}

func TestStoreList(t *testing.T) {
	s := writerStore(t)
	ctx := context.Background()
	base := time.Now().Add(-time.Hour).UTC().Truncate(time.Millisecond)
	for i := range 5 {
		op, org := "op_1", fmt.Sprintf("org_%d", i%2)
		if i == 4 {
			op = "op_2"
		}
		if _, err := s.CreateIntent(ctx, matched(fmt.Sprintf("r%d", i), op, org, base.Add(time.Duration(i)*time.Minute))); err != nil {
			t.Fatal(err)
		}
	}
	ids := func(f Filter) ([]string, bool) {
		t.Helper()
		if f.Limit == 0 {
			f.Limit = 10
		}
		list, more, err := s.List(ctx, f)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, e := range list {
			out = append(out, e.RequestID)
		}
		return out, more
	}
	eq := func(got []string, want ...string) bool { return fmt.Sprint(got) == fmt.Sprint(want) }

	if got, _ := ids(Filter{}); !eq(got, "r4", "r3", "r2", "r1", "r0") {
		t.Fatalf("newest first: %v", got)
	}
	page, more := ids(Filter{Limit: 2})
	if !eq(page, "r4", "r3") || !more {
		t.Fatalf("page 1: %v more %v", page, more)
	}
	first, _, _ := s.List(ctx, Filter{Limit: 2})
	if got, more := ids(Filter{Limit: 2, StartingAfter: first[1].ID}); !eq(got, "r2", "r1") || !more {
		t.Fatalf("page 2: %v more %v", got, more)
	}
	if got, _ := ids(Filter{OperatorID: "op_2"}); !eq(got, "r4") {
		t.Fatalf("operator: %v", got)
	}
	if got, _ := ids(Filter{Params: map[string]string{"organization_id": "org_1"}}); !eq(got, "r3", "r1") {
		t.Fatalf("params: %v", got)
	}
	after, before := base.Add(time.Minute), base.Add(3*time.Minute)
	if got, _ := ids(Filter{OccurredAfter: &after, OccurredBefore: &before}); !eq(got, "r2", "r1") {
		t.Fatalf("time window: %v", got)
	}
	if got, _ := ids(Filter{Upstream: "identity", Method: "GET", Route: "/organizations/{organization_id}/members", Outcome: OutcomePending}); len(got) != 5 {
		t.Fatalf("exact filters: %v", got)
	}
	if got, _ := ids(Filter{Method: "POST"}); len(got) != 0 {
		t.Fatalf("method: %v", got)
	}
}

func TestStorePartitionsAndStale(t *testing.T) {
	s := writerStore(t)
	ctx := context.Background()
	now := time.Now()
	for range 2 {
		if err := s.EnsureAround(ctx, now); err != nil {
			t.Fatal(err)
		}
	}
	for i, age := range []time.Duration{time.Minute, 20 * time.Minute, 2 * time.Hour, 30 * time.Hour} {
		if _, err := s.CreateIntent(ctx, matched(fmt.Sprintf("p%d", i), "op_1", "org_1", now.Add(-age))); err != nil {
			t.Fatal(err)
		}
	}
	status := 200
	if _, err := s.ApplyOutcome(ctx, "p2", &Outcome{Outcome: OutcomeResponded, Status: &status}); err != nil {
		t.Fatal(err)
	}
	// p0 is too young to be stale, p2 has its outcome, p3 is past the 24-hour window.
	n, err := s.StalePending(ctx, now)
	if err != nil || n != 1 {
		t.Fatalf("stale %d err %v", n, err)
	}
}
