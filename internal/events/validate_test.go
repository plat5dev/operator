package events

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

var now = time.Date(2026, 10, 9, 18, 30, 0, 0, time.UTC)

// intentJSON is a valid matched intent with over applied on top. A nil value deletes the key.
func intentJSON(t *testing.T, over map[string]any) []byte {
	t.Helper()
	m := map[string]any{
		"occurred_at": "2026-10-09T18:30:00.123456Z",
		"actor":       map[string]any{"issuer": "https://idp.test", "operator_id": "op_1", "email": nil, "client_id": "console"},
		"acting_as":   map[string]any{"member_id": "01jbyb0d1e2f3g4h5j6k7m8n9p", "principal": "person"},
		"root_id":     "01jbx9q3t5vk8m2n4p6r7s8t9w",
		"nodes":       []any{map[string]any{"kind": "organization", "id": "01jbx9q3t5vk8m2n4p6r7s8t9w"}, map[string]any{"kind": "project", "id": "01jbx9r2c4dk6m8n0p2q4r6s8t"}},
		"service":     "identity",
		"method":      "POST",
		"route":       "/as/{member}/nodes/{node_id}/children/{kind}",
		"params":      map[string]any{"member": "01jbyb0d1e2f3g4h5j6k7m8n9p", "node_id": "01jbx9r2c4dk6m8n0p2q4r6s8t", "kind": "env"},
		"path":        nil,
		"ip":          "10.0.0.1",
		"user_agent":  "curl/8",
		"future":      "ignored",
	}
	for k, v := range over {
		if v == nil {
			delete(m, k)
			continue
		}
		m[k] = v
	}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func fields(err error) []string {
	var bad Invalid
	if !errors.As(err, &bad) {
		return nil
	}
	return bad
}

func TestParseIntent(t *testing.T) {
	in, err := ParseIntent("req.1_a-b", intentJSON(t, nil), now)
	if err != nil {
		t.Fatal(err)
	}
	if !in.OccurredAt.Equal(time.Date(2026, 10, 9, 18, 30, 0, 123_000_000, time.UTC)) {
		t.Fatalf("occurred_at %v not cut to the millisecond", in.OccurredAt)
	}
	if in.Actor.Email != nil || *in.Actor.ClientID != "console" || *in.Service != "identity" || in.Path != nil ||
		in.Params["node_id"] != "01jbx9r2c4dk6m8n0p2q4r6s8t" || *in.UserAgent != "curl/8" || *in.RootID != "01jbx9q3t5vk8m2n4p6r7s8t9w" ||
		len(in.Nodes) != 2 || in.Nodes[1] != (Node{Kind: "project", ID: "01jbx9r2c4dk6m8n0p2q4r6s8t"}) ||
		*in.ActingAs != (ActingAs{MemberID: "01jbyb0d1e2f3g4h5j6k7m8n9p", Principal: "person"}) {
		t.Fatalf("%+v", in)
	}

	plain, err := ParseIntent("req1", intentJSON(t, map[string]any{
		"acting_as": nil, "root_id": nil, "nodes": []any{}, "route": "/nodes", "params": map[string]any{},
	}), now)
	if err != nil {
		t.Fatal(err)
	}
	if plain.ActingAs != nil || plain.RootID != nil || plain.Nodes == nil || len(plain.Nodes) != 0 {
		t.Fatalf("%+v", plain)
	}

	unmatched, err := ParseIntent("req1", intentJSON(t, map[string]any{
		"acting_as": nil, "root_id": nil, "nodes": []any{},
		"service": nil, "route": nil, "params": map[string]any{}, "path": "/nope/a%2Fb",
	}), now)
	if err != nil {
		t.Fatal(err)
	}
	if unmatched.Route != nil || *unmatched.Path != "/nope/a%2Fb" {
		t.Fatalf("%+v", unmatched)
	}
}

func TestParseIntentInvalid(t *testing.T) {
	cases := []struct {
		name      string
		requestID string
		over      map[string]any
		want      string
	}{
		{"request id", "a b", nil, "request_id"},
		{"no time", "r", map[string]any{"occurred_at": nil}, "occurred_at"},
		{"time skew", "r", map[string]any{"occurred_at": "2026-10-11T18:30:00Z"}, "occurred_at"},
		{"no actor", "r", map[string]any{"actor": nil}, "actor"},
		{"no operator", "r", map[string]any{"actor": map[string]any{"issuer": "i"}}, "actor.operator_id"},
		{"NUL in email", "r", map[string]any{"actor": map[string]any{"issuer": "i", "operator_id": "o", "email": "a\x00b"}}, "actor.email"},
		{"method", "r", map[string]any{"method": "GE T"}, "method"},
		{"service", "r", map[string]any{"service": "Identity"}, "service"},
		{"upstream instead of service", "r", map[string]any{"service": nil, "upstream": "identity"}, "service"},
		{"route without slash", "r", map[string]any{"route": "nodes"}, "route"},
		{"route and path", "r", map[string]any{"path": "/nodes/01jbxa5m7n9p1q3r5s7t9v1w3x"}, "path"},
		{"neither route nor path", "r", map[string]any{"route": nil}, "route"},
		{"path with service", "r", map[string]any{"route": nil, "params": map[string]any{}, "nodes": []any{}, "root_id": nil, "acting_as": nil, "path": "/x"}, "service"},
		{"path with params", "r", map[string]any{"route": nil, "service": nil, "path": "/x"}, "params"},
		{"path with nodes", "r", map[string]any{"route": nil, "service": nil, "params": map[string]any{}, "path": "/x"}, "nodes"},
		{"param name", "r", map[string]any{"params": map[string]any{"Org": "x"}}, "params.Org"},
		{"empty param", "r", map[string]any{"params": map[string]any{"node_id": ""}}, "params.node_id"},
		{"no nodes", "r", map[string]any{"nodes": nil}, "nodes"},
		{"bad kind", "r", map[string]any{"nodes": []any{map[string]any{"kind": "Org", "id": "01jbx9q3t5vk8m2n4p6r7s8t9w"}}}, "nodes.0"},
		{"empty node id", "r", map[string]any{"nodes": []any{map[string]any{"kind": "organization", "id": ""}}, "root_id": ""}, "nodes.0"},
		{"too many nodes", "r", map[string]any{"nodes": manyNodes(maxNodes + 1)}, "nodes"},
		{"root not the head", "r", map[string]any{"root_id": "01jbx9r2c4dk6m8n0p2q4r6s8t"}, "root_id"},
		{"no root", "r", map[string]any{"root_id": nil}, "root_id"},
		{"root without nodes", "r", map[string]any{"nodes": []any{}, "acting_as": nil}, "root_id"},
		{"acting as without nodes", "r", map[string]any{"nodes": []any{}, "root_id": nil}, "acting_as"},
		{"acting as no member", "r", map[string]any{"acting_as": map[string]any{"principal": "person"}}, "acting_as.member_id"},
		{"acting as an operator", "r", map[string]any{"acting_as": map[string]any{"member_id": "01jbyb7g8h9j0k1m2n3p4q5r6s", "principal": "operator"}}, "acting_as.principal"},
		{"no ip", "r", map[string]any{"ip": nil}, "ip"},
		{"user agent too long", "r", map[string]any{"user_agent": strings.Repeat("é", maxUserAgentLen+1)}, "user_agent"},
		{"wrong type", "r", map[string]any{"method": 7}, "method"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ParseIntent(c.requestID, intentJSON(t, c.over), now)
			if !slices.Contains(fields(err), c.want) {
				t.Fatalf("err %v, want field %s", err, c.want)
			}
		})
	}
	if _, err := ParseIntent("r", []byte(`[`), now); !slices.Contains(fields(err), "body") {
		t.Fatalf("malformed body: %v", err)
	}
}

func TestParseOutcome(t *testing.T) {
	out, err := ParseOutcome("r", []byte(`{"outcome":"responded","status":201,"response_bytes":12,"details":{"a":1},"created_id":"01jbx9x9b1c3d5e7f9g1h3j5k7"}`))
	if err != nil {
		t.Fatal(err)
	}
	if out.Outcome != OutcomeResponded || *out.Status != 201 || out.ResponseBytes != 12 || string(out.Details) != `{"a":1}` ||
		*out.CreatedID != "01jbx9x9b1c3d5e7f9g1h3j5k7" {
		t.Fatalf("%+v", out)
	}
	gone, err := ParseOutcome("r", []byte(`{"outcome":"no_response","status":null,"response_bytes":0}`))
	if err != nil || gone.Status != nil {
		t.Fatalf("%+v %v", gone, err)
	}

	cases := map[string]string{
		`{"outcome":"pending","status":200,"response_bytes":0}`:                                            "outcome",
		`{"status":200,"response_bytes":0}`:                                                                "outcome",
		`{"outcome":"responded","status":null,"response_bytes":0}`:                                         "status",
		`{"outcome":"rejected","status":99,"response_bytes":0}`:                                            "status",
		`{"outcome":"rejected","status":404}`:                                                              "response_bytes",
		`{"outcome":"rejected","status":404,"response_bytes":-1}`:                                          "response_bytes",
		`{"outcome":"rejected","status":404,"response_bytes":1.5}`:                                         "response_bytes",
		`{"outcome":"rejected","status":404,"response_bytes":0,"created_id":"01jbxa5m7n9p1q3r5s7t9v1w3x"}`: "created_id",
		`{"outcome":"responded","status":201,"response_bytes":0,"created_id":"a/b"}`:                       "created_id",
		`{"outcome":"responded","status":201,"response_bytes":0,"created_id":""}`:                          "created_id",
		`{"outcome":"rejected","status":404,"response_bytes":0,"details":{"a":1}}`:                         "details",
		`{"outcome":"responded","status":200,"response_bytes":0,"details":[1]}`:                            "details",
		`{"outcome":"responded","status":200,"response_bytes":0,"details":{"a":"\u0000"}}`:                 "details",
	}
	for body, want := range cases {
		if _, err := ParseOutcome("r", []byte(body)); !slices.Contains(fields(err), want) {
			t.Errorf("%s: err %v, want field %s", body, err, want)
		}
	}
}

func TestParseFilter(t *testing.T) {
	f, err := ParseFilter("limit=500&starting_after=01ja2z6q3y8d5v2k9n4r7t1w0x&operator_id=op_1&service=identity" +
		"&method=PATCH&route=%2Fnodes%2F%7Bnode_id%7D&outcome=pending&request_id=abc&node_id=01jbx9q3t5vk8m2n4p6r7s8t9w" +
		"&acting_as_member_id=01jbyb0d1e2f3g4h5j6k7m8n9p&created_id=01jbx9x9b1c3d5e7f9g1h3j5k7" +
		"&params%5Bnode_id%5D=01jbxa5m7n9p1q3r5s7t9v1w3x&occurred_after=2026-10-01T00:00:00Z&occurred_before=2026-10-02T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if f.Limit != maxListLimit || f.OperatorID != "op_1" || f.Route != "/nodes/{node_id}" || f.Service != "identity" ||
		f.Params["node_id"] != "01jbxa5m7n9p1q3r5s7t9v1w3x" || f.OccurredAfter == nil || f.OccurredBefore == nil || f.Outcome != "pending" ||
		f.NodeID != "01jbx9q3t5vk8m2n4p6r7s8t9w" || f.ActingAsMember != "01jbyb0d1e2f3g4h5j6k7m8n9p" || f.CreatedID != "01jbx9x9b1c3d5e7f9g1h3j5k7" {
		t.Fatalf("%+v", f)
	}
	if d, _ := ParseFilter(""); d.Limit != defaultListLimit {
		t.Fatalf("default limit %d", d.Limit)
	}

	for query, want := range map[string]string{
		"limit=0":             "limit",
		"limit=x":             "limit",
		"starting_after=nope": "starting_after",
		"starting_after=01JA2Z6Q3Y8D5V2K9N4R7T1W0X": "starting_after",
		"outcome=done":                "outcome",
		"method=a%20b":                "method",
		"occurred_after=yesterday":    "occurred_after",
		"params%5BBad%5D=x":           "params[Bad]",
		"operator_id=a&operator_id=b": "operator_id",
		"organization_id=x":           "organization_id",
		"upstream=identity":           "upstream",
		"service=Identity":            "service",
		"created_id=a%2Fb":            "created_id",
		"node_id=":                    "node_id",
		"%zz":                         "query",
	} {
		if _, err := ParseFilter(query); !slices.Contains(fields(err), want) {
			t.Errorf("%s: err %v, want field %s", query, err, want)
		}
	}
}

func manyNodes(n int) []any {
	out := make([]any, n)
	for i := range out {
		out[i] = map[string]any{"kind": "folder", "id": fmt.Sprintf("n_%d", i)}
	}
	return out
}
