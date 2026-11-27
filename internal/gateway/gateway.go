// Package gateway is the operator front door: authenticate, route /services/{service}/{path},
// resolve node chains, audit intent, authorize via AuthZEN, strip headers, forward, log.
// Contract: docs/concepts/requests.md, docs/concepts/audit.md, docs/reference/gateway.md.
package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/plat5dev/operator/internal/apierr"
	"github.com/plat5dev/operator/internal/audit"
	"github.com/plat5dev/operator/internal/auth"
	"github.com/plat5dev/operator/internal/authorizer"
	"github.com/plat5dev/operator/internal/httpx"
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

// Authorizer decides whether an operator may perform an action. *authorizer.Client is
// the real one. Nil means authorizer.mode is disabled.
type Authorizer interface {
	Evaluate(ctx context.Context, req authorizer.Request) (authorizer.Result, error)
	EvaluateBatch(ctx context.Context, subject authorizer.Subject, reqCtx map[string]any, items []authorizer.BatchItem) ([]authorizer.Result, error)
}

// AuditWriter records the operator audit log. *audit.Writer is the real one.
type AuditWriter interface {
	WriteIntent(ctx context.Context, in *audit.Intent) error
	SendOutcome(requestID string, out audit.Outcome)
}

type Config struct {
	Registry *routes.Registry
	Auth     Authenticator
	// Identity is Plat5 identity's client (may be nil if no service/route resolves nodes).
	Identity Identity
	// Authorizer is nil when authorizer.mode is disabled.
	Authorizer      Authorizer
	AllowedOrigins  []string
	TrustedProxies  []netip.Prefix
	UpstreamTimeout time.Duration
	// Audit is nil when audit is off.
	Audit   AuditWriter
	Signer  *Signer
	Metrics *metrics.Registry
	Log     *slog.Logger
}

type authzHolder struct {
	authz Authorizer
}

type Gateway struct {
	registry        *routes.Registry
	proxy           *httputil.ReverseProxy
	defaultTimeout  time.Duration
	transportMu     sync.RWMutex
	transports      map[time.Duration]http.RoundTripper
	auth            Authenticator
	identity        Identity
	authorizer      atomic.Pointer[authzHolder]
	audit           AuditWriter
	signer          *Signer
	origins         map[string]bool
	trustedProxies  []netip.Prefix
	unauthenticated *metrics.Counter
	log             *slog.Logger
}

const (
	plat5Prefix      = "X-Plat5-"
	asMemberHeader   = "X-Plat5-As-Member"
	reasonHeader     = "X-Plat5-Operator-Reason"
	operatorHeader   = "X-Plat5-Operator-Id"
	memberHeader     = "X-Plat5-Member-Id"
	principalHeader  = "X-Plat5-Principal"
	preflightMethods = "GET, HEAD, POST, PUT, PATCH, DELETE"
	preflightHeaders = "Authorization, Content-Type, X-Plat5-As-Member, X-Plat5-Operator-Reason, X-Request-ID, traceparent"
	maxMemberIDLen   = 256
)

var (
	errBadAsMember = errors.New("invalid or disallowed X-Plat5-As-Member header")
	errBadReason   = errors.New("invalid X-Plat5-Operator-Reason header")
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
	reg := cfg.Metrics
	if reg == nil {
		reg = metrics.NewRegistry()
	}
	timeout := cfg.UpstreamTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	g := &Gateway{
		registry:       cfg.Registry,
		defaultTimeout: timeout,
		transports:     map[time.Duration]http.RoundTripper{},
		auth:           cfg.Auth,
		identity:       cfg.Identity,
		audit:          cfg.Audit,
		signer:         cfg.Signer,
		origins:        map[string]bool{},
		trustedProxies: cfg.TrustedProxies,
		unauthenticated: reg.Counter("operator_unauthenticated_total",
			"Requests answered 401, by reason. They are not audit events.", "reason"),
		log: cfg.Log,
	}
	g.SetAuthorizer(cfg.Authorizer)
	g.transports[timeout] = httpx.NewTransport(timeout)
	g.proxy = g.newProxy()
	for _, reason := range []string{"missing", "expired", "invalid"} {
		g.unauthenticated.Add(0, reason)
	}
	for _, o := range cfg.AllowedOrigins {
		g.origins[strings.ToLower(o)] = true
	}
	return g
}

// SetAuthorizer atomically swaps the active authorizer (nil = disabled).
func (g *Gateway) SetAuthorizer(a Authorizer) {
	g.authorizer.Store(&authzHolder{authz: a})
}

func (g *Gateway) currentAuthorizer() Authorizer {
	if h := g.authorizer.Load(); h != nil {
		return h.authz
	}
	return nil
}

func (g *Gateway) transportFor(timeout time.Duration) http.RoundTripper {
	if timeout <= 0 {
		timeout = g.defaultTimeout
	}
	g.transportMu.RLock()
	tr, ok := g.transports[timeout]
	g.transportMu.RUnlock()
	if ok {
		return tr
	}
	g.transportMu.Lock()
	defer g.transportMu.Unlock()
	if tr, ok := g.transports[timeout]; ok {
		return tr
	}
	tr = httpx.NewTransport(timeout)
	g.transports[timeout] = tr
	return tr
}

type forward struct {
	res        *routes.Resolution
	operatorID string
	chain      []identity.Node
	member     string
	principal  string
}

type forwardKey struct{}

type perServiceTransport struct {
	g *Gateway
}

func (t perServiceTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var timeout time.Duration
	if f, ok := req.Context().Value(forwardKey{}).(*forward); ok && f != nil && f.res != nil {
		timeout = f.res.Timeout
	}
	return t.g.transportFor(timeout).RoundTrip(req)
}

func (g *Gateway) newProxy() *httputil.ReverseProxy {
	return &httputil.ReverseProxy{
		Transport: perServiceTransport{g: g},
		Rewrite: func(pr *httputil.ProxyRequest) {
			f := pr.In.Context().Value(forwardKey{}).(*forward)
			pr.SetURL(f.res.Target)
			// Strip /services/{service} while preserving exact percent-encoding and query string.
			if u, err := url.ParseRequestURI(f.res.UpstreamPath); err == nil {
				pr.Out.URL.Path = u.Path
				pr.Out.URL.RawPath = u.RawPath
			}
			h := pr.Out.Header
			for _, k := range stripped {
				h.Del(k)
			}
			// A client can't name a node, a member, a reason, or an operator.
			for k := range h {
				if hasPrefixFold(k, plat5Prefix) {
					h.Del(k)
				}
			}
			reqID := apierr.RequestID(pr.In.Context())
			h.Set("X-Request-ID", reqID)
			h.Set(operatorHeader, f.operatorID)
			for _, n := range f.chain {
				h.Set(kindHeader(n.Kind), n.ID)
			}
			if f.member != "" {
				h.Set(memberHeader, f.member)
				h.Set(principalHeader, f.principal)
			}
			if g.signer != nil {
				h.Set(SignatureHeader, g.signer.SignNow(pr.Out.Method, pr.Out.URL.RequestURI(), reqID, h))
			}
		},
		ModifyResponse: func(res *http.Response) error {
			f := res.Request.Context().Value(forwardKey{}).(*forward)
			res.Header.Del("X-Request-ID")
			for k := range securityHeaders {
				res.Header.Del(k)
			}
			details := res.Header.Values(audit.DetailsHeader)
			created := res.Header.Values(audit.CreatedHeader)
			for k := range res.Header {
				if hasPrefixFold(k, "Access-Control-") || hasPrefixFold(k, audit.HeaderPrefix) {
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
							"request_id", id, "service", f.res.Service)
					}
				}
				if len(created) > 0 {
					var ok bool
					if st.CreatedID, ok = audit.ParseCreated(created); !ok {
						g.log.Warn("ignoring an audit created id that is not one path segment of ASCII within 128 bytes",
							"request_id", id, "service", f.res.Service)
					}
				}
			}
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			if r.Context().Err() != nil {
				if st := audit.StateFrom(r.Context()); st != nil {
					st.ClientGone = true
				}
				return
			}
			f := r.Context().Value(forwardKey{}).(*forward)
			g.log.Warn("upstream failed", "request_id", apierr.RequestID(r.Context()), "service", f.res.Service, "err", err)
			apierr.BadGateway(w, r)
		},
	}
}

func hasPrefixFold(s, prefix string) bool {
	return len(s) >= len(prefix) && strings.EqualFold(s[:len(prefix)], prefix)
}

func kindHeader(kind string) string {
	return http.CanonicalHeaderKey(plat5Prefix + strings.ReplaceAll(kind, "_", "-") + "-Id")
}

type resolved struct {
	chain     []identity.Node
	principal string
	err       error
}

func (r resolved) found() []identity.Node {
	if errors.Is(r.err, identity.ErrUnavailable) {
		return nil
	}
	return r.chain
}

func (g *Gateway) resolve(ctx context.Context, id string, res *routes.Resolution, member string) resolved {
	var out resolved
	if res == nil || res.NodeID == "" {
		return out
	}
	if g.identity == nil {
		out.err = identity.ErrUnavailable
		return out
	}
	out.chain, out.err = g.identity.Chain(ctx, res.NodeID, id)
	if out.err != nil || member == "" {
		return out
	}
	out.principal, out.err = g.identity.Member(ctx, out.chain[0].ID, member, id)
	return out
}

func parseAsMember(h http.Header) (string, error) {
	vals := h.Values(asMemberHeader)
	if len(vals) == 0 {
		return "", nil
	}
	if len(vals) > 1 {
		return "", errBadAsMember
	}
	v := strings.TrimSpace(vals[0])
	if !httpx.ValidSegmentID(v, maxMemberIDLen) {
		return "", errBadAsMember
	}
	return v, nil
}

func parseReason(h http.Header) (string, error) {
	vals := h.Values(reasonHeader)
	if len(vals) == 0 {
		return "", nil
	}
	if len(vals) > 1 {
		return "", errBadReason
	}
	v := strings.TrimSpace(vals[0])
	if !validReason(v) {
		return "", errBadReason
	}
	return v, nil
}

func validReason(v string) bool {
	if v == "" || !utf8.ValidString(v) || utf8.RuneCountInString(v) > audit.MaxReasonChars {
		return false
	}
	for _, r := range v {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	id := httpx.NewRequestID()
	r = r.WithContext(apierr.WithRequestID(r.Context(), id))
	rec := &httpx.Recorder{ResponseWriter: w}
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
		op       *auth.Operator
		match    *routes.Resolution
		asMember string
		reason   string
		res      resolved
		state    *audit.State
	)
	defer func() {
		rv := recover()
		if rv != nil && rv != http.ErrAbortHandler {
			g.log.Error("panic", "request_id", id, "panic", rv)
			if rec.Status == 0 {
				apierr.Internal(w, r)
			}
		}
		if state != nil {
			g.audit.SendOutcome(id, audit.OutcomeFor(state, rec.Status, rec.Bytes))
		}
		g.logRequest(r, id, op, match, asMember, reason, res, rec.Status, time.Since(start))
		if rv == http.ErrAbortHandler {
			panic(rv)
		}
	}()

	if r.URL.EscapedPath() == "" {
		apierr.InvalidRequest(w, r)
		return
	}

	// 1. Preflight from an allowed origin is answered here, never forwarded.
	if r.Method == http.MethodOptions && allowedOrigin && r.Header.Get("Access-Control-Request-Method") != "" {
		h.Set("Access-Control-Allow-Methods", preflightMethods)
		h.Set("Access-Control-Allow-Headers", preflightHeaders)
		h.Set("Access-Control-Max-Age", "600")
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// 2. Authenticate before anything about the service list is revealed.
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

	ip := clientIP(r, g.trustedProxies)

	// 3. Advisory UI batch route checks (not audited, never forwards).
	if r.URL.EscapedPath() == "/access/routes" {
		if r.Method != http.MethodPost {
			apierr.NotFound(w, r)
			return
		}
		g.handleAccessRoutes(w, r, id, ip, op)
		return
	}

	// 4. Parse /services/{service}/{path...}, match template (optional), and read headers.
	var pathErr error
	match, pathErr = g.registry.Resolve(r.Method, r.URL.EscapedPath())
	var asMemberErr, reasonErr error
	asMember, asMemberErr = parseAsMember(r.Header)
	reason, reasonErr = parseReason(r.Header)
	if match != nil && asMemberErr == nil && asMember != "" && match.NodeID == "" {
		// Acting as a member requires a route node so membership can be verified in its tree.
		asMemberErr = errBadAsMember
	}

	// 5. Resolve node chain and acting-as member.
	resolveMember := ""
	if asMemberErr == nil {
		resolveMember = asMember
	}
	res = g.resolve(r.Context(), id, match, resolveMember)

	// 6. Nothing past authentication answers until the audit intent is written.
	if g.audit != nil {
		in := intent(r, id, start, ip, op, match, asMember, reason, res)
		state = &audit.State{OccurredAt: in.OccurredAt}
		r = r.WithContext(audit.WithState(r.Context(), state))
		if err := g.audit.WriteIntent(r.Context(), in); err != nil {
			apierr.Unavailable(w, r)
			return
		}
	}

	// 7. Answer what can't be forwarded.
	switch {
	case pathErr != nil:
		apierr.InvalidRequest(w, r)
		return
	case match == nil:
		apierr.NotFound(w, r)
		return
	case asMemberErr != nil, reasonErr != nil:
		apierr.InvalidRequest(w, r)
		return
	case errors.Is(res.err, identity.ErrNotFound):
		apierr.NotFoundResource(w, r, "node", match.NodeID)
		return
	case errors.Is(res.err, identity.ErrNotActive):
		apierr.InvalidRequest(w, r)
		return
	case res.err != nil:
		g.log.Error("identity unavailable", "request_id", id, "err", res.err)
		apierr.Unavailable(w, r)
		return
	}

	// 8. Authorize live via AuthZEN (fail closed, no cache).
	if authz := g.currentAuthorizer(); authz != nil {
		authzReq := buildAuthZENRequest(op, match, r.Method, res, asMember, reason, ip, id)
		decision, err := authz.Evaluate(r.Context(), authzReq)
		if err != nil {
			g.log.Error("authorizer unavailable", "request_id", id, "service", match.Service, "err", err)
			apierr.Unavailable(w, r)
			return
		}
		if !decision.Allowed {
			if state != nil && decision.Reason != "" {
				if raw, err := json.Marshal(map[string]string{"policy_reason": decision.Reason}); err == nil && len(raw) <= audit.MaxDetailsBytes {
					state.Details = raw
				}
			}
			apierr.Forbidden(w, r, decision.Reason)
			return
		}
	}

	// 9. Forward.
	f := &forward{res: match, operatorID: op.ID, chain: res.chain}
	if asMember != "" {
		f.member, f.principal = asMember, res.principal
	}
	r = r.WithContext(context.WithValue(r.Context(), forwardKey{}, f))
	if state != nil {
		state.Forwarded = true
	}
	g.proxy.ServeHTTP(w, r)
}

func (g *Gateway) unauthorized(w http.ResponseWriter, r *http.Request, reason string) {
	g.unauthenticated.Inc(reason)
	apierr.Unauthorized(w, r, reason)
}

func buildAuthZENSubject(op *auth.Operator) authorizer.Subject {
	props := map[string]any{"issuer": op.Issuer}
	if op.Email != "" {
		props["email"] = op.Email
	}
	if op.ClientID != "" {
		props["client_id"] = op.ClientID
	}
	if len(op.Claims) > 0 {
		props["claims"] = op.Claims
	}
	return authorizer.Subject{
		Type:       "operator",
		ID:         op.ID,
		Properties: props,
	}
}

func buildAuthZENAction(method string, m *routes.Resolution, asMember, principal, reason string) authorizer.Action {
	props := map[string]any{
		"service": m.Service,
		"method":  method,
		"path":    m.UpstreamPath,
	}
	if m.Route != "" {
		props["route"] = m.Route
	}
	if asMember != "" {
		props["acting_as"] = asMember
		if principal != "" {
			props["acting_as_principal"] = principal
		}
	}
	if reason != "" {
		props["reason"] = reason
	}
	return authorizer.Action{Name: method, Properties: props}
}

func buildAuthZENResource(m *routes.Resolution, chain []identity.Node) authorizer.Resource {
	if len(chain) > 0 {
		leaf := chain[len(chain)-1]
		nodes := make([]map[string]string, len(chain))
		props := map[string]any{
			"service": m.Service,
			"root_id": chain[0].ID,
		}
		for i, n := range chain {
			nodes[i] = map[string]string{"kind": n.Kind, "id": n.ID}
			props[n.Kind+"_id"] = n.ID
		}
		props["nodes"] = nodes
		if len(m.Params) > 0 {
			props["params"] = m.Params
		}
		if len(m.Permissions) > 0 {
			props["plat5_permissions"] = m.Permissions
		}
		return authorizer.Resource{Type: leaf.Kind, ID: leaf.ID, Properties: props}
	}
	props := map[string]any{
		"service": m.Service,
		"path":    m.UpstreamPath,
	}
	if m.Route != "" {
		props["route"] = m.Route
	}
	if len(m.Params) > 0 {
		props["params"] = m.Params
	}
	if len(m.Permissions) > 0 {
		props["plat5_permissions"] = m.Permissions
	}
	return authorizer.Resource{Type: "service", ID: m.Service, Properties: props}
}

func buildAuthZENRequest(op *auth.Operator, m *routes.Resolution, method string, res resolved, asMember, reason, ip, reqID string) authorizer.Request {
	return authorizer.Request{
		Subject:  buildAuthZENSubject(op),
		Action:   buildAuthZENAction(method, m, asMember, res.principal, reason),
		Resource: buildAuthZENResource(m, res.found()),
		Context: map[string]any{
			"client_ip":  ip,
			"request_id": reqID,
		},
	}
}

func intent(r *http.Request, id string, at time.Time, ip string, op *auth.Operator, m *routes.Resolution, member, reason string, res resolved) *audit.Intent {
	in := &audit.Intent{
		RequestID:  id,
		OccurredAt: audit.Timestamp(at),
		Actor: audit.Actor{
			Issuer:     audit.TruncateBytes(op.Issuer, audit.MaxActorBytes),
			OperatorID: audit.TruncateBytes(op.ID, audit.MaxActorBytes),
			Email:      optional(op.Email, audit.MaxActorBytes),
			ClientID:   optional(op.ClientID, audit.MaxActorBytes),
		},
		Nodes:     []audit.Node{},
		Method:    r.Method,
		Params:    map[string]string{},
		IP:        audit.TruncateBytes(ip, audit.MaxIPLen),
		UserAgent: audit.UserAgent(r.UserAgent()),
	}
	if reason != "" {
		rVal := reason
		in.Reason = &rVal
	}
	if m == nil {
		path := audit.Path(r.URL.EscapedPath())
		in.Path = &path
		return in
	}
	service := m.Service
	in.Service = &service
	if m.Route == "" {
		// Known service, unlisted path.
		path := audit.Path(m.UpstreamPath)
		in.Path = &path
		return in
	}
	template := m.Route
	in.Route = &template
	for k, v := range m.Params {
		in.Params[k] = audit.TruncateBytes(v, audit.MaxParamValueBytes)
	}
	if chain := res.found(); len(chain) > 0 {
		for _, n := range chain {
			in.Nodes = append(in.Nodes, audit.Node{Kind: n.Kind, ID: n.ID})
		}
		root := chain[0].ID
		in.RootID = &root
	}
	if res.principal != "" {
		in.ActingAs = &audit.ActingAs{MemberID: member, Principal: res.principal}
	}
	return in
}

func optional(s string, maxBytes int) *string {
	if s == "" {
		return nil
	}
	s = audit.TruncateBytes(s, maxBytes)
	return &s
}

func (g *Gateway) logRequest(r *http.Request, id string, op *auth.Operator, m *routes.Resolution, member, reason string, res resolved, status int, d time.Duration) {
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
	if reason != "" {
		attrs = append(attrs, "reason", reason)
	}
	if m != nil {
		attrs = append(attrs, "service", m.Service)
		if m.Route != "" {
			attrs = append(attrs, "route", m.Route)
		} else {
			attrs = append(attrs, "path", m.UpstreamPath)
		}
		if len(m.Params) > 0 {
			attrs = append(attrs, "params", m.Params)
		}
		if chain := res.found(); len(chain) > 0 {
			attrs = append(attrs, "root_id", chain[0].ID, "node_id", chain[len(chain)-1].ID)
		}
		if res.principal != "" {
			attrs = append(attrs, "member_id", member)
		}
	}
	g.log.Info("request", attrs...)
}
