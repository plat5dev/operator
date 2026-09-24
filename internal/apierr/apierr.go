package apierr

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

type ctxKey struct{}

type body struct {
	Type      string `json:"type"`
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
	Details   any    `json:"details"`
}

type envelope struct {
	Error body `json:"error"`
}

func NewID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

func With(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}

func From(ctx context.Context) string {
	id, _ := ctx.Value(ctxKey{}).(string)
	return id
}

func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := usableID(r.Header.Get("X-Request-ID"))
		if id == "" {
			id = NewID()
		}
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(With(r.Context(), id)))
	})
}

func usableID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" || len(id) > 128 {
		return ""
	}
	for _, c := range id {
		if c < 0x21 || c > 0x7e {
			return ""
		}
	}
	return id
}

func Write(w http.ResponseWriter, status int, kind, code, message string, details any) {
	w.Header().Set("Content-Type", "application/json")
	rid := ""
	if hw, ok := w.(interface{ Header() http.Header }); ok {
		rid = hw.Header().Get("X-Request-ID")
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(envelope{Error: body{
		Type:      kind,
		Code:      code,
		Message:   message,
		RequestID: rid,
		Details:   details,
	}})
}

func Unauthorized(w http.ResponseWriter) {
	Write(w, http.StatusUnauthorized, "invalid_request_error", "UNAUTHORIZED", "Authentication required.", nil)
}

func NotFound(w http.ResponseWriter) {
	Write(w, http.StatusNotFound, "invalid_request_error", "NOT_FOUND", "Resource not found.", nil)
}

func Unavailable(w http.ResponseWriter) {
	Write(w, http.StatusServiceUnavailable, "api_error", "SERVICE_UNAVAILABLE", "Service temporarily unavailable.", nil)
}

func Invalid(w http.ResponseWriter) {
	Write(w, http.StatusBadRequest, "invalid_request_error", "INVALID_REQUEST", "Malformed request.", nil)
}

func Internal(w http.ResponseWriter) {
	Write(w, http.StatusInternalServerError, "api_error", "INTERNAL_ERROR", "An unexpected error occurred.", nil)
}

func Validation(w http.ResponseWriter, message, field string) {
	Write(w, http.StatusBadRequest, "invalid_request_error", "VALIDATION_ERROR", message, map[string]any{
		"fields": []map[string]string{{"path": field, "message": message}},
	})
}
