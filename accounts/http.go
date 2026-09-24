package accounts

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"

	"github.com/plat5dev/operator/internal/apierr"
)

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
		log.Info("login", "request_id", apierr.From(r.Context()), "operator_id", id)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"token": token})
	})
}
