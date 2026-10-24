// Package gateway is the operator front door: authenticate, match, resolve, audit, strip,
// forward, log. Contract: docs/concepts/requests.md, docs/concepts/audit.md.
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
	"net/url"
	"strings"
	"time"

	"github.com/plat5dev/operator/internal/apierr"
	"github.com/plat5dev/operator/internal/audit"
	"github.com/plat5dev/operator/internal/auth"
	"github.com/plat5dev/operator/internal/identity"
	"github.com/plat5dev/operator/internal/metrics"
	"github.com/plat5dev/operator/internal/routes"
)

// Authenticator turns an Authorization header into an operator. Errors are the auth package's.
type Authenticator interface {
	Verify(ctx context.Context, header string) (*auth.Operator, error)
}

// Identity is Plat5 identity: a node's chain, and a member in a tree. *identity.Client is
// the real one. Errors are the identity package's.
type Identity interface {
	Chain(ctx context.Context, nodeID, requestID string) ([]identity.Node, error)
	Member(ctx context.Context, rootID, memberID, requestID string) (principal string, err error)
}

// AuditWriter records the staff audit log. *audit.Writer is the real one.
type AuditWriter interface {
	WriteIntent(ctx context.Context, in *audit.Intent) error
	SendOutcome(requestID string, out audit.Outcome)
}

type Config struct {
	Routes *routes.Table
	Auth   Authenticator
	// Identity is required when a route names a node.
	Identity        Identity
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
	identity        Identity
	audit           AuditWriter
	origins         map[string]bool
	proxies         map[*routes.Route]*httputil.ReverseProxy
	unauthenticated *metrics.Counter
	log             *slog.Logger
}

const (
	plat5Prefix      = "X-Plat5-"
	operatorHeader   = "X-Plat5-Operator-Id"
	memberHeader     = "X-Plat5-Member-Id"
	principalHeader  = "X-Plat5-Principal"
	preflightMethods = "GET, HEAD, POST, PUT, PATCH, DELETE"
	preflightHeaders = "Authorization, Content-Type, X-Request-ID, traceparent"
)

// Credentials the upstream never sees. Every incoming X-Plat5-* header goes too.
var stripped = []string{"Authorization", "Cookie", "X-API-Key"}

// Set on every API response, as Plat5's gateway sets them.
var securityHeaders = map[string]string{
	"X-Content-Type-Options": "nosniff",
	"X-Frame-Options":        "DENY",
	"Referrer-Policy":        "strict-origin-when-cross-origin",
}

func New(cfg Config) *Gateway {
	if cfg.Routes.HasNodes() && cfg.Identity == nil {
		panic("gateway: routes name nodes but there is no identity client")
	}
	reg := cfg.Metrics
	if reg == nil {
		reg = metrics.NewRegistry()
	}
	g := &Gateway{
		routes:   cfg.Routes,
		auth:     cfg.Auth,
		identity: cfg.Identity,
		audit:    cfg.Audit,
		origins:  map[string]bool{},
		proxies:  map[*routes.Route]*httputil.ReverseProxy{},
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

// forward is what the proxy sends beside the path. The proxy hooks see only the request.
type forward struct {
	operatorID string
	chain      []identity.Node
	// member and principal are set when acting as a member.
	member, principal string
	// path is the escaped path to send: the request's, without /as/{member}.
	path string
}

type forwardKey struct{}

func (g *Gateway) proxy(route *routes.Route, transport http.RoundTripper) *httputil.ReverseProxy {
	target := route.Target()
	return &httputil.ReverseProxy{
		Transport: transport,
		Rewrite: func(pr *httputil.ProxyRequest) {
			f := pr.In.Context().Value(forwardKey{}).(*forward)
			// SetURL keeps the escaped request path and query exactly as received.
			pr.SetURL(target)
			if f.path != pr.In.URL.EscapedPath() {
				pr.Out.URL.RawPath = f.path
				pr.Out.URL.Path, _ = url.PathUnescape(f.path)
			}
			h := pr.Out.Header
			for _, k := range stripped {
				h.Del(k)
			}
			// A client can't name a node, a member or an operator.
			for k := range h {
				if strings.HasPrefix(k, plat5Prefix) {
					h.Del(k)
				}
			}
			h.Set("X-Request-ID", apierr.RequestID(pr.In.Context()))
			h.Set(operatorHeader, f.operatorID)
			for _, n := range f.chain {
				h.Set(kindHeader(n.Kind), n.ID)
			}
			if f.member != "" {
				h.Set(memberHeader, f.member)
				h.Set(principalHeader, f.principal)
			}
		},
		ModifyResponse: func(res *http.Response) error {
			// This gateway owns the request id, CORS and the security headers.
			res.Header.Del("X-Request-ID")
			for k := range securityHeaders {
				res.Header.Del(k)
			}
			details := res.Header.Values(audit.DetailsHeader)
			created := res.Header.Values(audit.CreatedHeader)
			// What the service tells the audit log never reaches the client, audit on or off.
			for k := range res.Header {
				if strings.HasPrefix(k, "Access-Control-") || strings.HasPrefix(k, audit.HeaderPrefix) {
					res.Header.Del(k)
				}
			}
			if st := audit.StateFrom(res.Request.Context()); st != nil {
				st.UpstreamStatus = res.StatusCode
				id := apierr.RequestID(res.Request.Context())
				if len(details) > 0 {
					var ok bool
					if st.Details, ok = audit.ParseDetails(details); !ok {
						g.log.Warn("ignoring audit details that are not one ASCII JSON object within 4096 bytes",
							"request_id", id, "service", route.Service())
					}
				}
				if len(created) > 0 {
					var ok bool
					if st.CreatedID, ok = audit.ParseCreated(created); !ok {
						g.log.Warn("ignoring an audit created id that is not one path segment of ASCII within 128 bytes",
							"request_id", id, "service", route.Service())
					}
				}
			}
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			if st := audit.StateFrom(r.Context()); st != nil && r.Context().Err() != nil {
				st.ClientGone = true
			}
			g.log.Warn("upstream failed", "request_id", apierr.RequestID(r.Context()), "service", route.Service(), "err", err)
			apierr.BadGateway(w, r)
		},
	}
}

// kindHeader is X-Plat5-<Kind>-Id, as Plat5 names it: project → X-Plat5-Project-Id,
// cost_center → X-Plat5-Cost-Center-Id.
func kindHeader(kind string) string {
	return http.CanonicalHeaderKey(plat5Prefix + strings.ReplaceAll(kind, "_", "-") + "-Id")
}

// resolved is what identity said about a matched request.
type resolved struct {
	chain []identity.Node
	// principal is set once the member passed the check.
	principal string
	err       error
}

// found is the chain when identity answered for it, else nil.
func (r resolved) found() []identity.Node {
	if errors.Is(r.err, identity.ErrUnavailable) {
		return nil
	}
	return r.chain
}

// resolve looks up the route node's chain and, acting as a member, the member in the
// chain's root. It answers nothing: the intent is written first.
func (g *Gateway) resolve(ctx context.Context, id string, m *routes.Match) resolved {
	var res resolved
	if m == nil || m.Route.Node == "" {
		return res
	}
	res.chain, res.err = g.identity.Chain(ctx, m.NodeID(), id)
	if res.err != nil || m.Member == "" {
		return res
	}
	res.principal, res.err = g.identity.Member(ctx, res.chain[0].ID, m.Member, id)
	return res
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
	for k, v := range securityHeaders {
		h.Set(k, v)
	}
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
		op    *auth.Operator
		match *routes.Match
		res   resolved
		state *audit.State
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
		g.logRequest(r, id, op, match, res, rec.status, time.Since(start))
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
		match = g.routes.Match(r.Method, segs)
	}

	// 4. The route node's chain, and the member when acting as one. Nothing is answered yet.
	res = g.resolve(r.Context(), id, match)

	// 5. Nothing past authentication answers until the audit intent is written.
	if g.audit != nil {
		state = &audit.State{}
		r = r.WithContext(audit.WithState(r.Context(), state))
		if err := g.audit.WriteIntent(r.Context(), intent(r, id, start, op, match, res)); err != nil {
			apierr.Unavailable(w, r)
			return
		}
	}

	// 6. Answer what can't be forwarded.
	switch {
	case pathErr != nil:
		apierr.InvalidRequest(w, r)
		return
	case match == nil:
		apierr.NotFound(w, r)
		return
	case errors.Is(res.err, identity.ErrNotFound):
		apierr.NotFoundResource(w, r, "node", match.NodeID())
		return
	case errors.Is(res.err, identity.ErrNotActive):
		apierr.InvalidRequest(w, r)
		return
	case res.err != nil:
		g.log.Error("identity unavailable", "request_id", id, "err", res.err)
		apierr.Unavailable(w, r)
		return
	}

	// 7. Forward.
	f := &forward{operatorID: op.ID, chain: res.chain, path: r.URL.EscapedPath()}
	if match.Member != "" {
		f.member, f.principal = match.Member, res.principal
		f.path = stripAs(f.path)
	}
	r = r.WithContext(context.WithValue(r.Context(), forwardKey{}, f))
	if state != nil {
		state.Forwarded = true
	}
	g.proxies[match.Route].ServeHTTP(w, r)
}

// stripAs drops the /as/{member} prefix from an escaped path that has one.
func stripAs(escaped string) string {
	// "/as/{member}/rest": the third slash starts the rest.
	n := 0
	for i := 0; i < len(escaped); i++ {
		if escaped[i] == '/' {
			if n++; n == 3 {
				return escaped[i:]
			}
		}
	}
	return escaped
}

func (g *Gateway) unauthorized(w http.ResponseWriter, r *http.Request, reason string) {
	g.unauthenticated.Inc(reason)
	apierr.Unauthorized(w, r, reason)
}

// intent is everything known about the request before it is answered or forwarded.
func intent(r *http.Request, id string, at time.Time, op *auth.Operator, m *routes.Match, res resolved) *audit.Intent {
	in := &audit.Intent{
		RequestID:  id,
		OccurredAt: audit.Timestamp(at),
		Actor: audit.Actor{
			Issuer:     audit.Text(op.Issuer),
			OperatorID: audit.Text(op.ID),
			Email:      optional(op.Email),
			ClientID:   optional(op.ClientID),
		},
		Nodes:     []audit.Node{},
		Method:    r.Method,
		Params:    map[string]string{},
		IP:        audit.ClientIP(r.RemoteAddr),
		UserAgent: audit.UserAgent(r.UserAgent()),
	}
	if m == nil {
		path := audit.Path(r.URL.EscapedPath())
		in.Path = &path
		return in
	}
	service, template := m.Route.Service(), m.Template()
	in.Service, in.Route = &service, &template
	for k, v := range m.Params {
		in.Params[k] = audit.Text(v)
	}
	if chain := res.found(); len(chain) > 0 {
		for _, n := range chain {
			in.Nodes = append(in.Nodes, audit.Node{Kind: n.Kind, ID: n.ID})
		}
		root := chain[0].ID
		in.RootID = &root
	}
	if res.principal != "" {
		in.ActingAs = &audit.ActingAs{MemberID: m.Member, Principal: res.principal}
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

func (g *Gateway) logRequest(r *http.Request, id string, op *auth.Operator, m *routes.Match, res resolved, status int, d time.Duration) {
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
	if m != nil {
		attrs = append(attrs, "service", m.Route.Service(), "route", m.Template())
		if len(m.Params) > 0 {
			attrs = append(attrs, "params", m.Params)
		}
		if chain := res.found(); len(chain) > 0 {
			attrs = append(attrs, "root_id", chain[0].ID, "node_id", chain[len(chain)-1].ID)
		}
		if res.principal != "" {
			attrs = append(attrs, "member_id", m.Member)
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
