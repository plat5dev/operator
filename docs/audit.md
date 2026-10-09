# Audit

**Slice 2.** The contract it builds to.

The staff audit log records which operator called which route, on which ids, from where, and how the request ended. This gateway records every authenticated request. The **operator-audit** service, in this repo, stores the events and serves them back through this gateway.

Plat5 has an org audit log, which is a customer's record of its own members. This log is separate and in a different store. A staff request is never written into an org's log, and customers do not read this one.

## The model

An audit event is one request, not one change. A service may add what changed ([details](#details)). Nothing else.

| Piece | Says | Owner |
|-------|------|-------|
| Actor | The staff principal from the token | This gateway (authentication) |
| Action | Upstream, method, route template, path params | This gateway (route match) |
| Origin | IP, user agent | This gateway |
| Outcome | How the request ended | This gateway |
| Details | What changed | The service, on `X-Plat5-Audit-Details` |
| Store and read API | | `operator-audit` |

This gateway is the only writer. Services are not told the actor, and audit does not change that ([`model.md`](model.md#actor-and-target)). Details flow from the service to this gateway, never the other way.

Audit is on or off for the whole deployment. There is no mix.

| | Audit on (default) | Audit off (`AUDIT_ENABLED=false`) |
|---|---|---|
| `AUDIT_URL`, `AUDIT_TOKEN` | Required to boot | Ignored |
| `operator-audit` | Runs | Not run |
| Authenticated request | Intent written before it is answered or forwarded. **503** if it cannot be | Answered or forwarded with no event |
| `X-Plat5-Audit-Details` | Read, stored, stripped | Stripped |
| `/operator-audit-events` route | In the route file | Left out. A route to an upstream that is not running gets **503** |
| `operator_audit_*` metrics | Reported | Not reported |

This gateway logs `audit: off` at warn level at boot, as it does `authz: none`. Turning audit on does not backfill. The log starts when audit is first on.

## Which requests

With audit on, every request that authenticates is recorded, whatever its method. No route opts out. Reads are recorded, because staff reading customer data is the access this log exists to show.

| Gateway result | Event |
|----------------|-------|
| `OPTIONS` preflight | None. It is answered before authentication |
| **401** | None. There is no trusted actor, and nothing bounds the rate. The request log line and `operator_unauthenticated_total` count it |
| **503**, staff IdP keys never fetched | None. The token cannot be judged, so there is no actor |
| **503**, intent not written | None, or `rejected` if the intent landed unseen ([delivery](#delivery)) |
| **400** unacceptable path | `rejected`. `route` is `null`, `path` is set |
| **404** no route | `rejected`. `route` is `null`, `path` is set |
| **403** / **503** from authz | `rejected`. Slice 3 |
| Forwarded | `responded` or `no_response` |

The intent is written after the route match and before anything past authentication answers ([`model.md`](model.md#request-order)). A request that will be **400** or **404** is recorded first, then answered.

## Delivery

Each event takes two writes, both keyed by `request_id`.

**Intent.** Written before this gateway answers or forwards, and the client waits on it. It carries every event field except the outcome fields. Each attempt times out at 500ms, with up to 3 attempts within 1s. If none succeeds, this gateway answers **503** `SERVICE_UNAVAILABLE` and does not call the service. It then sends a `rejected` / `503` outcome in the background, because the intent may have landed without this gateway seeing it.

**Outcome.** Written after the response, or the failure. The client does not wait on it. It carries `outcome`, `status`, `response_bytes`, `decision`, and `details`. This gateway queues it in process and retries with backoff for about five minutes. The queue is bounded. An outcome that overflows the queue or runs out of retries is dropped, logged, and counted. On shutdown, this gateway stops taking requests, then sends what is queued for up to 10 seconds.

Both writes are idempotent. A retried intent does not create a second event. An outcome is applied once, `pending` → final. A later outcome for the same request changes nothing.

**The guarantee:** a request does not reach its service unless its intent is recorded. If this gateway dies or drops the outcome, the event stays `pending`. Pending is not success, and nothing turns it into one. Stale pending events are counted ([metrics](#metrics)).

**The cost:** one internal round trip on every authenticated request, before this gateway answers or forwards.

Audit is not part of this gateway's `/health/ready`. With audit on, every authenticated request is **503** while operator-audit is down.

## Event

```json
{
  "id": "01JA2Z6Q3Y8D5V2K9N4R7T1W0X",
  "occurred_at": "2026-10-09T18:30:00.123Z",
  "request_id": "9f2c4e1a7b3d4c8e9a0b1c2d3e4f5a6b",
  "actor": {
    "issuer": "https://staff.example.okta.com/oauth2/default",
    "operator_id": "00u1abc",
    "email": "a@example.com",
    "client_id": "operator-console"
  },
  "upstream": "identity",
  "method": "PATCH",
  "route": "/organizations/{organization_id}/members/{member_id}",
  "params": {
    "organization_id": "01J9ZX4K7M2P5Q8R1S3T6V9W0Y",
    "member_id": "01J9ZX6N2P5Q8R1S4T7V0W3X6Y"
  },
  "path": null,
  "ip": "10.0.4.17",
  "user_agent": "Mozilla/5.0 …",
  "outcome": "responded",
  "status": 200,
  "response_bytes": 312,
  "decision": null,
  "details": { "role": { "from": "developer", "to": "admin" } }
}
```

| Field | |
|-------|--|
| `id` | ULID, assigned by operator-audit when the intent is written |
| `occurred_at` | When this gateway received the request. UTC, milliseconds |
| `request_id` | `X-Request-ID`. This gateway generates one for every request ([`model.md`](model.md#request-id-and-tracing)). Joins to every service log line |
| `actor.issuer` | The token's `iss` |
| `actor.operator_id` | The operator id claim ([`idp.md`](idp.md)). Unique within an issuer |
| `actor.email` | The `email` claim. `null` when absent |
| `actor.client_id` | The `azp` claim, else `client_id`: the client the token was issued to, such as a console, a CLI, or automation. `null` when neither claim is present |
| `upstream` | The upstream's name in the route file. `null` when no route matched |
| `method` | HTTP method |
| `route` | The matched path template, not the raw path. `null` when no route matched |
| `params` | Path params by name, as matched. `{}` when there are none, or no route matched. Bytes Postgres cannot store (invalid UTF-8, NUL) are recorded as U+FFFD |
| `path` | The raw path, escaped as received, cut to 2048 characters. Set only when no route matched; otherwise `null` |
| `ip` | The TCP peer address of the connection. Behind a proxy, that is the proxy's address. `X-Forwarded-For` is not read |
| `user_agent` | `User-Agent`, cut to 512 characters. `null` when absent |
| `outcome` | See below |
| `status` | The HTTP status this gateway answered with. `null` while `pending`, or when the client was gone before an answer |
| `response_bytes` | Response body bytes written to the client. `null` while `pending` |
| `decision` | Reserved for authz. Always `null` in this slice. Slice 3 defines it |
| `details` | The service's object, or `null` |

| `outcome` | Means | The change |
|-----------|-------|------------|
| `pending` | Intent recorded, no outcome | Unknown |
| `rejected` | This gateway answered without calling the service | Did not happen |
| `responded` | The service answered. `status` is its status | Per `status` |
| `no_response` | This gateway called the service and got no answer (**503**) | Unknown |

An event never contains request or response bodies, the query string, headers other than `User-Agent`, the token, claims other than the four actor fields, or any customer credential.

## Details

`X-Plat5-Audit-Details` on the upstream response. This is Plat5's service contract (Plat5 `docs/audit.md`). This gateway reads the header as Plat5 defines it and does not import Plat5 code.

| | |
|--|--|
| Set by | The service, on its response |
| Format | One JSON object. Visible ASCII only, with the rest escaped as `\u`. At most 4096 bytes |
| Read | From the upstream response, on every forwarded request |
| Invalid or over the cap | The event is recorded with `details: null`. This gateway logs a warning |
| Stripped | Always, on every route: from the request before upstream, and from the response before the client |
| Meaning | The service's. Stored as sent. This gateway never reads inside it |
| Cannot | Create an event, or change its outcome, status, actor, or target |

A service does not know which front door called it. The details on a staff request are the same object the customer plane would record for the same write. Each service documents its own shapes. Identity's are in Plat5's `docs/identity.md`.

## Reading

```
GET /operator-audit-events
```

The read API is on operator-audit's public port. It is published through this gateway like any route, so each read is itself an audit event, and in slice 3 an authz action:

```yaml
upstreams:
  operator-audit:
    url: http://operator-audit:5005
    routes:
      - path: /operator-audit-events
        methods: [GET]
```

| Query | |
|-------|--|
| `limit` | Default 50, max 100. A value over the max is clamped. Below 1, or not an integer → **422** |
| `starting_after` | The last `id` of the previous page. Must be a ULID |
| `operator_id` | The acting operator |
| `upstream`, `method`, `outcome`, `request_id` | Exact match |
| `route` | Exact template, e.g. `/organizations/{organization_id}/members/{member_id}` |
| `params[<name>]` | A path param's value, e.g. `params[organization_id]=01J…`. One per name; several names AND |
| `occurred_after`, `occurred_before` | RFC 3339. After is inclusive, before is exclusive |

Filters AND. An unknown query param or a malformed value → **422** `VALIDATION_ERROR`.

```json
{
  "audit_events": [ ... ],
  "has_more": false
}
```

Events are ordered by `id`, newest first. `starting_after` continues the walk to older events.

Until slice 3, any operator can read the whole log. That is part of [the hole](v2.md#the-hole).

## Internal API

The internal API is on operator-audit's `INTERNAL_PORT` and is not routed through this gateway. Every call carries `Authorization: Bearer <INTERNAL_TOKEN>`. A missing or wrong token gets **401**. Only this gateway writes the staff log.

### Intent

```
PUT /internal/events/{request_id}
Authorization: Bearer <INTERNAL_TOKEN>
Content-Type: application/json

{
  "occurred_at": "…",
  "actor": { "issuer": "…", "operator_id": "…", "email": "…", "client_id": "…" },
  "upstream": "…", "method": "…", "route": "…", "params": { … }, "path": null,
  "ip": "…", "user_agent": "…"
}
```

| Result | Response |
|--------|----------|
| Written | **201** |
| An event for this `request_id` exists | **200**. Left unchanged |
| Missing or wrong token | **401** |
| Malformed. Includes `occurred_at` more than 24h from operator-audit's clock, and `route` and `path` both set or both `null` | **422** |
| Postgres unavailable | **503**. This gateway retries |

The intent is created only if absent. A retry sends the same body. Unknown fields are ignored, so a newer gateway can send more.

### Outcome

```
PATCH /internal/events/{request_id}

{ "outcome": "responded", "status": 200, "response_bytes": 312, "decision": null, "details": { … } }
```

| Result | Response |
|--------|----------|
| Applied, or the event is already final | **204**. A final event is left unchanged |
| No event for this `request_id` | **404**. This gateway stops retrying |
| Missing or wrong token | **401** |
| Malformed, or `outcome: pending` | **422** |
| Postgres unavailable | **503**. This gateway retries |

`status` may be `null` only when the client was gone before an answer, so it is never `null` with `responded`. `details` may be set only with `responded`. `response_bytes` is a non-negative integer. `decision` must be `null` in this slice.

## Storage

The log lives in the operator plane's own Postgres, never Plat5's. Other operator-plane concerns may share the instance later, each in its own schema with its own roles.

Events are stored in `audit_events` in schema `audit`, partitioned by month on `occurred_at` and unique on `request_id`. The table is indexed for the read filters. Operator-audit keeps partitions from last month to two months ahead, checked at boot and hourly. An intent outside them creates its month.

The table is append-only, except for the one `pending` → final transition. A trigger enforces it: an update may only move a pending event to its outcome, and a delete is refused. No API updates or deletes an event. Events are kept, with no purge yet.

### Roles

| Role | Can | Used by |
|------|-----|---------|
| Owner | Owns schema `audit`. Runs migrations. Owns the partition function | `operator-audit migrate` |
| Writer | `SELECT`, `INSERT`, `UPDATE` on `audit_events`, and `EXECUTE` on the partition function. Nothing else: no `DELETE`, `TRUNCATE`, DDL, or ownership | `operator-audit serve` |

The deployment creates both login roles. `migrate` grants the role named by `WRITER_ROLE` exactly the writer's rights. `serve` refuses to boot if its role owns schema `audit`, or can delete from or truncate `audit_events`.

The partition function is `SECURITY DEFINER`, owned by the owner role, so the writer can add a month without DDL rights.

The trigger stops the writer. It does not stop the owner or a superuser. Neither credential is used at runtime.

## Metrics

Each service serves `/metrics` on its internal port, in Prometheus text format.

| Metric | Labels | |
|--------|--------|--|
| `operator_audit_writes_total` | `write` (`intent`, `outcome`), `result` (`ok`, `failed`, `dropped`, `not_found`) | This gateway |
| `operator_audit_intent_duration_seconds` | `result` | This gateway. Histogram |
| `operator_audit_outcome_queue` | | This gateway. Outcomes waiting to be sent |
| `operator_unauthenticated_total` | `reason` (`missing`, `expired`, `invalid`) | This gateway. 401s, which are not events |
| `operator_audit_stale_pending` | | operator-audit. Events from the last 24 hours still `pending` after 10 minutes. Checked every minute |

Alert on `failed`, `dropped`, and stale pending events. A stale pending event is a request that may have reached its service and has no recorded outcome. `GET /operator-audit-events?outcome=pending` finds them all.

## Runtime

| | |
|--|--|
| Binary | `operator-audit`, from `cmd/operator-audit/` |
| Image | `ghcr.io/plat5dev/operator-audit` on `v*` tags |
| Commands | `operator-audit migrate`, `operator-audit serve` |
| Public port | `5005` (`GET /operator-audit-events`) |
| Internal port | `8005` (`/health/live`, `/health/ready`, `/metrics`, `/internal/events`) |
| Database | `DATABASE_URL`, schema `audit` |

| Variable | Default | |
|----------|---------|--|
| `ADDR` | `:5005` | Public listener |
| `INTERNAL_PORT` | `8005` | |
| `DATABASE_URL` | | Required. The owner for `migrate`, the writer for `serve` |
| `INTERNAL_TOKEN` | | Required by `serve`. The bearer this gateway sends |
| `WRITER_ROLE` | | Required by `migrate`. The role it grants |

`/health/ready` fails closed with **503** `unhealthy` when Postgres is unreachable.

### Gateway env

| Variable | |
|----------|--|
| `AUDIT_ENABLED` | Default `true`. `false` turns audit off for the deployment. Any other value refuses boot |
| `AUDIT_URL` | Required unless audit is off. Operator-audit's internal base URL, e.g. `http://operator-audit:8005` |
| `AUDIT_TOKEN` | Required unless audit is off. Sent as `Authorization: Bearer`. Same value as operator-audit's `INTERNAL_TOKEN` |

When audit is on and `AUDIT_URL` is missing, boot fails. A forgotten URL never turns audit off silently.

## Not here

- Request or response bodies, query strings, the token, or claims other than the actor fields, in an event
- 401s in the log
- Audit on for some routes or operators and off for others
- A client-chosen request id as an event's key
- Staff events in a Plat5 org log, or Plat5's audit service or database as this log's store
- This log shown to customers (deferred, [`../AGENTS.md`](../AGENTS.md#deferred))
- An actor header, or any other way a service learns who called
- This gateway reading inside details, or details creating, moving, or re-outcoming an event
- Changing an event after its outcome, or deleting events through an API
- Turning `pending` into anything else after the fact
- Trusted proxies, a reason for access, retention and purge, and export (deferred, [`../AGENTS.md`](../AGENTS.md#deferred))
