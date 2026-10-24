# Audit storage

For implementers. The contract is [audit](../concepts/audit.md) and the [operator-audit API](../reference/api/operator-audit.md).

## Storage

The log lives in the operator plane's own Postgres, never Plat5's. Other operator-plane concerns may share the instance later, each in its own schema with its own roles.

`audit_events` in schema `audit`, partitioned by month on `occurred_at`, unique on `request_id`. The chain is stored twice, as Plat5's audit does: `nodes` as sent, and `node_ids` (`text[]`) with a GIN index for reads by node. Indexes back the operator, member-acted-as and params filters. Check constraints keep `root_id` the chain's head and a matched route or a raw path, never both. Partitions are kept from last month to two months ahead, checked at boot and hourly; an intent outside them creates its month.

One migration, `001_init.sql`. A database migrated by an earlier build with a different `001` has to be recreated.

Append-only, except the one `pending` → final transition. A trigger enforces it: an update may only move a pending event to its outcome, and a delete is refused. No API updates or deletes an event. Events are kept, with no purge yet.

## Roles

| Role | Can | Used by |
|------|-----|---------|
| Owner | Owns schema `audit`. Runs migrations. Owns the partition function | `operator-audit migrate` |
| Writer | `SELECT`, `INSERT`, `UPDATE` on `audit_events`, and `EXECUTE` on the partition function. Nothing else: no `DELETE`, `TRUNCATE`, DDL, or ownership | `operator-audit serve` |

The deployment creates both login roles. `migrate` grants the role named by `WRITER_ROLE` exactly the writer's rights. `serve` refuses to boot if its role owns schema `audit`, or can delete from or truncate `audit_events`.

The partition function is `SECURITY DEFINER`, owned by the owner role, so the writer can add a month without DDL rights.

The trigger stops the writer. It doesn't stop the owner or a superuser. Neither credential is used at runtime.

## Runtime

| | |
|--|--|
| Binary | `operator-audit`, from `cmd/operator-audit/` |
| Image | `ghcr.io/plat5dev/operator-audit` on `v*` tags |
| Commands | `operator-audit migrate`, `operator-audit serve` |
| Public port | `5005` (`GET /operator-audit-events`) |
| Internal port | `8005` (`/health/live`, `/health/ready`, `/metrics`, `/internal/events`) |
| Database | `DATABASE_URL`, schema `audit` |

Environment: [`reference/env.md`](../reference/env.md#operator-audit). `/health/ready` fails closed with **503** `unhealthy` when Postgres is unreachable.
