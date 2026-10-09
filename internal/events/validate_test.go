package events

import (
	"encoding/json"
	"errors"
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
		"upstream":    "identity",
		"method":      "PATCH",
		"route":       "/organizations/{organization_id}",
		"params":      map[string]any{"organization_id": "org_1"},
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
	if in.Actor.Email != nil || *in.Actor.ClientID != "console" || *in.Upstream != "identity" || in.Path != nil ||
		in.Params["organization_id"] != "org_1" || *in.UserAgent != "curl/8" {
		t.Fatalf("%+v", in)
	}

	unmatched, err := ParseIntent("req1", intentJSON(t, map[string]any{
		"upstream": nil, "route": nil, "params": map[string]any{}, "path": "/nope/a%2Fb",
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
		{"upstream", "r", map[string]any{"upstream": "Identity"}, "upstream"},
		{"route without slash", "r", map[string]any{"route": "organizations"}, "route"},
		{"route and path", "r", map[string]any{"path": "/organizations/org_1"}, "path"},
		{"neither route nor path", "r", map[string]any{"route": nil}, "route"},
		{"path with upstream", "r", map[string]any{"route": nil, "params": map[string]any{}, "path": "/x"}, "upstream"},
		{"path with params", "r", map[string]any{"route": nil, "upstream": nil, "path": "/x"}, "params"},
		{"param name", "r", map[string]any{"params": map[string]any{"Org": "x"}}, "params.Org"},
		{"empty param", "r", map[string]any{"params": map[string]any{"organization_id": ""}}, "params.organization_id"},
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
	out, err := ParseOutcome("r", []byte(`{"outcome":"responded","status":200,"response_bytes":12,"decision":null,"details":{"a":1}}`))
	if err != nil {
		t.Fatal(err)
	}
	if out.Outcome != OutcomeResponded || *out.Status != 200 || out.ResponseBytes != 12 || string(out.Details) != `{"a":1}` {
		t.Fatalf("%+v", out)
	}
	gone, err := ParseOutcome("r", []byte(`{"outcome":"no_response","status":null,"response_bytes":0}`))
	if err != nil || gone.Status != nil {
		t.Fatalf("%+v %v", gone, err)
	}

	cases := map[string]string{
		`{"outcome":"pending","status":200,"response_bytes":0}`:                            "outcome",
		`{"status":200,"response_bytes":0}`:                                                "outcome",
		`{"outcome":"responded","status":null,"response_bytes":0}`:                         "status",
		`{"outcome":"rejected","status":99,"response_bytes":0}`:                            "status",
		`{"outcome":"rejected","status":404}`:                                              "response_bytes",
		`{"outcome":"rejected","status":404,"response_bytes":-1}`:                          "response_bytes",
		`{"outcome":"rejected","status":404,"response_bytes":1.5}`:                         "response_bytes",
		`{"outcome":"rejected","status":404,"response_bytes":0,"decision":true}`:           "decision",
		`{"outcome":"rejected","status":404,"response_bytes":0,"details":{"a":1}}`:         "details",
		`{"outcome":"responded","status":200,"response_bytes":0,"details":[1]}`:            "details",
		`{"outcome":"responded","status":200,"response_bytes":0,"details":{"a":"\u0000"}}`: "details",
	}
	for body, want := range cases {
		if _, err := ParseOutcome("r", []byte(body)); !slices.Contains(fields(err), want) {
			t.Errorf("%s: err %v, want field %s", body, err, want)
		}
	}
}

func TestParseFilter(t *testing.T) {
	f, err := ParseFilter("limit=500&starting_after=01JA2Z6Q3Y8D5V2K9N4R7T1W0X&operator_id=op_1&upstream=identity" +
		"&method=PATCH&route=%2Forganizations%2F%7Borganization_id%7D&outcome=pending&request_id=abc" +
		"&params%5Borganization_id%5D=org_1&occurred_after=2026-10-01T00:00:00Z&occurred_before=2026-10-02T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if f.Limit != maxListLimit || f.OperatorID != "op_1" || f.Route != "/organizations/{organization_id}" ||
		f.Params["organization_id"] != "org_1" || f.OccurredAfter == nil || f.OccurredBefore == nil || f.Outcome != "pending" {
		t.Fatalf("%+v", f)
	}
	if d, _ := ParseFilter(""); d.Limit != defaultListLimit {
		t.Fatalf("default limit %d", d.Limit)
	}

	for query, want := range map[string]string{
		"limit=0":                     "limit",
		"limit=x":                     "limit",
		"starting_after=nope":         "starting_after",
		"outcome=done":                "outcome",
		"method=a%20b":                "method",
		"occurred_after=yesterday":    "occurred_after",
		"params%5BBad%5D=x":           "params[Bad]",
		"operator_id=a&operator_id=b": "operator_id",
		"organization_id=x":           "organization_id",
		"%zz":                         "query",
	} {
		if _, err := ParseFilter(query); !slices.Contains(fields(err), want) {
			t.Errorf("%s: err %v, want field %s", query, err, want)
		}
	}
}
