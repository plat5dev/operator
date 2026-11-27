package authorizer

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestEvaluateAllowAndDeny(t *testing.T) {
	var gotAuth, gotReqID string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/access/v1/evaluation" || r.Method != http.MethodPost {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(404)
			return
		}
		gotAuth = r.Header.Get("Authorization")
		gotReqID = r.Header.Get("X-Request-ID")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		act := gotBody["action"].(map[string]any)
		if act["name"] == "DELETE" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"decision": false,
				"context":  map[string]any{"reason": "ticket required"},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"decision": true})
	}))
	defer srv.Close()

	c := New(Config{URL: srv.URL, Token: "pdp-secret", Timeout: 2 * time.Second})
	res, err := c.Evaluate(context.Background(), Request{
		Subject:  Subject{Type: "operator", ID: "op_1"},
		Action:   Action{Name: "GET"},
		Resource: Resource{Type: "service", ID: "identity"},
		Context:  map[string]any{"request_id": "req-1"},
	})
	if err != nil || !res.Allowed || res.Reason != "" {
		t.Fatalf("allow: %+v %v", res, err)
	}
	if gotAuth != "Bearer pdp-secret" || gotReqID != "req-1" {
		t.Fatalf("auth=%q reqID=%q", gotAuth, gotReqID)
	}

	res, err = c.Evaluate(context.Background(), Request{
		Subject:  Subject{Type: "operator", ID: "op_1"},
		Action:   Action{Name: "DELETE"},
		Resource: Resource{Type: "project", ID: "01jbx"},
		Context:  map[string]any{"request_id": "req-2"},
	})
	if err != nil || res.Allowed || res.Reason != "ticket required" {
		t.Fatalf("deny: %+v %v", res, err)
	}
}

func TestEvaluateFailClosed(t *testing.T) {
	cases := []struct {
		name string
		h    http.HandlerFunc
	}{
		{"500", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(500) }},
		{"redirect", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/other", http.StatusFound) }},
		{"bad json", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{`)) }},
		{"missing decision", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{}`)) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(tc.h)
			defer srv.Close()
			c := New(Config{URL: srv.URL})
			_, err := c.Evaluate(context.Background(), Request{
				Subject:  Subject{Type: "operator", ID: "op_1"},
				Action:   Action{Name: "GET"},
				Resource: Resource{Type: "service", ID: "api"},
			})
			if !errors.Is(err, ErrUnavailable) {
				t.Fatalf("err = %v, want ErrUnavailable", err)
			}
		})
	}
}

func TestEvaluateBatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/access/v1/evaluations" {
			w.WriteHeader(404)
			return
		}
		var req struct {
			Options     map[string]any `json:"options"`
			Evaluations []BatchItem    `json:"evaluations"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Options["evaluations_semantic"] != "execute_all" {
			t.Errorf("semantic = %v", req.Options["evaluations_semantic"])
		}
		out := make([]map[string]any, len(req.Evaluations))
		for i, item := range req.Evaluations {
			if item.Action.Name == "GET" {
				out[i] = map[string]any{"decision": true}
			} else {
				out[i] = map[string]any{"decision": false, "context": map[string]any{"reason": "read-only"}}
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"evaluations": out})
	}))
	defer srv.Close()

	c := New(Config{URL: srv.URL})
	res, err := c.EvaluateBatch(context.Background(),
		Subject{Type: "operator", ID: "op_1"},
		map[string]any{"request_id": "r1"},
		[]BatchItem{
			{Action: Action{Name: "GET"}, Resource: Resource{Type: "service", ID: "identity"}},
			{Action: Action{Name: "POST"}, Resource: Resource{Type: "service", ID: "identity"}},
		},
	)
	if err != nil || len(res) != 2 || !res[0].Allowed || res[1].Allowed || res[1].Reason != "read-only" {
		t.Fatalf("batch = %+v, %v", res, err)
	}
}
