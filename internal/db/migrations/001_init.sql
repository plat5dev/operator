-- One row per authenticated staff request (docs/audit.md). Monthly partitions on
-- occurred_at are created by ensure_partition, which the writer may call.
CREATE TABLE audit_events (
    id                TEXT        NOT NULL,  -- ULID on occurred_at
    occurred_at       TIMESTAMPTZ NOT NULL,
    request_id        TEXT        NOT NULL,
    actor_issuer      TEXT        NOT NULL,
    actor_operator_id TEXT        NOT NULL,
    actor_email       TEXT,
    actor_client_id   TEXT,
    upstream          TEXT,
    method            TEXT        NOT NULL,
    route             TEXT,
    params            JSONB       NOT NULL DEFAULT '{}'::jsonb,
    path              TEXT,
    ip                TEXT        NOT NULL,
    user_agent        TEXT,
    outcome           TEXT        NOT NULL DEFAULT 'pending'
        CHECK (outcome IN ('pending', 'rejected', 'responded', 'no_response')),
    status            INTEGER,
    response_bytes    BIGINT,
    decision          JSONB,
    details           JSONB,
    PRIMARY KEY (id, occurred_at),
    -- The partition key must be in a unique constraint. A retried intent sends
    -- the same body, so this is the request_id dedupe; the insert also checks
    -- request_id alone.
    UNIQUE (request_id, occurred_at),
    -- A matched route, or the raw path of an unmatched request. Never both.
    CHECK ((route IS NULL) <> (path IS NULL)),
    CHECK ((route IS NULL) = (upstream IS NULL))
) PARTITION BY RANGE (occurred_at);

CREATE INDEX audit_events_operator_id ON audit_events (actor_operator_id, id DESC);
CREATE INDEX audit_events_params ON audit_events USING GIN (params jsonb_path_ops);
-- Stale pending events are counted every minute.
CREATE INDEX audit_events_pending ON audit_events (occurred_at) WHERE outcome = 'pending';

-- Append-only, except one pending -> final transition that sets outcome,
-- status, response_bytes, decision, and details. Dropping a partition
-- (retention, later) is not a row delete and does not fire this.
CREATE FUNCTION audit_events_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'audit events are not deleted';
    END IF;
    IF OLD.outcome <> 'pending' THEN
        RAISE EXCEPTION 'audit event % already has an outcome', OLD.request_id;
    END IF;
    IF NEW.outcome = 'pending'
        OR (NEW.id, NEW.occurred_at, NEW.request_id, NEW.actor_issuer, NEW.actor_operator_id,
            NEW.actor_email, NEW.actor_client_id, NEW.upstream, NEW.method, NEW.route,
            NEW.params, NEW.path, NEW.ip, NEW.user_agent)
           IS DISTINCT FROM
           (OLD.id, OLD.occurred_at, OLD.request_id, OLD.actor_issuer, OLD.actor_operator_id,
            OLD.actor_email, OLD.actor_client_id, OLD.upstream, OLD.method, OLD.route,
            OLD.params, OLD.path, OLD.ip, OLD.user_agent)
    THEN
        RAISE EXCEPTION 'audit event % may only gain an outcome', OLD.request_id;
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER audit_events_guard
    BEFORE UPDATE OR DELETE ON audit_events
    FOR EACH ROW EXECUTE FUNCTION audit_events_guard();

-- Creates the month's partition that holds p_at. It runs as the owner, so the
-- writer can add a month without DDL rights (docs/audit.md#roles).
CREATE FUNCTION ensure_partition(p_at timestamptz) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = audit, pg_temp
SET timezone = 'UTC'
AS $$
DECLARE
    month_start timestamptz := date_trunc('month', p_at);
    partition_name text := 'audit_events_y' || to_char(month_start, 'YYYY') || 'm' || to_char(month_start, 'MM');
BEGIN
    PERFORM pg_advisory_xact_lock(hashtext('audit.audit_events.partitions'));
    EXECUTE format(
        'CREATE TABLE IF NOT EXISTS audit.%I PARTITION OF audit.audit_events FOR VALUES FROM (%L) TO (%L)',
        partition_name, month_start, month_start + interval '1 month');
END;
$$;

REVOKE ALL ON FUNCTION ensure_partition(timestamptz) FROM PUBLIC;
