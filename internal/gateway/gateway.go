// Package gateway is the operator front door: authenticate, match, audit, strip, forward, log.
// Contract: docs/model.md, docs/audit.md.
package gateway

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"strings"
	"time"

	"github.com/plat5dev/operator/internal/apierr"
	"github.com/plat5dev/operator/internal/audit"
	"github.com/plat5dev/operator/internal/auth"
	"github.com/plat5dev/operator/internal/metrics"
	"github.com/plat5dev/operator/internal/routes"
)

// Authenticator turns an Authorization header into an operator. Errors are the auth package's.
type Authenticator interface {
	Verify(ctx context.Context, header string) (*auth.Operator, error)
}

// AuditWriter records the staff audit log. *audit.Writer is the real one.
type AuditWriter interface {
	WriteIntent(ctx context.Context, in *audit.Intent) error
	SendOutcome(requestID string, out audit.Outcome)
}

type Config struct {
	Routes          *routes.Table
	Auth            Authenticator
	AllowedOrigins  []string
	UpstreamTimeout time.Duration
	// Audit is nil when audit is off.
	Audit   AuditWriter
	Metrics *metrics.Registry
	Log     *slog.Logger
}

type Gateway struct {
	routes          *routes.Table
	auth            Authenticator
	audit           AuditWriter
	origins         map[string]bool
	proxies         map[*routes.Route]*httputil.ReverseProxy
	unauthenticated *metrics.Counter
	log             *slog.Logger
}

// Credentials the upstream never sees.
var stripped = []string{"Authorization", "Cookie", "X-API-Key"}

const (
	preflightMethods = "GET, HEAD, POST, PUT, PATCH, DELETE"
	preflightHeaders = "Authorization, Content-Type, X-Request-ID, traceparent"
)

func New(cfg Config) *Gateway {
	reg := cfg.Metrics
	if reg == nil {
		reg = metrics.NewRegistry()
	}
	g := &Gateway{
		routes:  cfg.Routes,
		auth:    cfg.Auth,
		audit:   cfg.Audit,
		origins: map[string]bool{},
		proxies: map[*routes.Route]*httputil.ReverseProxy{},
		unauthenticated: reg.Counter("operator_unauthenticated_total",
			"Requests answered 401, by reason. They are not audit events.", "reason"),
		log: cfg.Log,
	}
	for _, reason := range []string{"missing", "expired", "invalid"} {
		g.unauthenticated.Add(0, reason)
	}
	for _, o := range cfg.AllowedOrigins {
		g.origins[strings.ToLower(o)] = true
	}
	transport := &http.Transport{
		DialContext:           (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ResponseHeaderTimeout: cfg.UpstreamTimeout,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
	}
	for _, r := range cfg.Routes.Routes() {
		g.proxies[r] = g.proxy(r, transport)
	}
	return g
}

func (g *Gateway) proxy(route *routes.Route, transport http.RoundTripper) *httputil.ReverseProxy {
	target := route.Target()
	return &httputil.ReverseProxy{
		Transport: transport,
		Rewrite: func(pr *httputil.ProxyRequest) {
			// SetURL keeps the escaped request path and query exactly as received.
			pr.SetURL(target)
			for _, h := range stripped {
				pr.Out.Header.Del(h)
			}
			// Only a service sets audit details, on its response. Never pass a client's on.
			pr.Out.Header.Del(audit.DetailsHeader)
			pr.Out.Header.Set("X-Request-ID", apierr.RequestID(pr.In.Context()))
		},
		ModifyResponse: func(res *http.Response) error {
			// This gateway owns the request id and CORS.
			res.Header.Del("X-Request-ID")
			for k := range res.Header {
				if strings.HasPrefix(k, "Access-Control-") {
					res.Header.Del(k)
				}
			}
			// The service's audit details never reach the client, audit on or off.
			details := res.Header.Values(audit.DetailsHeader)
			res.Header.Del(audit.DetailsHeader)
			if st := audit.StateFrom(res.Request.Context()); st != nil {
				st.UpstreamStatus = res.StatusCode
				if len(details) > 0 {
					var ok bool
					if st.Details, ok = audit.ParseDetails(details); !ok {
						g.log.Warn("ignoring audit details that are not one ASCII JSON object within 4096 bytes",
							"request_id", apierr.RequestID(res.Request.Context()), "upstream", route.Upstream())
					}
				}
			}
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			if st := audit.StateFrom(r.Context()); st != nil && r.Context().Err() != nil {
				st.ClientGone = true
			}
			g.log.Warn("upstream failed", "request_id", apierr.RequestID(r.Context()), "upstream", route.Upstream(), "err", err)
			apierr.Unavailable(w, r)
		},
	}
}

func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	// Every request id is generated here. A client's is not accepted: it keys the audit event.
	id := newRequestID()
	r = r.WithContext(apierr.WithRequestID(r.Context(), id))
	rec := &recorder{ResponseWriter: w}
	w = rec

	h := w.Header()
	h.Set("X-Request-ID", id)
	if len(g.origins) > 0 {
		h.Add("Vary", "Origin")
	}
	origin := r.Header.Get("Origin")
	allowedOrigin := origin != "" && g.origins[strings.ToLower(origin)]
	if allowedOrigin {
		h.Set("Access-Control-Allow-Origin", origin)
		h.Set("Access-Control-Expose-Headers", "X-Request-ID")
	}

	var (
		op     *auth.Operator
		route  *routes.Route
		params map[string]string
		state  *audit.State
	)
	defer func() {
		rv := recover()
		if rv != nil && rv != http.ErrAbortHandler {
			g.log.Error("panic", "request_id", id, "panic", rv)
			if rec.status == 0 {
				apierr.Internal(w, r)
			}
		}
		if state != nil {
			g.audit.SendOutcome(id, audit.OutcomeFor(state, rec.status, rec.bytes))
		}
		g.logRequest(r, id, op, route, params, rec.status, time.Since(start))
		if rv == http.ErrAbortHandler {
			panic(rv)
		}
	}()

	// 1. Preflight from an allowed origin is answered here, never forwarded.
	if r.Method == http.MethodOptions && allowedOrigin && r.Header.Get("Access-Control-Request-Method") != "" {
		h.Set("Access-Control-Allow-Methods", preflightMethods)
		h.Set("Access-Control-Allow-Headers", preflightHeaders)
		h.Set("Access-Control-Max-Age", "600")
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// 2. Authenticate before anything about the route list is revealed.
	op, err := g.auth.Verify(r.Context(), r.Header.Get("Authorization"))
	switch {
	case errors.Is(err, auth.ErrNoKeys):
		apierr.Unavailable(w, r)
		return
	case errors.Is(err, auth.ErrMissing):
		g.unauthorized(w, r, "missing")
		return
	case errors.Is(err, auth.ErrExpired):
		g.unauthorized(w, r, "expired")
		return
	case err != nil:
		g.unauthorized(w, r, "invalid")
		return
	}

	// 3. Check the path and match a route. Nothing is answered yet.
	segs, pathErr := routes.Segments(r.URL.EscapedPath())
	if pathErr == nil {
		route, params = g.routes.Match(r.Method, segs)
	}

	// 4. Nothing past authentication answers until the audit intent is written.
	if g.audit != nil {
		state = &audit.State{}
		r = r.WithContext(audit.WithState(r.Context(), state))
		if err := g.audit.WriteIntent(r.Context(), intent(r, id, start, op, route, params)); err != nil {
			apierr.Unavailable(w, r)
			return
		}
	}

	// 5. Refuse paths the service could read differently, then unrouted requests.
	if pathErr != nil {
		apierr.InvalidRequest(w, r)
		return
	}
	if route == nil {
		apierr.NotFound(w, r)
		return
	}

	// 7. Forward. Step 6, authz, is slice 3.
	if state != nil {
		state.Forwarded = true
	}
	g.proxies[route].ServeHTTP(w, r)
}

func (g *Gateway) unauthorized(w http.ResponseWriter, r *http.Request, reason string) {
	g.unauthenticated.Inc(reason)
	apierr.Unauthorized(w, r, reason)
}

// intent is everything known about the request before it is answered or forwarded.
func intent(r *http.Request, id string, at time.Time, op *auth.Operator, route *routes.Route, params map[string]string) *audit.Intent {
	in := &audit.Intent{
		RequestID:  id,
		OccurredAt: audit.Timestamp(at),
		Actor: audit.Actor{
			Issuer:     audit.Text(op.Issuer),
			OperatorID: audit.Text(op.ID),
			Email:      optional(op.Email),
			ClientID:   optional(op.ClientID),
		},
		Method:    r.Method,
		Params:    map[string]string{},
		IP:        audit.ClientIP(r.RemoteAddr),
		UserAgent: audit.UserAgent(r.UserAgent()),
	}
	if route == nil {
		path := audit.Path(r.URL.EscapedPath())
		in.Path = &path
		return in
	}
	upstream, template := route.Upstream(), route.Path
	in.Upstream, in.Route = &upstream, &template
	for k, v := range params {
		in.Params[k] = audit.Text(v)
	}
	return in
}

func optional(s string) *string {
	if s == "" {
		return nil
	}
	s = audit.Text(s)
	return &s
}

func (g *Gateway) logRequest(r *http.Request, id string, op *auth.Operator, route *routes.Route, params map[string]string, status int, d time.Duration) {
	attrs := []any{
		"request_id", id,
		"method", r.Method,
		"status", status,
		"duration_ms", d.Milliseconds(),
	}
	if op != nil {
		attrs = append(attrs, "operator_id", op.ID)
		if op.Email != "" {
			attrs = append(attrs, "operator_email", op.Email)
		}
	}
	if route != nil {
		attrs = append(attrs, "route", route.Path)
		if len(params) > 0 {
			attrs = append(attrs, "params", params)
		}
	}
	g.log.Info("request", attrs...)
}

func newRequestID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// recorder keeps the status and body size for the request log and the audit outcome.
// Unwrap lets the proxy flush.
type recorder struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (r *recorder) WriteHeader(code int) {
	// A 1xx the proxy passes on is not the answer.
	if r.status == 0 && code >= 200 {
		r.status = code
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *recorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytes += int64(n)
	return n, err
}

func (r *recorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }
