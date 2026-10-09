package events

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Limits on what the gateway sends (docs/audit.md#event). The gateway truncates or
// cleans before it sends, so a value over a limit is a bug and gets 422.
const (
	maxRequestIDLen  = 128
	maxActorLen      = 1024
	maxUpstreamLen   = 128
	maxMethodLen     = 32
	maxRouteLen      = 2048
	maxPathChars     = 2048
	maxParams        = 32
	maxParamNameLen  = 64
	maxParamValueLen = 4096
	maxIPLen         = 256
	maxUserAgentLen  = 512 // characters
	maxDetailsBytes  = 4096
	// An intent is written as the request arrives. Far from now is a bug, and would
	// ask for a partition nobody maintains.
	maxClockSkew = 24 * time.Hour

	defaultListLimit = 50
	maxListLimit     = 100
)

var (
	upstreamRe  = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	paramNameRe = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	requestIDRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)
	// An HTTP method is an RFC 9110 token.
	methodRe = regexp.MustCompile("^[!#$%&'*+.^_`|~0-9A-Za-z-]{1,32}$")
)

// Invalid lists the fields at fault. It is a 422.
type Invalid []string

func (v Invalid) Error() string { return "invalid: " + strings.Join(v, ", ") }

func (v *Invalid) add(path string) { *v = append(*v, path) }

func (v Invalid) err() error {
	if len(v) == 0 {
		return nil
	}
	return v
}

type intentBody struct {
	OccurredAt *string           `json:"occurred_at"`
	Actor      *actorBody        `json:"actor"`
	Upstream   *string           `json:"upstream"`
	Method     *string           `json:"method"`
	Route      *string           `json:"route"`
	Params     map[string]string `json:"params"`
	Path       *string           `json:"path"`
	IP         *string           `json:"ip"`
	UserAgent  *string           `json:"user_agent"`
}

type actorBody struct {
	Issuer     *string `json:"issuer"`
	OperatorID *string `json:"operator_id"`
	Email      *string `json:"email"`
	ClientID   *string `json:"client_id"`
}

type outcomeBody struct {
	Outcome       *string         `json:"outcome"`
	Status        *int            `json:"status"`
	ResponseBytes *int64          `json:"response_bytes"`
	Decision      json.RawMessage `json:"decision"`
	Details       json.RawMessage `json:"details"`
}

// decode unmarshals body into dst. A type error names its field. Unknown fields are
// ignored, so a newer gateway can send more.
func decode(body []byte, dst any) error {
	if err := json.Unmarshal(body, dst); err != nil {
		var typeErr *json.UnmarshalTypeError
		if errors.As(err, &typeErr) && typeErr.Field != "" {
			return Invalid{typeErr.Field}
		}
		return Invalid{"body"}
	}
	return nil
}

// ParseIntent checks an intent body for requestID against now.
func ParseIntent(requestID string, body []byte, now time.Time) (*Intent, error) {
	if !requestIDRe.MatchString(requestID) {
		return nil, Invalid{"request_id"}
	}
	var req intentBody
	if err := decode(body, &req); err != nil {
		return nil, err
	}

	var bad Invalid
	in := &Intent{RequestID: requestID, Params: map[string]string{}}

	switch t, ok := parseTime(req.OccurredAt); {
	case !ok, t.Before(now.Add(-maxClockSkew)), t.After(now.Add(maxClockSkew)):
		bad.add("occurred_at")
	default:
		// The event is to the millisecond, and so is its id.
		in.OccurredAt = t.UTC().Truncate(time.Millisecond)
	}

	if req.Actor == nil {
		bad.add("actor")
	} else {
		in.Actor.Issuer = text(&bad, "actor.issuer", req.Actor.Issuer, maxActorLen)
		in.Actor.OperatorID = text(&bad, "actor.operator_id", req.Actor.OperatorID, maxActorLen)
		in.Actor.Email = optionalText(&bad, "actor.email", req.Actor.Email, maxActorLen)
		in.Actor.ClientID = optionalText(&bad, "actor.client_id", req.Actor.ClientID, maxActorLen)
	}

	if req.Method == nil || !methodRe.MatchString(*req.Method) {
		bad.add("method")
	} else {
		in.Method = *req.Method
	}

	// A matched request names its upstream and route. An unmatched one has only its path.
	switch {
	case req.Route != nil && req.Path == nil:
		if req.Upstream == nil || len(*req.Upstream) > maxUpstreamLen || !upstreamRe.MatchString(*req.Upstream) {
			bad.add("upstream")
		} else {
			in.Upstream = req.Upstream
		}
		if r := *req.Route; r == "" || r[0] != '/' || len(r) > maxRouteLen || !storable(r) {
			bad.add("route")
		} else {
			in.Route = req.Route
		}
	case req.Route == nil && req.Path != nil:
		if req.Upstream != nil {
			bad.add("upstream")
		}
		if p := *req.Path; p == "" || utf8.RuneCountInString(p) > maxPathChars || !storable(p) {
			bad.add("path")
		} else {
			in.Path = req.Path
		}
		if len(req.Params) > 0 {
			bad.add("params")
		}
	default:
		bad.add("route")
		bad.add("path")
	}

	if len(req.Params) > maxParams {
		bad.add("params")
	}
	for name, value := range req.Params {
		if len(name) > maxParamNameLen || !paramNameRe.MatchString(name) ||
			value == "" || len(value) > maxParamValueLen || !storable(value) {
			bad.add("params." + name)
			continue
		}
		in.Params[name] = value
	}

	in.IP = text(&bad, "ip", req.IP, maxIPLen)
	if req.UserAgent != nil {
		if !storable(*req.UserAgent) || utf8.RuneCountInString(*req.UserAgent) > maxUserAgentLen {
			bad.add("user_agent")
		} else {
			in.UserAgent = req.UserAgent
		}
	}

	if err := bad.err(); err != nil {
		return nil, err
	}
	return in, nil
}

// ParseOutcome checks an outcome body. Pending is not an outcome.
func ParseOutcome(requestID string, body []byte) (*Outcome, error) {
	if !requestIDRe.MatchString(requestID) {
		return nil, Invalid{"request_id"}
	}
	var req outcomeBody
	if err := decode(body, &req); err != nil {
		return nil, err
	}

	var bad Invalid
	out := &Outcome{}
	switch {
	case req.Outcome == nil:
		bad.add("outcome")
	case *req.Outcome == OutcomeRejected, *req.Outcome == OutcomeResponded, *req.Outcome == OutcomeNoResponse:
		out.Outcome = *req.Outcome
	default:
		bad.add("outcome")
	}

	switch {
	case req.Status == nil:
		// The client was gone before an answer. A service that answered has a status.
		if out.Outcome == OutcomeResponded {
			bad.add("status")
		}
	case *req.Status < 100 || *req.Status > 599:
		bad.add("status")
	default:
		out.Status = req.Status
	}

	if req.ResponseBytes == nil || *req.ResponseBytes < 0 {
		bad.add("response_bytes")
	} else {
		out.ResponseBytes = *req.ResponseBytes
	}

	// Reserved for authz (slice 3).
	if !isNull(req.Decision) {
		bad.add("decision")
	}

	if !isNull(req.Details) {
		details := bytes.TrimSpace(req.Details)
		// Details come from the upstream response, so only a response has them.
		// Postgres cannot store a NUL in jsonb.
		if out.Outcome != OutcomeResponded || len(details) > maxDetailsBytes || details[0] != '{' ||
			!json.Valid(details) || bytes.Contains(details, []byte(`\u0000`)) {
			bad.add("details")
		} else {
			out.Details = json.RawMessage(details)
		}
	}

	if err := bad.err(); err != nil {
		return nil, err
	}
	return out, nil
}

var listOutcomes = map[string]bool{
	OutcomePending: true, OutcomeRejected: true, OutcomeResponded: true, OutcomeNoResponse: true,
}

// ParseFilter reads the list query. Unknown, repeated, or malformed params are invalid
// (docs/audit.md#reading).
func ParseFilter(rawQuery string) (Filter, error) {
	f := Filter{Limit: defaultListLimit}
	q, err := url.ParseQuery(rawQuery)
	if err != nil {
		return Filter{}, Invalid{"query"}
	}
	var bad Invalid
	for key, values := range q {
		if len(values) != 1 {
			bad.add(key)
			continue
		}
		value := strings.TrimSpace(values[0])

		if name, ok := paramFilterName(key); ok {
			if len(name) > maxParamNameLen || !paramNameRe.MatchString(name) ||
				value == "" || len(value) > maxParamValueLen || !storable(value) {
				bad.add(key)
				continue
			}
			if f.Params == nil {
				f.Params = map[string]string{}
			}
			f.Params[name] = value
			continue
		}

		switch key {
		case "limit":
			n, err := strconv.Atoi(value)
			if err != nil || n < 1 {
				bad.add(key)
				continue
			}
			f.Limit = min(n, maxListLimit)
		case "starting_after":
			if !validID(value) {
				bad.add(key)
				continue
			}
			f.StartingAfter = value
		case "operator_id":
			f.OperatorID = text(&bad, key, &value, maxActorLen)
		case "upstream":
			if !upstreamRe.MatchString(value) || len(value) > maxUpstreamLen {
				bad.add(key)
				continue
			}
			f.Upstream = value
		case "method":
			if !methodRe.MatchString(value) {
				bad.add(key)
				continue
			}
			f.Method = value
		case "route":
			f.Route = text(&bad, key, &value, maxRouteLen)
		case "outcome":
			if !listOutcomes[value] {
				bad.add(key)
				continue
			}
			f.Outcome = value
		case "request_id":
			if !requestIDRe.MatchString(value) {
				bad.add(key)
				continue
			}
			f.RequestID = value
		case "occurred_after", "occurred_before":
			t, err := time.Parse(time.RFC3339Nano, value)
			if err != nil {
				bad.add(key)
				continue
			}
			if key == "occurred_after" {
				f.OccurredAfter = &t
			} else {
				f.OccurredBefore = &t
			}
		default:
			bad.add(key)
		}
	}
	if err := bad.err(); err != nil {
		return Filter{}, err
	}
	return f, nil
}

// paramFilterName is name for a "params[name]" key.
func paramFilterName(key string) (string, bool) {
	if !strings.HasPrefix(key, "params[") || !strings.HasSuffix(key, "]") {
		return "", false
	}
	return key[len("params[") : len(key)-1], true
}

func parseTime(s *string) (time.Time, bool) {
	if s == nil {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339Nano, *s)
	return t, err == nil
}

// text is a required string of 1..max bytes that Postgres can store.
func text(bad *Invalid, path string, v *string, max int) string {
	if v == nil || *v == "" || len(*v) > max || !storable(*v) {
		bad.add(path)
		return ""
	}
	return *v
}

// optionalText is text or null.
func optionalText(bad *Invalid, path string, v *string, max int) *string {
	if v == nil {
		return nil
	}
	s := text(bad, path, v, max)
	if s == "" {
		return nil
	}
	return &s
}

// storable is valid UTF-8 with no NUL, which Postgres text refuses.
func storable(s string) bool {
	return utf8.ValidString(s) && !strings.ContainsRune(s, 0)
}

func isNull(raw json.RawMessage) bool {
	t := bytes.TrimSpace(raw)
	return len(t) == 0 || bytes.Equal(t, []byte("null"))
}
