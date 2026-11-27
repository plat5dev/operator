# Audit

The operator audit log: which operator called which service and path, on which nodes, as which member, with what reason, from where, and how the request ended. This gateway records every authenticated request (except `POST /access/routes`). The **operator-audit** service, in this repo, stores the events and serves them back through this gateway.

Plat5 keeps an audit log per tree, a customer's record of its own members, and one per person for their own routes. This log is separate and in a different store. An operator request is never written into a tree's log, even when acting as a member, and customers don't read this one.

## The model

An event is one request, not one change. A service may add what changed ([details](#what-a-service-adds)). Nothing else.

| Piece | Says | Owner |
|-------|------|-------|
| Actor | The operator, from the token | This gateway (authentication) |
| Acting as | The member the operator acted as, and its principal | This gateway (`X-Plat5-As-Member`, checked against identity) |
| Reason | Ticket or incident reference | This gateway (`X-Plat5-Operator-Reason`) |
| Nodes | The route node's chain, root first | This gateway (identity) |
| Action | Service, method, matched route template + params (or raw upstream path) | This gateway (routing) |
| Origin | Client IP (honoring `TRUSTED_PROXIES`), user agent | This gateway |
| Outcome | How the request ended | This gateway |
| Details | What changed (on `responded`) or `policy_reason` (on AuthZEN `rejected`) | Service `X-Plat5-Audit-Details` or AuthZEN PDP `context.reason` |
| Created | The id a create produced | The service, on `X-Plat5-Audit-Created` |
| Store and read API | | `operator-audit` |

This gateway is the only writer. Services are told the operator id and, when acting as one, the member ([requests](requests.md#acting-as-a-member)); the record of who did what is still this log. Details flow from the service to this gateway, never the other way.

Audit is on or off for the whole deployment. There is no mix.

| | Audit on (default) | Audit off (`AUDIT_ENABLED=false`) |
|---|---|---|
| `AUDIT_URL` | Required to boot | Ignored |
| `operator-audit` | Runs | Not run |
| Authenticated request | Intent written before it's answered or forwarded. **503** if it can't be | Answered or forwarded with no event |
| `X-Plat5-Audit-*` | Read, stored, stripped | Stripped |

This gateway logs `audit: off` at warn level at boot. Turning audit on doesn't backfill.

## Which requests

With audit on, every request that authenticates is recorded (other than `POST /access/routes`), whatever its method. No service route opts out. Reads are recorded, because operators reading customer data is the access this log exists to show.

| Gateway result | Event |
|----------------|-------|
| `OPTIONS` preflight | None. Answered before authentication |
| **401** | None. No trusted actor. Logged and counted by `operator_unauthenticated_total` |
| **503**, operator IdP keys never fetched | None. Token can't be judged |
| `POST /access/routes` | None. Advisory UI check; never forwards |
| **503**, intent not written | None, or `rejected` if the intent landed unseen ([delivery](#delivery)) |
| **400** unacceptable path, **404** unknown service or non-`/services/` path | `rejected`. `route` is `null`, `path` is set |
| **404** route node not found | `rejected`. `service` and `route` are set, `nodes` is `[]` |
| **400** invalid or unscoped `X-Plat5-As-Member`, member not active, or invalid `X-Plat5-Operator-Reason` | `rejected` |
| **403** AuthZEN denied | `rejected`. `status: 403`; `details: { "policy_reason": "…" }` when PDP returned `context.reason` |
| **503** identity or authorizer unreachable | `rejected` |
| Forwarded | `responded` or `no_response` |

The intent is written after routing/node resolution and before anything past authentication answers or calls the authorizer ([request order](requests.md#how-a-request-is-decided)).

## Delivery

Two writes per event, both keyed by `request_id`.

**Intent.** Written before this gateway answers, calls AuthZEN, or forwards, and the client waits on it. It carries every event field except the outcome fields. Each attempt times out at 500ms, with up to 3 attempts within 1s. If none succeeds, this gateway answers **503** and doesn't call the service. It then sends a `rejected` / `503` outcome in the background, because the intent may have landed without this gateway seeing it.

**Outcome.** Written after the response, rejection, or failure. The client doesn't wait on it. This gateway queues it in process and retries with backoff for about five minutes. The queue is bounded. An outcome that overflows the queue or runs out of retries is dropped, logged and counted. On shutdown, this gateway stops taking requests, then sends what is queued for up to 10 seconds.

Both writes are idempotent. A retried intent doesn't create a second event. An outcome is applied once, `pending` → final; a later outcome for the same request changes nothing.

**The guarantee:** a request doesn't reach its service unless its intent is recorded. If this gateway dies or drops the outcome, the event stays `pending`. Pending is not success, and nothing turns it into one. Stale pending events are counted ([metrics](../reference/gateway.md#metrics)).

**The cost:** one internal round trip on every authenticated request, before this gateway answers or forwards.

Audit isn't part of this gateway's `/health/ready`. With audit on, every authenticated request is **503** while operator-audit is down.

## What a service adds

Headers on the upstream response, as Plat5 defines them (Plat5 `docs/concepts/audit.md`). This gateway reads them as Plat5's gateway does and doesn't import Plat5 code.

| Header | Becomes | Rule |
|--------|---------|------|
| `X-Plat5-Audit-Details` | `details` | One JSON object, visible ASCII only, at most 4096 bytes. Only on `responded`. Stored as sent; this gateway never reads inside it |
| `X-Plat5-Audit-Created` | `created_id` | One id: visible ASCII, one path segment, at most 128 bytes. Only on `responded` |

Either one invalid is recorded as `null`, with a warning. Every `X-Plat5-Audit-*` header is stripped from the response on every route, audit on or off, and from the request before upstream. `X-Plat5-Audit-Nodes` and `X-Plat5-Audit-Member` are dropped.

None of them can create an event, or change its outcome, status, actor, nodes, reason, or member.

## Reading

`GET /services/operator-audit/operator-audit-events`, on operator-audit's public port, published through this gateway like any service. So each read is itself an event. Events come back newest first, filtered by actor, member acted as, reason, node (an event on the node or anything below it), service, route, params, outcome, request id, created id, and time. Shapes and filters: [`reference/api/operator-audit.md`](../reference/api/operator-audit.md).

Access to read the operator audit log is controlled by the AuthZEN authorizer (or open to any authenticated operator when `authorizer.mode: disabled`).

## Export and retention

- **Export.** `operator-audit` exposes a pull cursor feed (`GET /internal/events`) on its `INTERNAL_PORT` returning finalized events in strictly monotonic commit order (`seq`) for external WORM archival and SIEM shipping ([`reference/api/operator-audit.md`](../reference/api/operator-audit.md#export)).
- **Retention.** `operator-audit serve` runs as the non-deleting `Writer` role and cannot delete or truncate events. After archival, run `operator-audit prune --before YYYY-MM` (or `--older-than <N>m`) from a scheduled job under the schema `Owner` role to drop expired whole monthly partitions ([`internals/audit-storage.md`](../internals/audit-storage.md#pruning)).

## Not here

- Request or response bodies, query strings, the token, or claims other than the actor fields, in an event
- 401s or `POST /access/routes` checks in the log
- Audit on for some routes or operators and off for others
- A client-chosen request id as an event's key
- Operator events in a Plat5 tree's log, or Plat5's audit service or database as this log's store
- This log shown to customers
- This gateway reading inside details, or a service header creating, moving or re-outcoming an event
- Changing an event after its outcome, or deleting individual events through an API
- Turning `pending` into anything else after the fact
- Built-in SIEM, S3, or webhook push connectors
