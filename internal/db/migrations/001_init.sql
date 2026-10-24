-- One row per authenticated staff request (docs/internals/audit-storage.md). Monthly
-- partitions on occurred_at are created by ensure_partition, which the writer may call.
CREATE TABLE audit_events (
    id                  TEXT        NOT NULL,  -- ULID on occurred_at
    occurred_at         TIMESTAMPTZ NOT NULL,
    request_id          TEXT        NOT NULL,
    actor_issuer        TEXT        NOT NULL,
    actor_operator_id   TEXT        NOT NULL,
    actor_email         TEXT,
    actor_client_id     TEXT,
    acting_as_member_id TEXT,
    acting_as_principal TEXT
        CHECK (acting_as_principal IN ('person', 'service_account')),
    -- The route node's chain, root first: as sent, and its ids for the GIN index.
    root_id             TEXT,
    nodes               JSONB       NOT NULL DEFAULT '[]'::jsonb,
    node_ids            TEXT[]      NOT NULL DEFAULT '{}',
    service             TEXT,
    method              TEXT        NOT NULL,
    route               TEXT,
    params              JSONB       NOT NULL DEFAULT '{}'::jsonb,
    path                TEXT,
    ip                  TEXT        NOT NULL,
    user_agent          TEXT,
    outcome             TEXT        NOT NULL DEFAULT 'pending'
        CHECK (outcome IN ('pending', 'rejected', 'responded', 'no_response')),
    status              INTEGER,
    response_bytes      BIGINT,
    details             JSONB,
    created_id          TEXT,
    PRIMARY KEY (id, occurred_at),
    -- The partition key must be in a unique constraint. A retried intent sends
    -- the same body, so this is the request_id dedupe; the insert also checks
    -- request_id alone.
    UNIQUE (request_id, occurred_at),
    -- A matched route, or the raw path of an unmatched request. Never both.
    CHECK ((route IS NULL) <> (path IS NULL)),
    CHECK ((route IS NULL) = (service IS NULL)),
    -- The root is the chain's head, and there is a chain only on a matched route.
    CHECK (root_id IS NOT DISTINCT FROM node_ids[1]),
    CHECK (jsonb_array_length(nodes) = cardinality(node_ids)),
    CHECK (route IS NOT NULL OR cardinality(node_ids) = 0),
    -- A member acted as is checked in the chain's root.
    CHECK ((acting_as_member_id IS NULL) = (acting_as_principal IS NULL)),
    CHECK (acting_as_member_id IS NULL OR root_id IS NOT NULL)
) PARTITION BY RANGE (occurred_at);

CREATE INDEX audit_events_operator_id ON audit_events (actor_operator_id, id DESC);
CREATE INDEX audit_events_acting_as ON audit_events (acting_as_member_id, id DESC)
    WHERE acting_as_member_id IS NOT NULL;
CREATE INDEX audit_events_node_ids ON audit_events USING GIN (node_ids);
CREATE INDEX audit_events_params ON audit_events USING GIN (params jsonb_path_ops);
-- Stale pending events are counted every minute.
CREATE INDEX audit_events_pending ON audit_events (occurred_at) WHERE outcome = 'pending';

-- Append-only, except one pending -> final transition that sets outcome,
-- status, response_bytes, details, and created_id. Dropping a partition
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
            NEW.actor_email, NEW.actor_client_id, NEW.acting_as_member_id, NEW.acting_as_principal,
            NEW.root_id, NEW.nodes, NEW.node_ids, NEW.service, NEW.method, NEW.route,
            NEW.params, NEW.path, NEW.ip, NEW.user_agent)
           IS DISTINCT FROM
           (OLD.id, OLD.occurred_at, OLD.request_id, OLD.actor_issuer, OLD.actor_operator_id,
            OLD.actor_email, OLD.actor_client_id, OLD.acting_as_member_id, OLD.acting_as_principal,
            OLD.root_id, OLD.nodes, OLD.node_ids, OLD.service, OLD.method, OLD.route,
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
-- writer can add a month without DDL rights (docs/internals/audit-storage.md#roles).
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
