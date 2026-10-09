// Package auth validates staff JWTs. Contract: docs/idp.md.
package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

var (
	ErrMissing = errors.New("missing")
	ErrInvalid = errors.New("invalid")
	ErrExpired = errors.New("expired")
	// ErrNoKeys means JWKS has never been fetched. The token cannot be judged.
	ErrNoKeys = errors.New("no keys")
)

const (
	leeway          = 60 * time.Second
	refetchInterval = 30 * time.Second
	refreshInterval = 15 * time.Minute
	maxJWKSBytes    = 1 << 20
)

var algorithms = []jose.SignatureAlgorithm{
	jose.RS256, jose.RS384, jose.RS512,
	jose.ES256, jose.ES384,
	jose.PS256,
}

type Config struct {
	Issuer    string
	JWKSURI   string
	Audiences []string
	// IDClaim is a dotted path into the claims. Default "sub".
	IDClaim string
	Client  *http.Client
}

type Operator struct {
	ID     string
	Email  string
	Issuer string
	// ClientID is the client the token was issued to: azp, else client_id. Empty when neither.
	ClientID string
	Claims   map[string]any
}

type Verifier struct {
	cfg Config
	log *slog.Logger
	now func() time.Time

	ready atomic.Bool

	keysMu sync.RWMutex
	keys   map[string]jose.JSONWebKey

	fetchMu     sync.Mutex
	lastAttempt time.Time
}

func New(cfg Config, log *slog.Logger) *Verifier {
	if cfg.IDClaim == "" {
		cfg.IDClaim = "sub"
	}
	if cfg.Client == nil {
		cfg.Client = &http.Client{Timeout: 10 * time.Second}
	}
	return &Verifier{cfg: cfg, log: log, now: time.Now}
}

// Ready reports whether JWKS has been fetched at least once.
func (v *Verifier) Ready() bool { return v.ready.Load() }

// Run fetches JWKS until the first success, then refreshes it periodically. It returns when ctx ends.
func (v *Verifier) Run(ctx context.Context) {
	backoff := time.Second
	for !v.Ready() {
		if err := v.Refresh(ctx); err != nil {
			v.log.Warn("jwks fetch failed", "err", err, "retry_in", backoff.String())
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			backoff = min(backoff*2, 30*time.Second)
		}
	}
	t := time.NewTicker(refreshInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := v.Refresh(ctx); err != nil {
				v.log.Warn("jwks refresh failed; keeping cached keys", "err", err)
			}
		}
	}
}

// Refresh fetches JWKS now. A failure keeps the cached keys.
func (v *Verifier) Refresh(ctx context.Context) error {
	v.fetchMu.Lock()
	defer v.fetchMu.Unlock()
	return v.refresh(ctx)
}

func (v *Verifier) refresh(ctx context.Context) error {
	v.lastAttempt = v.now()
	keys, err := v.fetch(ctx)
	if err != nil {
		return err
	}
	v.keysMu.Lock()
	v.keys = keys
	v.keysMu.Unlock()
	v.ready.Store(true)
	return nil
}

func (v *Verifier) fetch(ctx context.Context) (map[string]jose.JSONWebKey, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.cfg.JWKSURI, nil)
	if err != nil {
		return nil, err
	}
	res, err := v.cfg.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("jwks: status %d", res.StatusCode)
	}
	var set struct {
		Keys []json.RawMessage `json:"keys"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, maxJWKSBytes)).Decode(&set); err != nil {
		return nil, fmt.Errorf("jwks: %w", err)
	}
	keys := map[string]jose.JSONWebKey{}
	for _, raw := range set.Keys {
		// One key this library cannot read must not drop the rest.
		var k jose.JSONWebKey
		if err := k.UnmarshalJSON(raw); err != nil {
			continue
		}
		if k.KeyID == "" || k.Use == "enc" {
			continue
		}
		pub := k.Public()
		if !pub.Valid() {
			continue
		}
		keys[k.KeyID] = pub
	}
	if len(keys) == 0 {
		return nil, errors.New("jwks: no usable signing keys")
	}
	return keys, nil
}

func (v *Verifier) lookup(kid string) (jose.JSONWebKey, bool) {
	v.keysMu.RLock()
	defer v.keysMu.RUnlock()
	k, ok := v.keys[kid]
	return k, ok
}

// key returns the key for kid. An unknown kid refetches JWKS, at most once per refetchInterval.
func (v *Verifier) key(ctx context.Context, kid string) (jose.JSONWebKey, bool) {
	if k, ok := v.lookup(kid); ok {
		return k, true
	}
	v.fetchMu.Lock()
	defer v.fetchMu.Unlock()
	if k, ok := v.lookup(kid); ok {
		return k, true
	}
	if v.now().Sub(v.lastAttempt) < refetchInterval {
		return jose.JSONWebKey{}, false
	}
	if err := v.refresh(ctx); err != nil {
		v.log.Warn("jwks refetch for unknown kid failed", "err", err)
	}
	return v.lookup(kid)
}

// Verify checks an Authorization header value and returns the operator it names.
func (v *Verifier) Verify(ctx context.Context, header string) (*Operator, error) {
	if header == "" {
		return nil, ErrMissing
	}
	scheme, raw, ok := strings.Cut(header, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || raw == "" || strings.ContainsAny(raw, " \t") {
		return nil, ErrInvalid
	}
	if !v.Ready() {
		return nil, ErrNoKeys
	}
	tok, err := jwt.ParseSigned(raw, algorithms)
	if err != nil || len(tok.Headers) != 1 || tok.Headers[0].KeyID == "" {
		return nil, ErrInvalid
	}
	key, ok := v.key(ctx, tok.Headers[0].KeyID)
	if !ok {
		return nil, ErrInvalid
	}
	var reg jwt.Claims
	var all map[string]any
	if err := tok.Claims(key.Key, &reg, &all); err != nil {
		return nil, ErrInvalid
	}
	if reg.Expiry == nil {
		return nil, ErrInvalid
	}
	err = reg.ValidateWithLeeway(jwt.Expected{
		Issuer:      v.cfg.Issuer,
		AnyAudience: v.cfg.Audiences,
		Time:        v.now(),
	}, leeway)
	if errors.Is(err, jwt.ErrExpired) {
		return nil, ErrExpired
	}
	if err != nil {
		return nil, ErrInvalid
	}
	id, _ := claim(all, v.cfg.IDClaim).(string)
	if id == "" {
		return nil, ErrInvalid
	}
	email, _ := all["email"].(string)
	client, _ := all["azp"].(string)
	if client == "" {
		client, _ = all["client_id"].(string)
	}
	return &Operator{ID: id, Email: email, Issuer: reg.Issuer, ClientID: client, Claims: all}, nil
}

func claim(claims map[string]any, path string) any {
	var cur any = claims
	for _, part := range strings.Split(path, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = m[part]
	}
	return cur
}
