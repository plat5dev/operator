// Package events is operator-audit: it stores the staff audit log the gateway writes and
// serves it back through the gateway. Contract: docs/audit.md.
package events

import (
	"crypto/rand"
	"encoding/json"
	"time"

	"github.com/oklog/ulid/v2"
)

// Outcomes (docs/audit.md#event). Pending is the only one an intent writes.
const (
	OutcomePending    = "pending"
	OutcomeRejected   = "rejected"
	OutcomeResponded  = "responded"
	OutcomeNoResponse = "no_response"
)

type Actor struct {
	Issuer     string  `json:"issuer"`
	OperatorID string  `json:"operator_id"`
	Email      *string `json:"email"`
	ClientID   *string `json:"client_id"`
}

// Intent is what the gateway knows before it answers or forwards.
type Intent struct {
	RequestID  string
	OccurredAt time.Time
	Actor      Actor
	// Upstream and Route are nil when no route matched. Path is set only then.
	Upstream  *string
	Method    string
	Route     *string
	Params    map[string]string
	Path      *string
	IP        string
	UserAgent *string
}

// Outcome is how the request ended. Status is nil when the client was gone before an
// answer, never on responded. Details is nil for null, and only set on responded.
type Outcome struct {
	Outcome       string
	Status        *int
	ResponseBytes int64
	Details       json.RawMessage
}

// Event is one stored request. The outcome fields are empty while pending.
type Event struct {
	ID string
	Intent
	Outcome       string
	Status        *int
	ResponseBytes *int64
	Details       json.RawMessage
}

// Filter is one list query. Zero values do not filter.
type Filter struct {
	Limit          int
	StartingAfter  string
	OperatorID     string
	Upstream       string
	Method         string
	Route          string
	Outcome        string
	RequestID      string
	Params         map[string]string
	OccurredAfter  *time.Time
	OccurredBefore *time.Time
}

// newID is a ULID on t: events sort by id in time order.
func newID(t time.Time) string {
	return ulid.MustNew(ulid.Timestamp(t), rand.Reader).String()
}

func validID(s string) bool {
	_, err := ulid.ParseStrict(s)
	return err == nil
}

// idTime is the millisecond an id was made for.
func idTime(s string) time.Time {
	return ulid.Time(ulid.MustParseStrict(s).Time())
}
