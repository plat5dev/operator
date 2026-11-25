// Package httpx holds small HTTP and identifier helpers shared by the operator gateway
// and operator-audit binaries.
package httpx

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"regexp"
	"time"
)

var requestIDRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

// NewTransport returns a pooled HTTP transport with sane dial, TLS, and idle timeouts.
func NewTransport(responseHeaderTimeout time.Duration) *http.Transport {
	return &http.Transport{
		DialContext:           (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: responseHeaderTimeout,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   64,
		IdleConnTimeout:       90 * time.Second,
	}
}

// NewRequestID generates a random 32-hex-character request correlation id.
func NewRequestID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// ValidRequestID reports whether s is a valid X-Request-ID value.
func ValidRequestID(s string) bool {
	return requestIDRe.MatchString(s)
}

// ValidSegmentID reports whether s is a single visible-ASCII path segment within maxLen bytes:
// non-empty, not "." or "..", and free of whitespace, control chars, '/', '\', '?', and '#'.
func ValidSegmentID(s string, maxLen int) bool {
	if s == "" || len(s) > maxLen || s == "." || s == ".." {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] <= 0x20 || s[i] > 0x7e || s[i] == '/' || s[i] == '\\' || s[i] == '?' || s[i] == '#' {
			return false
		}
	}
	return true
}

// WriteStatus writes a small JSON health/status response.
func WriteStatus(w http.ResponseWriter, code int, status string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": status})
}

// Recorder wraps an http.ResponseWriter to capture the final status code and body bytes written.
// Unwrap preserves http.ResponseController / Flusher support for streaming proxies.
type Recorder struct {
	http.ResponseWriter
	Status int
	Bytes  int64
}

func (r *Recorder) WriteHeader(code int) {
	// A 1xx informational response is not the final status.
	if r.Status == 0 && code >= 200 {
		r.Status = code
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *Recorder) Write(b []byte) (int, error) {
	if r.Status == 0 {
		r.Status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(b)
	r.Bytes += int64(n)
	return n, err
}

func (r *Recorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }
