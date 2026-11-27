-- Adds export_seq and finalized_seq on audit_events for deployment-wide pull
-- export (GET /internal/events) in strict commit order (docs/reference/api/operator-audit.md).

CREATE SEQUENCE export_seq;

ALTER TABLE audit_events ADD COLUMN finalized_seq BIGINT;

ALTER TABLE audit_events DISABLE TRIGGER audit_events_guard;
UPDATE audit_events
SET finalized_seq = nextval('export_seq')
WHERE outcome <> 'pending' AND finalized_seq IS NULL;
ALTER TABLE audit_events ENABLE TRIGGER audit_events_guard;

CREATE INDEX audit_events_finalized_seq
    ON audit_events (finalized_seq)
    WHERE finalized_seq IS NOT NULL;

CREATE OR REPLACE FUNCTION audit_events_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'audit events are not deleted';
    END IF;
    IF OLD.outcome <> 'pending' THEN
        RAISE EXCEPTION 'audit event % already has an outcome', OLD.request_id;
    END IF;
    IF NEW.outcome = 'pending'
        OR NEW.finalized_seq IS NULL
        OR (NEW.id, NEW.occurred_at, NEW.request_id, NEW.actor_issuer, NEW.actor_operator_id,
            NEW.actor_email, NEW.actor_client_id, NEW.acting_as_member_id, NEW.acting_as_principal,
            NEW.reason, NEW.root_id, NEW.nodes, NEW.node_ids, NEW.service, NEW.method, NEW.route,
            NEW.params, NEW.path, NEW.ip, NEW.user_agent)
           IS DISTINCT FROM
           (OLD.id, OLD.occurred_at, OLD.request_id, OLD.actor_issuer, OLD.actor_operator_id,
            OLD.actor_email, OLD.actor_client_id, OLD.acting_as_member_id, OLD.acting_as_principal,
            OLD.reason, OLD.root_id, OLD.nodes, OLD.node_ids, OLD.service, OLD.method, OLD.route,
            OLD.params, OLD.path, OLD.ip, OLD.user_agent)
    THEN
        RAISE EXCEPTION 'audit event % may only gain an outcome', OLD.request_id;
    END IF;
    RETURN NEW;
END;
$$;
