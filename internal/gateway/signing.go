package gateway

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	SignatureHeader  = "X-Plat5-Signature"
	signatureVersion = "v1"
)

// Signer signs outgoing upstream requests with Ed25519 using Plat5's v1
// X-Plat5-Signature format (docs/reference/headers.md#to-services).
type Signer struct {
	key ed25519.PrivateKey
	kid string
}

// NewSignerFromSeed constructs a Signer from a base64url-no-pad 32-byte Ed25519
// seed and optional kid. When kid is empty, it defaults to the first 16 lowercase
// hex chars of SHA-256(public_key).
func NewSignerFromSeed(seedB64URL, kid string) (*Signer, error) {
	seed, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(seedB64URL))
	if err != nil {
		return nil, fmt.Errorf("UPSTREAM_SIGNING_KEY must be base64url without padding: %w", err)
	}
	if len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("UPSTREAM_SIGNING_KEY must decode to %d bytes, got %d", ed25519.SeedSize, len(seed))
	}
	key := ed25519.NewKeyFromSeed(seed)
	kid = strings.TrimSpace(kid)
	if kid == "" {
		kid = defaultKID(key.Public().(ed25519.PublicKey))
	} else if err := ValidateKID(kid); err != nil {
		return nil, fmt.Errorf("UPSTREAM_SIGNING_KEY_ID %w", err)
	}
	return &Signer{key: key, kid: kid}, nil
}

// KID returns the key identifier placed in X-Plat5-Signature.
func (s *Signer) KID() string {
	return s.kid
}

// SignNow returns the X-Plat5-Signature header value using the current UTC time.
func (s *Signer) SignNow(method, pathAndQuery, requestID string, headers http.Header) string {
	return s.Sign(method, pathAndQuery, requestID, headers, time.Now().Unix())
}

// Sign returns the X-Plat5-Signature header value for an explicit UTC timestamp.
func (s *Signer) Sign(method, pathAndQuery, requestID string, headers http.Header, timestamp int64) string {
	payload := CanonicalPayload(s.kid, timestamp, method, pathAndQuery, requestID, headers)
	sig := ed25519.Sign(s.key, []byte(payload))
	return fmt.Sprintf("%s,kid=%s,t=%d,sig=%s",
		signatureVersion, s.kid, timestamp, base64.RawURLEncoding.EncodeToString(sig))
}

// CanonicalPayload builds the exact UTF-8 string signed by X-Plat5-Signature:
//
//	v1\n
//	<kid>\n
//	<t>\n
//	<METHOD>\n
//	<path_and_query>\n
//	<X-Request-ID>\n
//	<sorted x-plat5-* headers as lowercase-name:value\n>
func CanonicalPayload(kid string, timestamp int64, method, pathAndQuery, requestID string, headers http.Header) string {
	type pair struct {
		name, value string
	}
	var pairs []pair
	for k, vals := range headers {
		lower := strings.ToLower(k)
		if !strings.HasPrefix(lower, "x-plat5-") || lower == "x-plat5-signature" {
			continue
		}
		for _, v := range vals {
			pairs = append(pairs, pair{name: lower, value: v})
		}
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].name != pairs[j].name {
			return pairs[i].name < pairs[j].name
		}
		return pairs[i].value < pairs[j].value
	})

	var b strings.Builder
	b.Grow(128 + len(pairs)*48)
	b.WriteString(signatureVersion)
	b.WriteByte('\n')
	b.WriteString(kid)
	b.WriteByte('\n')
	b.WriteString(strconv.FormatInt(timestamp, 10))
	b.WriteByte('\n')
	b.WriteString(strings.ToUpper(method))
	b.WriteByte('\n')
	b.WriteString(pathAndQuery)
	b.WriteByte('\n')
	b.WriteString(requestID)
	b.WriteByte('\n')
	for _, p := range pairs {
		b.WriteString(p.name)
		b.WriteByte(':')
		b.WriteString(p.value)
		b.WriteByte('\n')
	}
	return b.String()
}

// ValidateKID checks that kid is 1–64 bytes of [A-Za-z0-9._-].
func ValidateKID(kid string) error {
	if len(kid) == 0 || len(kid) > 64 {
		return errors.New("must be 1 to 64 characters")
	}
	for i := 0; i < len(kid); i++ {
		c := kid[i]
		ok := (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') ||
			c == '.' || c == '_' || c == '-'
		if !ok {
			return errors.New("may contain only ASCII letters, digits, '.', '_' and '-'")
		}
	}
	return nil
}

func defaultKID(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	return hex.EncodeToString(sum[:8])
}
