package gateway

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"strings"
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
			h.Del("X-User-Id")
			h.Del("X-Organization-Id")
			h.Del("X-Member-Id")
			h.Del("X-Operator-Id")
			if rt.userID != "" {
				h.Set("X-User-Id", rt.userID)
			}
			if rt.orgID != "" {
				h.Set("X-Organization-Id", rt.orgID)
			}
			if rt.memberID != "" {
				h.Set("X-Member-Id", rt.memberID)
			}
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
		userID, userOK := cleanHeader(r.Header.Get("X-User-Id"))
		orgID, orgOK := cleanHeader(r.Header.Get("X-Organization-Id"))
		memberID, memberOK := cleanHeader(r.Header.Get("X-Member-Id"))
		if !userOK || !orgOK || !memberOK {
			log.Info("request", "operator_id", operatorID, "request_id", rid, "method", r.Method, "path", r.URL.Path, "code", "VALIDATION_ERROR")
			apierr.Validation(w, "That doesn't look right.", "target")
			return
		}
		injected := matched{route: rt}
		switch rt.Requires {
		case RequiresUser:
			if userID == "" {
				log.Info("request", "operator_id", operatorID, "request_id", rid, "method", r.Method, "path", r.URL.Path, "code", "VALIDATION_ERROR")
				apierr.Validation(w, "Name the user this call applies to.", "X-User-Id")
				return
			}
			injected.userID = userID
		case RequiresOrganization:
			if orgID == "" || memberID == "" {
				field := "X-Organization-Id"
				if orgID != "" {
					field = "X-Member-Id"
				}
				log.Info("request", "operator_id", operatorID, "request_id", rid, "method", r.Method, "path", r.URL.Path, "code", "VALIDATION_ERROR")
				apierr.Validation(w, "Name the organization and member this call applies to.", field)
				return
			}
			injected.orgID = orgID
			injected.memberID = memberID
		}
		if pathOrg, has := params["organization_id"]; has && pathOrg != orgID {
			log.Info("request", "operator_id", operatorID, "request_id", rid, "method", r.Method, "path", r.URL.Path, "code", "VALIDATION_ERROR")
			apierr.Validation(w, "The organization in the path does not match the target.", "X-Organization-Id")
			return
		}
		log.Info("request",
			"operator_id", operatorID,
			"request_id", rid,
			"method", r.Method,
			"path", r.URL.Path,
			"target_user_id", injected.userID,
			"target_organization_id", injected.orgID,
			"target_member_id", injected.memberID,
		)
		proxy.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), routeKey{}, injected)))
	})
}

type matched struct {
	route    Route
	userID   string
	orgID    string
	memberID string
}

func cleanHeader(v string) (string, bool) {
	v = strings.TrimSpace(v)
	if strings.ContainsAny(v, "\r\n\x00") {
		return "", false
	}
	return v, true
}
