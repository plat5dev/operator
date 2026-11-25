// Package events is operator-audit: it stores the operator audit log the gateway writes and
// serves it back through the gateway. Contract: docs/reference/api/operator-audit.md.
package events

import (
	"crypto/rand"
	"encoding/json"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/plat5dev/operator/internal/audit"
)

// Outcomes (docs/reference/api/operator-audit.md#event). Pending is the only one an intent writes.
const (
	OutcomePending    = "pending"
	OutcomeRejected   = string(audit.Rejected)
	OutcomeResponded  = string(audit.Responded)
	OutcomeNoResponse = string(audit.NoResponse)
)

type (
	Actor    = audit.Actor
	ActingAs = audit.ActingAs
	Node     = audit.Node
)

// Intent is what the gateway knows before it answers or forwards.
type Intent struct {
	RequestID  string
	OccurredAt time.Time
	Actor      Actor
	ActingAs   *ActingAs
	// RootID is Nodes[0].ID, nil when Nodes is empty.
	RootID *string
	Nodes  []Node
	// Service and Route are nil when no route matched. Path is set only then.
	Service   *string
	Method    string
	Route     *string
	Params    map[string]string
	Path      *string
	IP        string
	UserAgent *string
}

// Outcome is how the request ended. Status is nil when the client was gone before an
// answer, never on responded. Details and CreatedID are nil for null, and only set on
// responded. OccurredAt is optional; when set, ApplyOutcome prunes to that partition.
type Outcome struct {
	OccurredAt    *time.Time
	Outcome       string
	Status        *int
	ResponseBytes int64
	Details       json.RawMessage
	CreatedID     *string
}

// Event is one stored request. The outcome fields are empty while pending.
type Event struct {
	ID string
	Intent
	Outcome       string
	Status        *int
	ResponseBytes *int64
	Details       json.RawMessage
	CreatedID     *string
}

// Filter is one list query. Zero values do not filter.
type Filter struct {
	Limit          int
	StartingAfter  string
	OperatorID     string
	ActingAsMember string
	// NodeID matches events on the node or below it: whose chain contains it.
	NodeID         string
	Service        string
	CreatedID      string
	Method         string
	Route          string
	Outcome        string
	RequestID      string
	Params         map[string]string
	OccurredAfter  *time.Time
	OccurredBefore *time.Time
}

// newID is a lowercase ULID on t, as Plat5's ids are: events sort by id in time order.
func newID(t time.Time) string {
	return strings.ToLower(ulid.MustNew(ulid.Timestamp(t), rand.Reader).String())
}

// validID is a lowercase ULID. An uppercase one isn't canonical and matches nothing.
func validID(s string) bool {
	_, err := ulid.ParseStrict(s)
	return err == nil && s == strings.ToLower(s)
}

// idTime is the millisecond an id was made for.
func idTime(s string) time.Time {
	return ulid.Time(ulid.MustParseStrict(s).Time())
}
