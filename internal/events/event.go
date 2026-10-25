// Package events is operator-audit: it stores the operator audit log the gateway writes and
// serves it back through the gateway. Contract: docs/reference/api/operator-audit.md.
package events

import (
	"crypto/rand"
	"encoding/json"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
)

// Outcomes (docs/reference/api/operator-audit.md#event). Pending is the only one an intent writes.
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

// ActingAs is the member an /as/{member} request acted as.
type ActingAs struct {
	MemberID  string `json:"member_id"`
	Principal string `json:"principal"`
}

// Node is one node of the route node's chain.
type Node struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

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
// responded.
type Outcome struct {
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
