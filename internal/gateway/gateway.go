// Package gateway is the operator front door: authenticate, match, strip, forward, log.
// Contract: docs/model.md.
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
	"regexp"
	"strings"
	"time"

	"github.com/plat5dev/operator/internal/apierr"
	"github.com/plat5dev/operator/internal/auth"
	"github.com/plat5dev/operator/internal/routes"
)

// Authenticator turns an Authorization header into an operator. Errors are the auth package's.
type Authenticator interface {
	Verify(ctx context.Context, header string) (*auth.Operator, error)
}

type Config struct {
	Routes          *routes.Table
	Auth            Authenticator
	AllowedOrigins  []string
	UpstreamTimeout time.Duration
	Log             *slog.Logger
}

type Gateway struct {
	routes  *routes.Table
	auth    Authenticator
	origins map[string]bool
	proxies map[*routes.Route]*httputil.ReverseProxy
	log     *slog.Logger
}

// Credentials the upstream never sees.
var stripped = []string{"Authorization", "Cookie", "X-API-Key"}

var requestIDRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

const (
	preflightMethods = "GET, HEAD, POST, PUT, PATCH, DELETE"
	preflightHeaders = "Authorization, Content-Type, X-Request-ID, traceparent"
)

func New(cfg Config) *Gateway {
	g := &Gateway{
		routes:  cfg.Routes,
		auth:    cfg.Auth,
		origins: map[string]bool{},
		proxies: map[*routes.Route]*httputil.ReverseProxy{},
		log:     cfg.Log,
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
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			g.log.Warn("upstream failed", "request_id", apierr.RequestID(r.Context()), "upstream", route.Upstream(), "err", err)
			apierr.Unavailable(w, r)
		},
	}
}

func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	id := r.Header.Get("X-Request-ID")
	if !requestIDRe.MatchString(id) {
		id = newRequestID()
	}
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
	)
	defer func() {
		rv := recover()
		if rv != nil && rv != http.ErrAbortHandler {
			g.log.Error("panic", "request_id", id, "panic", rv)
			if rec.status == 0 {
				apierr.Internal(w, r)
			}
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
		apierr.Unauthorized(w, r, "missing")
		return
	case errors.Is(err, auth.ErrExpired):
		apierr.Unauthorized(w, r, "expired")
		return
	case err != nil:
		apierr.Unauthorized(w, r, "invalid")
		return
	}

	// 3. Refuse paths the service could read differently.
	segs, err := routes.Segments(r.URL.EscapedPath())
	if err != nil {
		apierr.InvalidRequest(w, r)
		return
	}

	// 4. Match.
	route, params = g.routes.Match(r.Method, segs)
	if route == nil {
		apierr.NotFound(w, r)
		return
	}

	// 6. Forward. Step 5, authz, is slice 2.
	g.proxies[route].ServeHTTP(w, r)
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

// recorder keeps the status for the attribution log. Unwrap lets the proxy flush.
type recorder struct {
	http.ResponseWriter
	status int
}

func (r *recorder) WriteHeader(code int) {
	if r.status == 0 {
		r.status = code
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *recorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.ResponseWriter.Write(b)
}

func (r *recorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }
