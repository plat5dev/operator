package gateway

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"time"

	"github.com/plat5dev/operator/accounts"
	"github.com/plat5dev/operator/internal/apierr"
)

type Authenticator interface {
	Authenticate(token string) (operatorID string, ok bool, err error)
}

type routeKey struct{}

func Proxy(auth Authenticator, routes []Route, log *slog.Logger) http.Handler {
	transport := &http.Transport{
		Proxy:                 nil,
		DialContext:           (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ResponseHeaderTimeout: 60 * time.Second,
		IdleConnTimeout:       90 * time.Second,
		ForceAttemptHTTP2:     false,
	}
	proxy := &httputil.ReverseProxy{
		Transport: transport,
		Rewrite: func(pr *httputil.ProxyRequest) {
			rt := pr.In.Context().Value(routeKey{}).(matched)
			pr.SetURL(rt.route.Upstream)
			pr.Out.Host = rt.route.Upstream.Host
			h := pr.Out.Header
			h.Del("Authorization")
			h.Del("Cookie")
			h.Del("Proxy-Authorization")
			h.Del("X-Api-Key")
			h.Set("X-Request-ID", apierr.From(pr.In.Context()))
		},
		ModifyResponse: func(resp *http.Response) error {
			if id := apierr.From(resp.Request.Context()); id != "" {
				resp.Header.Set("X-Request-ID", id)
			}
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			log.Error("upstream", "request_id", apierr.From(r.Context()), "err", err)
			apierr.Unavailable(w)
		},
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rid := apierr.From(r.Context())
		token, ok := accounts.TokenFromRequest(r)
		if !ok {
			apierr.Unauthorized(w)
			return
		}
		operatorID, ok, err := auth.Authenticate(token)
		if err != nil {
			log.Error("authn", "request_id", rid, "err", err)
			apierr.Internal(w)
			return
		}
		if !ok || operatorID == "" {
			apierr.Unauthorized(w)
			return
		}
		var rt Route
		var params map[string]string
		found := false
		for _, candidate := range routes {
			if ok, p := candidate.match(r.Method, r.URL.Path); ok {
				rt = candidate
				params = p
				found = true
				break
			}
		}
		if !found {
			log.Info("request", "operator_id", operatorID, "request_id", rid, "method", r.Method, "path", r.URL.Path, "code", "NOT_FOUND")
			apierr.NotFound(w)
			return
		}
		log.Info("request",
			"operator_id", operatorID,
			"request_id", rid,
			"method", r.Method,
			"path", r.URL.Path,
			"target_user_id", params["user_id"],
			"target_organization_id", params["organization_id"],
			"target_member_id", params["member_id"],
		)
		proxy.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), routeKey{}, matched{route: rt})))
	})
}

type matched struct {
	route Route
}
