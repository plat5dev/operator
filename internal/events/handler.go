package events

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/plat5dev/operator/internal/apierr"
	"github.com/plat5dev/operator/internal/db"
)

const (
	maxBodyBytes       = 64 << 10
	defaultExportLimit = 100
	maxExportLimit     = 1000
)

type store interface {
	CreateIntent(ctx context.Context, in *Intent) (bool, error)
	ApplyOutcome(ctx context.Context, requestID string, out *Outcome) (bool, error)
	List(ctx context.Context, f Filter) ([]*Event, bool, error)
	Export(ctx context.Context, after int64, limit int) ([]*Event, int64, bool, error)
}

type Handler struct {
	store store
	now   func() time.Time
	log   *slog.Logger
}

func NewHandler(s store, log *slog.Logger) *Handler {
	return &Handler{store: s, now: time.Now, log: log}
}

// MountInternal registers the gateway's writes and the deployment-wide export
// feed, on the internal port: only the deployment's network reaches it.
func (h *Handler) MountInternal(mux *http.ServeMux) {
	mux.HandleFunc("GET /internal/events", h.Export)
	mux.HandleFunc("PUT /internal/events/{request_id}", h.PutIntent)
	mux.HandleFunc("PATCH /internal/events/{request_id}", h.PatchOutcome)
}

// MountPublic registers the operator log read. The gateway publishes it as a route.
func (h *Handler) MountPublic(mux *http.ServeMux) {
	mux.HandleFunc("GET /operator-audit-events", h.List)
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
		writeStoreErr(w, r, err)
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
		writeStoreErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type listResponse struct {
	AuditEvents []eventResponse `json:"audit_events"`
	HasMore     bool            `json:"has_more"`
}

type exportEventResponse struct {
	Seq string `json:"seq"`
	eventResponse
}

type exportResponse struct {
	AuditEvents []exportEventResponse `json:"audit_events"`
	Cursor      string                `json:"cursor"`
	HasMore     bool                  `json:"has_more"`
}

type eventResponse struct {
	ID            string            `json:"id"`
	OccurredAt    string            `json:"occurred_at"`
	RequestID     string            `json:"request_id"`
	Actor         Actor             `json:"actor"`
	ActingAs      *ActingAs         `json:"acting_as"`
	Reason        *string           `json:"reason"`
	RootID        *string           `json:"root_id"`
	Nodes         []Node            `json:"nodes"`
	Service       *string           `json:"service"`
	Method        string            `json:"method"`
	Route         *string           `json:"route"`
	Params        map[string]string `json:"params"`
	Path          *string           `json:"path"`
	IP            string            `json:"ip"`
	UserAgent     *string           `json:"user_agent"`
	Outcome       string            `json:"outcome"`
	Status        *int              `json:"status"`
	ResponseBytes *int64            `json:"response_bytes"`
	Details       json.RawMessage   `json:"details"`
	CreatedID     *string           `json:"created_id"`
}

func toResponse(e *Event) eventResponse {
	params := e.Params
	if params == nil {
		params = map[string]string{}
	}
	nodes := e.Nodes
	if nodes == nil {
		nodes = []Node{}
	}
	return eventResponse{
		ID:            e.ID,
		OccurredAt:    e.OccurredAt.UTC().Format("2006-01-02T15:04:05.000Z"),
		RequestID:     e.RequestID,
		Actor:         e.Actor,
		ActingAs:      e.ActingAs,
		Reason:        e.Reason,
		RootID:        e.RootID,
		Nodes:         nodes,
		Service:       e.Service,
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
		CreatedID:     e.CreatedID,
	}
}

// List is one page of the operator log, newest first.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	f, err := ParseFilter(r.URL.RawQuery)
	if err != nil {
		invalid(w, r, err)
		return
	}
	list, hasMore, err := h.store.List(r.Context(), f)
	if err != nil {
		h.log.Error("list audit events", "request_id", apierr.RequestID(r.Context()), "err", err)
		writeStoreErr(w, r, err)
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

// Export is GET /internal/events: finalized operator audit events in commit
// order for SIEM / WORM archival (docs/reference/api/operator-audit.md#export).
func (h *Handler) Export(w http.ResponseWriter, r *http.Request) {
	after, limit, err := parseExportQuery(r.URL.RawQuery)
	if err != nil {
		invalid(w, r, err)
		return
	}
	list, cursor, hasMore, err := h.store.Export(r.Context(), after, limit)
	if errors.Is(err, ErrCursorAhead) {
		apierr.CursorAhead(w, r)
		return
	}
	if err != nil {
		h.log.Error("export audit events", "request_id", apierr.RequestID(r.Context()), "err", err)
		writeStoreErr(w, r, err)
		return
	}
	resp := exportResponse{
		AuditEvents: make([]exportEventResponse, 0, len(list)),
		Cursor:      strconv.FormatInt(cursor, 10),
		HasMore:     hasMore,
	}
	for _, e := range list {
		seq := ""
		if e.FinalizedSeq != nil {
			seq = strconv.FormatInt(*e.FinalizedSeq, 10)
		}
		resp.AuditEvents = append(resp.AuditEvents, exportEventResponse{
			Seq:           seq,
			eventResponse: toResponse(e),
		})
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(resp)
}

func parseExportQuery(raw string) (int64, int, error) {
	q, err := url.ParseQuery(raw)
	if err != nil {
		return 0, 0, Invalid{"query"}
	}
	var after int64
	limit := defaultExportLimit
	var bad Invalid
	for k, vals := range q {
		if len(vals) != 1 {
			bad = append(bad, k)
			continue
		}
		v := strings.TrimSpace(vals[0])
		switch k {
		case "after":
			n, ok := parseSeq(v)
			if !ok {
				bad = append(bad, k)
				continue
			}
			after = n
		case "limit":
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 {
				bad = append(bad, k)
				continue
			}
			limit = min(n, maxExportLimit)
		default:
			bad = append(bad, k)
		}
	}
	if len(bad) > 0 {
		return 0, 0, bad
	}
	return after, limit, nil
}

func parseSeq(v string) (int64, bool) {
	if v == "" || v[0] < '1' || v[0] > '9' {
		return 0, false
	}
	for i := 0; i < len(v); i++ {
		if v[i] < '0' || v[i] > '9' {
			return 0, false
		}
	}
	n, err := strconv.ParseInt(v, 10, 64)
	return n, err == nil
}

func writeStoreErr(w http.ResponseWriter, r *http.Request, err error) {
	if db.Unavailable(err) {
		apierr.Unavailable(w, r)
		return
	}
	apierr.Internal(w, r)
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
