// Package apierr writes the Plat5 error envelope.
package apierr

import (
	"context"
	"encoding/json"
	"net/http"
)

type ctxKey struct{}

// WithRequestID stores the request id that every error carries.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}

// RequestID returns the id stored by WithRequestID, or "".
func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(ctxKey{}).(string)
	return id
}

type envelope struct {
	Error body `json:"error"`
}

type body struct {
	Type      string `json:"type"`
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
	Details   any    `json:"details"`
}

func write(w http.ResponseWriter, r *http.Request, status int, typ, code, message string, details any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(envelope{Error: body{
		Type:      typ,
		Code:      code,
		Message:   message,
		RequestID: RequestID(r.Context()),
		Details:   details,
	}})
}

// Unauthorized is a missing or invalid staff token. reason is missing, invalid, or expired.
func Unauthorized(w http.ResponseWriter, r *http.Request, reason string) {
	w.Header().Set("WWW-Authenticate", "Bearer")
	write(w, r, http.StatusUnauthorized, "invalid_request_error", "UNAUTHORIZED", "Authentication required.", map[string]string{"reason": reason})
}

func InvalidRequest(w http.ResponseWriter, r *http.Request) {
	write(w, r, http.StatusBadRequest, "invalid_request_error", "INVALID_REQUEST", "Malformed request.", nil)
}

func NotFound(w http.ResponseWriter, r *http.Request) {
	write(w, r, http.StatusNotFound, "invalid_request_error", "NOT_FOUND", "Resource not found.", nil)
}

func Internal(w http.ResponseWriter, r *http.Request) {
	write(w, r, http.StatusInternalServerError, "api_error", "INTERNAL_ERROR", "An unexpected error occurred.", nil)
}

func Unavailable(w http.ResponseWriter, r *http.Request) {
	write(w, r, http.StatusServiceUnavailable, "api_error", "SERVICE_UNAVAILABLE", "Service temporarily unavailable.", nil)
}

// Field is one invalid input, in details.fields.
type Field struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

const validationMessage = "That doesn't look right."

// Validation is 422 VALIDATION_ERROR naming the fields at fault.
func Validation(w http.ResponseWriter, r *http.Request, paths ...string) {
	fields := make([]Field, len(paths))
	for i, p := range paths {
		fields[i] = Field{Path: p, Message: validationMessage}
	}
	write(w, r, http.StatusUnprocessableEntity, "invalid_request_error", "VALIDATION_ERROR", validationMessage, map[string]any{"fields": fields})
}
