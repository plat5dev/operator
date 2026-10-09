package events

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/plat5dev/operator/internal/apierr"
)

const maxBodyBytes = 64 << 10

type store interface {
	CreateIntent(ctx context.Context, in *Intent) (bool, error)
	ApplyOutcome(ctx context.Context, requestID string, out *Outcome) (bool, error)
	List(ctx context.Context, f Filter) ([]*Event, bool, error)
}

type Handler struct {
	store store
	now   func() time.Time
	log   *slog.Logger
}

func NewHandler(s store, log *slog.Logger) *Handler {
	return &Handler{store: s, now: time.Now, log: log}
}

// MountInternal registers the gateway's writes. Every call needs the bearer token.
func (h *Handler) MountInternal(mux *http.ServeMux, token string) {
	mux.Handle("PUT /internal/events/{request_id}", requireToken(token, http.HandlerFunc(h.PutIntent)))
	mux.Handle("PATCH /internal/events/{request_id}", requireToken(token, http.HandlerFunc(h.PatchOutcome)))
}

// MountPublic registers the staff log read. The gateway publishes it as a route.
func (h *Handler) MountPublic(mux *http.ServeMux) {
	mux.HandleFunc("GET /operator-audit-events", h.List)
}

// requireToken admits the gateway alone: only it writes the staff log.
func requireToken(token string, next http.Handler) http.Handler {
	want := []byte("Bearer " + token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := r.Header.Get("Authorization")
		if got == "" {
			apierr.Unauthorized(w, r, "missing")
			return
		}
		if subtle.ConstantTimeCompare([]byte(got), want) != 1 {
			apierr.Unauthorized(w, r, "invalid")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// PutIntent creates the pending event, or leaves an existing one alone.
func (h *Handler) PutIntent(w http.ResponseWriter, r *http.Request) {
	body, ok := readBody(w, r)
	if !ok {
		return
	}
	in, err := ParseIntent(r.PathValue("request_id"), body, h.now())
	if err != nil {
		invalid(w, r, err)
		return
	}
	created, err := h.store.CreateIntent(r.Context(), in)
	if err != nil {
		h.log.Error("create audit intent", "request_id", in.RequestID, "err", err)
		apierr.Unavailable(w, r)
		return
	}
	if created {
		w.WriteHeader(http.StatusCreated)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// PatchOutcome applies the outcome once. A final event is left unchanged.
func (h *Handler) PatchOutcome(w http.ResponseWriter, r *http.Request) {
	body, ok := readBody(w, r)
	if !ok {
		return
	}
	requestID := r.PathValue("request_id")
	out, err := ParseOutcome(requestID, body)
	if err != nil {
		invalid(w, r, err)
		return
	}
	if _, err := h.store.ApplyOutcome(r.Context(), requestID, out); err != nil {
		if errors.Is(err, ErrNotFound) {
			apierr.NotFound(w, r)
			return
		}
		h.log.Error("apply audit outcome", "request_id", requestID, "err", err)
		apierr.Unavailable(w, r)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type listResponse struct {
	AuditEvents []eventResponse `json:"audit_events"`
	HasMore     bool            `json:"has_more"`
}

type eventResponse struct {
	ID            string            `json:"id"`
	OccurredAt    string            `json:"occurred_at"`
	RequestID     string            `json:"request_id"`
	Actor         Actor             `json:"actor"`
	Upstream      *string           `json:"upstream"`
	Method        string            `json:"method"`
	Route         *string           `json:"route"`
	Params        map[string]string `json:"params"`
	Path          *string           `json:"path"`
	IP            string            `json:"ip"`
	UserAgent     *string           `json:"user_agent"`
	Outcome       string            `json:"outcome"`
	Status        *int              `json:"status"`
	ResponseBytes *int64            `json:"response_bytes"`
	Decision      json.RawMessage   `json:"decision"`
	Details       json.RawMessage   `json:"details"`
}

func toResponse(e *Event) eventResponse {
	params := e.Params
	if params == nil {
		params = map[string]string{}
	}
	return eventResponse{
		ID:            e.ID,
		OccurredAt:    e.OccurredAt.UTC().Format("2006-01-02T15:04:05.000Z"),
		RequestID:     e.RequestID,
		Actor:         e.Actor,
		Upstream:      e.Upstream,
		Method:        e.Method,
		Route:         e.Route,
		Params:        params,
		Path:          e.Path,
		IP:            e.IP,
		UserAgent:     e.UserAgent,
		Outcome:       e.Outcome,
		Status:        e.Status,
		ResponseBytes: e.ResponseBytes,
		Details:       e.Details,
	}
}

// List is one page of the staff log, newest first.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	f, err := ParseFilter(r.URL.RawQuery)
	if err != nil {
		invalid(w, r, err)
		return
	}
	list, hasMore, err := h.store.List(r.Context(), f)
	if err != nil {
		h.log.Error("list audit events", "request_id", apierr.RequestID(r.Context()), "err", err)
		apierr.Internal(w, r)
		return
	}
	resp := listResponse{AuditEvents: make([]eventResponse, 0, len(list)), HasMore: hasMore}
	for _, e := range list {
		resp.AuditEvents = append(resp.AuditEvents, toResponse(e))
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(resp)
}

func readBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		apierr.Validation(w, r, "body")
		return nil, false
	}
	return body, true
}

func invalid(w http.ResponseWriter, r *http.Request, err error) {
	var bad Invalid
	if !errors.As(err, &bad) {
		bad = Invalid{"body"}
	}
	paths := make([]string, len(bad))
	for i, p := range bad {
		paths[i] = strings.TrimSpace(p)
	}
	apierr.Validation(w, r, paths...)
}
