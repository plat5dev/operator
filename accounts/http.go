package accounts

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/plat5dev/operator/internal/apierr"
)

const SessionCookie = "operator_session"

func LoginHandler(store *Store, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&body); err != nil {
			apierr.Invalid(w)
			return
		}
		id, token, err := store.LoginOperator(body.Email, body.Password)
		if IsUnauthorized(err) {
			apierr.Unauthorized(w)
			return
		}
		if err != nil {
			log.Error("login", "request_id", apierr.From(r.Context()), "err", err)
			apierr.Internal(w)
			return
		}
		setSessionCookie(w, r, token)
		log.Info("login", "request_id", apierr.From(r.Context()), "operator_id", id)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"token": token})
	})
}

func LogoutHandler(store *Store, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := TokenFromRequest(r)
		if ok {
			id, authed, err := store.Authenticate(token)
			if err != nil {
				log.Error("logout", "request_id", apierr.From(r.Context()), "err", err)
				apierr.Internal(w)
				return
			}
			if err := store.Revoke(token); err != nil {
				log.Error("logout", "request_id", apierr.From(r.Context()), "err", err)
				apierr.Internal(w)
				return
			}
			if authed {
				log.Info("logout", "request_id", apierr.From(r.Context()), "operator_id", id)
			}
		}
		clearSessionCookie(w, r)
		w.WriteHeader(http.StatusNoContent)
	})
}

func SessionHandler(store *Store, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := operatorID(w, r, store, log)
		if !ok {
			return
		}
		email, ok, err := store.Email(id)
		if err != nil {
			log.Error("session", "request_id", apierr.From(r.Context()), "err", err)
			apierr.Internal(w)
			return
		}
		if !ok {
			apierr.Unauthorized(w)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"id": id, "email": email})
	})
}

func PasswordHandler(store *Store, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := operatorID(w, r, store, log)
		if !ok {
			return
		}
		var body struct {
			CurrentPassword string `json:"current_password"`
			Password        string `json:"password"`
		}
		dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&body); err != nil {
			apierr.Invalid(w)
			return
		}
		token, err := store.ChangePassword(id, body.CurrentPassword, body.Password)
		if IsInvalid(err) {
			apierr.Validation(w, "Enter a new password.", "password")
			return
		}
		if IsUnauthorized(err) {
			apierr.Validation(w, "Current password is wrong.", "current_password")
			return
		}
		if err != nil {
			log.Error("password", "request_id", apierr.From(r.Context()), "err", err)
			apierr.Internal(w)
			return
		}
		setSessionCookie(w, r, token)
		log.Info("password", "request_id", apierr.From(r.Context()), "operator_id", id)
		w.WriteHeader(http.StatusNoContent)
	})
}

func operatorID(w http.ResponseWriter, r *http.Request, store *Store, log *slog.Logger) (string, bool) {
	token, ok := TokenFromRequest(r)
	if !ok {
		apierr.Unauthorized(w)
		return "", false
	}
	id, ok, err := store.Authenticate(token)
	if err != nil {
		log.Error("authn", "request_id", apierr.From(r.Context()), "err", err)
		apierr.Internal(w)
		return "", false
	}
	if !ok || id == "" {
		apierr.Unauthorized(w)
		return "", false
	}
	return id, true
}

func TokenFromRequest(r *http.Request) (string, bool) {
	if header := r.Header.Get("Authorization"); header != "" {
		return bearer(header)
	}
	c, err := r.Cookie(SessionCookie)
	if err != nil {
		return "", false
	}
	token := c.Value
	if token == "" || strings.ContainsAny(token, " \r\n") {
		return "", false
	}
	return token, true
}

func bearer(header string) (string, bool) {
	parts := strings.SplitN(strings.TrimSpace(header), " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "", false
	}
	token := strings.TrimSpace(parts[1])
	if token == "" || strings.ContainsAny(token, " \r\n") {
		return "", false
	}
	return token, true
}

func setSessionCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, sessionCookie(r, token, int(tokenTTL.Seconds())))
}

func clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, sessionCookie(r, "", -1))
}

func sessionCookie(r *http.Request, token string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name:     SessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil,
		MaxAge:   maxAge,
	}
}
