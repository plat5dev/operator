# Audit storage

For implementers. The contract is [audit](../concepts/audit.md) and the [operator-audit API](../reference/api/operator-audit.md).

## Storage

The log lives in the operator plane's own Postgres, never Plat5's. Other operator-plane concerns may share the instance later, each in its own schema with its own roles.

`audit_events` in schema `audit`, partitioned by month on `occurred_at`, unique on `(request_id, occurred_at)`. The chain is stored twice, as Plat5's audit does: `nodes` as sent, and `node_ids` (`text[]`) with a GIN index for reads by node. Indexes back the operator, member-acted-as, reason, and params filters. Check constraints keep `root_id` the chain's head, `route` or raw `path` (never both), `service` required whenever `route` is set (and optional when `path` is set, for unlisted paths on a known service). Partitions are kept from last month to two months ahead, checked at boot and hourly; an intent outside them creates its month.

`finalized_seq` is assigned from sequence `export_seq` under a transaction-scoped advisory lock (`audit.export_seq`) when an event transitions from `pending` to its final outcome, guaranteeing strict commit-order visibility for `GET /internal/events` export readers.

Append-only, except the one `pending` → final transition (which also sets `finalized_seq`). A trigger enforces it: an update may only move a pending event to its outcome, and a delete is refused. No API updates or deletes an event. Dropping expired whole monthly partitions via `operator-audit prune` runs under the schema `Owner` role and does not fire row delete triggers.

## Roles

| Role | Can | Used by |
|------|-----|---------|
| Owner | Owns schema `audit`. Runs migrations, owns `ensure_partition`, and drops expired monthly partitions | `operator-audit migrate`, `operator-audit prune` |
| Writer | `SELECT`, `INSERT`, column-limited `UPDATE` on `audit_events`, `USAGE`/`SELECT` on `export_seq`, and `EXECUTE` on `ensure_partition`. Nothing else: no `DELETE`, `TRUNCATE`, DDL, or ownership | `operator-audit serve` |

The deployment creates both login roles. `migrate` grants the role named by `WRITER_ROLE` exactly the writer's rights. `serve` refuses to boot if its role owns schema `audit`, or can delete from or truncate `audit_events`.

The partition function is `SECURITY DEFINER`, owned by the owner role, so the writer can add a month without DDL rights.

The trigger stops the writer. It doesn't stop the owner or a superuser. Neither credential is used at runtime.

## Pruning

`operator-audit prune` runs as the schema owner and drops whole monthly partitions of `audit.audit_events` strictly older than the cutoff:

```bash
operator-audit prune --before 2026-01 --dry-run
operator-audit prune --older-than 12m
```

Specify exactly one of `--before YYYY-MM` or `--older-than <N>m`. Cutoffs past the current UTC month are refused so live or future partitions cannot be dropped.

## Runtime

| | |
|--|--|
| Binary | `operator-audit`, from `cmd/operator-audit/` |
| Image | `ghcr.io/plat5dev/operator-audit` on `v*` tags |
| Commands | `operator-audit migrate`, `operator-audit serve`, `operator-audit prune` |
| Public port | `5005` (`GET /operator-audit-events`) |
| Internal port | `8005` (`/health/live`, `/health/ready`, `/metrics`, `/internal/events`) |
| Database | `DATABASE_URL`, schema `audit` |

Environment: [`reference/env.md`](../reference/env.md#operator-audit). `/health/ready` fails closed with **503** `unhealthy` when Postgres is unreachable.
