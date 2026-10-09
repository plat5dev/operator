package events

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("not found")

const queryTimeout = 5 * time.Second

// Store is the audit_events table, used as the writer role (docs/audit.md#roles).
type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

const eventColumns = `id, occurred_at, request_id, actor_issuer, actor_operator_id, actor_email,
	actor_client_id, upstream, method, route, params, path, ip, user_agent, outcome, status,
	response_bytes, details`

// CreateIntent writes a pending event unless one exists for the request id. created is
// false for a retry, which leaves the existing event as it is.
func (s *Store) CreateIntent(ctx context.Context, in *Intent) (created bool, err error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	params, err := json.Marshal(in.Params)
	if err != nil {
		return false, err
	}
	insert := func() (pgconn.CommandTag, error) {
		// request_id alone is the dedupe. The unique index has occurred_at too (the
		// partition key), so check request_id here and let the index catch two
		// identical intents racing.
		return s.pool.Exec(ctx, `
			INSERT INTO audit_events (id, occurred_at, request_id, actor_issuer, actor_operator_id,
				actor_email, actor_client_id, upstream, method, route, params, path, ip, user_agent)
			SELECT $1::text, $2::timestamptz, $3::text, $4::text, $5::text, $6::text, $7::text,
				$8::text, $9::text, $10::text, $11::jsonb, $12::text, $13::text, $14::text
			WHERE NOT EXISTS (SELECT 1 FROM audit_events WHERE request_id = $3::text)
			ON CONFLICT DO NOTHING
		`, newID(in.OccurredAt), in.OccurredAt, in.RequestID, in.Actor.Issuer, in.Actor.OperatorID,
			in.Actor.Email, in.Actor.ClientID, in.Upstream, in.Method, in.Route, string(params), in.Path,
			in.IP, in.UserAgent)
	}
	tag, err := insert()
	if isNoPartition(err) {
		// Maintenance runs ahead of the clock; this is a boot or skew race.
		if err := s.ensurePartition(ctx, in.OccurredAt); err != nil {
			return false, err
		}
		tag, err = insert()
	}
	if err != nil {
		return false, fmt.Errorf("create intent: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// ApplyOutcome moves a pending event to its outcome. applied is false when the event
// was already final; it is left unchanged. ErrNotFound means there is no event.
func (s *Store) ApplyOutcome(ctx context.Context, requestID string, out *Outcome) (applied bool, err error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	var details any
	if out.Details != nil {
		details = string(out.Details)
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE audit_events
		SET outcome = $2, status = $3, response_bytes = $4, details = $5::jsonb
		WHERE request_id = $1 AND outcome = 'pending'
	`, requestID, out.Outcome, out.Status, out.ResponseBytes, details)
	if err != nil {
		return false, fmt.Errorf("apply outcome: %w", err)
	}
	if tag.RowsAffected() > 0 {
		return true, nil
	}
	var exists bool
	if err := s.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM audit_events WHERE request_id = $1)`, requestID,
	).Scan(&exists); err != nil {
		return false, fmt.Errorf("apply outcome: %w", err)
	}
	if !exists {
		return false, ErrNotFound
	}
	return false, nil
}

// List returns one page of events, newest first, and whether more follow.
func (s *Store) List(ctx context.Context, f Filter) ([]*Event, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	var where []string
	var args []any
	add := func(cond string, v any) {
		args = append(args, v)
		where = append(where, strings.ReplaceAll(cond, "?", "$"+strconv.Itoa(len(args))))
	}
	if f.StartingAfter != "" {
		add("id < ?", f.StartingAfter)
		// The id's time is occurred_at to the millisecond. Saying so lets Postgres
		// skip newer partitions.
		add("occurred_at < ?", idTime(f.StartingAfter).Add(time.Millisecond))
	}
	if f.OperatorID != "" {
		add("actor_operator_id = ?", f.OperatorID)
	}
	if f.Upstream != "" {
		add("upstream = ?", f.Upstream)
	}
	if f.Method != "" {
		add("method = ?", f.Method)
	}
	if f.Route != "" {
		add("route = ?", f.Route)
	}
	if f.Outcome != "" {
		add("outcome = ?", f.Outcome)
	}
	if f.RequestID != "" {
		add("request_id = ?", f.RequestID)
	}
	if len(f.Params) > 0 {
		p, err := json.Marshal(f.Params)
		if err != nil {
			return nil, false, err
		}
		add("params @> ?::jsonb", string(p))
	}
	if f.OccurredAfter != nil {
		add("occurred_at >= ?", *f.OccurredAfter)
	}
	if f.OccurredBefore != nil {
		add("occurred_at < ?", *f.OccurredBefore)
	}
	cond := "TRUE"
	if len(where) > 0 {
		cond = strings.Join(where, " AND ")
	}
	args = append(args, f.Limit+1)

	rows, err := s.pool.Query(ctx, `SELECT `+eventColumns+` FROM audit_events
		WHERE `+cond+`
		ORDER BY id DESC
		LIMIT $`+strconv.Itoa(len(args)), args...)
	if err != nil {
		return nil, false, fmt.Errorf("list events: %w", err)
	}
	defer rows.Close()

	var out []*Event
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, false, fmt.Errorf("list events: %w", err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("list events: %w", err)
	}
	hasMore := len(out) > f.Limit
	if hasMore {
		out = out[:f.Limit]
	}
	return out, hasMore, nil
}

func scanEvent(row pgx.Row) (*Event, error) {
	var e Event
	var params, details []byte
	if err := row.Scan(&e.ID, &e.OccurredAt, &e.RequestID, &e.Actor.Issuer, &e.Actor.OperatorID,
		&e.Actor.Email, &e.Actor.ClientID, &e.Upstream, &e.Method, &e.Route, &params, &e.Path, &e.IP,
		&e.UserAgent, &e.Outcome, &e.Status, &e.ResponseBytes, &details); err != nil {
		return nil, err
	}
	e.OccurredAt = e.OccurredAt.UTC()
	if err := json.Unmarshal(params, &e.Params); err != nil {
		return nil, fmt.Errorf("params: %w", err)
	}
	if details != nil {
		e.Details = json.RawMessage(details)
	}
	return &e, nil
}

// StalePending counts events from the last 24 hours still pending after 10 minutes.
// Outcome retries end after about five, so these will not get one.
func (s *Store) StalePending(ctx context.Context, now time.Time) (int64, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	var n int64
	err := s.pool.QueryRow(ctx, `
		SELECT count(*) FROM audit_events
		WHERE outcome = 'pending' AND occurred_at >= $1 AND occurred_at < $2
	`, now.Add(-24*time.Hour), now.Add(-10*time.Minute)).Scan(&n)
	return n, err
}

// EnsureAround creates partitions from the month before now to two months after.
func (s *Store) EnsureAround(ctx context.Context, now time.Time) error {
	m := monthStart(now)
	for i := -1; i <= 2; i++ {
		if err := s.ensurePartition(ctx, m.AddDate(0, i, 0)); err != nil {
			return err
		}
	}
	return nil
}

// MaintainPartitions runs EnsureAround every interval until ctx ends. Intents are
// within a day of now, so this always runs ahead of them.
func (s *Store) MaintainPartitions(ctx context.Context, interval time.Duration, onErr func(error)) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := s.EnsureAround(ctx, time.Now()); err != nil {
				onErr(err)
			}
		}
	}
}

func (s *Store) Ping(ctx context.Context) error { return s.pool.Ping(ctx) }

// ensurePartition runs as the owner through a SECURITY DEFINER function, so the writer
// needs no DDL rights.
func (s *Store) ensurePartition(ctx context.Context, at time.Time) error {
	if _, err := s.pool.Exec(ctx, `SELECT ensure_partition($1)`, at); err != nil {
		return fmt.Errorf("ensure partition for %s: %w", at.UTC().Format("2006-01"), err)
	}
	return nil
}

func monthStart(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}

// isNoPartition is Postgres refusing a row no partition covers.
func isNoPartition(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23514" && strings.Contains(pgErr.Message, "no partition")
}
