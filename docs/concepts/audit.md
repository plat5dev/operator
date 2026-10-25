# Audit

The operator audit log: which operator called which route, on which nodes, as which member, from where, and how the request ended. This gateway records every authenticated request. The **operator-audit** service, in this repo, stores the events and serves them back through this gateway.

Plat5 keeps an audit log per tree, a customer's record of its own members, and one per person for their own routes. This log is separate and in a different store. An operator request is never written into a tree's log, even when acting as a member, and customers don't read this one.

## The model

An event is one request, not one change. A service may add what changed ([details](#what-a-service-adds)). Nothing else.

| Piece | Says | Owner |
|-------|------|-------|
| Actor | The operator, from the token | This gateway (authentication) |
| Acting as | The member the operator acted as, and its principal | This gateway (`/as/{member}`, checked against identity) |
| Nodes | The route node's chain, root first | This gateway (identity) |
| Action | Service, method, route template, path params | This gateway (route match) |
| Origin | IP, user agent | This gateway |
| Outcome | How the request ended | This gateway |
| Details | What changed | The service, on `X-Plat5-Audit-Details` |
| Created | The id a create produced | The service, on `X-Plat5-Audit-Created` |
| Store and read API | | `operator-audit` |

This gateway is the only writer. Services are told the operator id and, when acting as one, the member ([requests](requests.md#acting-as-a-member)); the record of who did what is still this log. Details flow from the service to this gateway, never the other way.

Audit is on or off for the whole deployment. There is no mix.

| | Audit on (default) | Audit off (`AUDIT_ENABLED=false`) |
|---|---|---|
| `AUDIT_URL`, `AUDIT_TOKEN` | Required to boot | Ignored |
| `operator-audit` | Runs | Not run |
| Authenticated request | Intent written before it's answered or forwarded. **503** if it can't be | Answered or forwarded with no event |
| `X-Plat5-Audit-*` | Read, stored, stripped | Stripped |
| The `operator-audit` route | In the route file | Left out. A route to a service that isn't running is **502** |

This gateway logs `audit: off` at warn level at boot. Turning audit on doesn't backfill.

## Which requests

With audit on, every request that authenticates is recorded, whatever its method. No route opts out. Reads are recorded, because operators reading customer data is the access this log exists to show.

| Gateway result | Event |
|----------------|-------|
| `OPTIONS` preflight | None. It's answered before authentication |
| **401** | None. There's no trusted actor, and nothing bounds the rate. The request log line and `operator_unauthenticated_total` count it |
| **503**, operator IdP keys never fetched | None. The token can't be judged, so there's no actor |
| **503**, intent not written | None, or `rejected` if the intent landed unseen ([delivery](#delivery)) |
| **400** unacceptable path, **404** no route | `rejected`. `route` is `null`, `path` is set |
| **404** route node not found | `rejected`. `route` is set, `nodes` is `[]` |
| **400** member not active in the node's tree | `rejected`. `nodes` is set, `acting_as` is `null` |
| **503** identity unreachable | `rejected`. `nodes` is `[]` |
| Forwarded | `responded` or `no_response` |

The intent is written after the route match and the lookups in identity, and before anything past authentication answers ([request order](requests.md#how-a-request-is-decided)).

## Delivery

Two writes per event, both keyed by `request_id`.

**Intent.** Written before this gateway answers or forwards, and the client waits on it. It carries every event field except the outcome fields. Each attempt times out at 500ms, with up to 3 attempts within 1s. If none succeeds, this gateway answers **503** and doesn't call the service. It then sends a `rejected` / `503` outcome in the background, because the intent may have landed without this gateway seeing it.

**Outcome.** Written after the response, or the failure. The client doesn't wait on it. This gateway queues it in process and retries with backoff for about five minutes. The queue is bounded. An outcome that overflows the queue or runs out of retries is dropped, logged and counted. On shutdown, this gateway stops taking requests, then sends what is queued for up to 10 seconds.

Both writes are idempotent. A retried intent doesn't create a second event. An outcome is applied once, `pending` → final; a later outcome for the same request changes nothing.

**The guarantee:** a request doesn't reach its service unless its intent is recorded. If this gateway dies or drops the outcome, the event stays `pending`. Pending is not success, and nothing turns it into one. Stale pending events are counted ([metrics](../reference/gateway.md#metrics)).

**The cost:** one internal round trip on every authenticated request, before this gateway answers or forwards.

Audit isn't part of this gateway's `/health/ready`. With audit on, every authenticated request is **503** while operator-audit is down.

## What a service adds

Headers on the upstream response, as Plat5 defines them (Plat5 `docs/concepts/audit.md`). This gateway reads them as Plat5's gateway does and doesn't import Plat5 code. A service doesn't know which front door called it: what it sends on an operator request is what it sends to Plat5's gateway.

| Header | Becomes | Rule |
|--------|---------|------|
| `X-Plat5-Audit-Details` | `details` | One JSON object, visible ASCII only, at most 4096 bytes. Only on `responded`. Stored as sent; this gateway never reads inside it |
| `X-Plat5-Audit-Created` | `created_id` | One id: visible ASCII, one path segment, at most 128 bytes. Only on `responded` |

Either one invalid is recorded as `null`, with a warning. Every `X-Plat5-Audit-*` header is stripped from the response on every route, audit on or off, and from the request before upstream. `X-Plat5-Audit-Nodes` and `X-Plat5-Audit-Member` name the tree a Plat5 user route changed; this log has no use for them and they're dropped.

None of them can create an event, or change its outcome, status, actor, nodes or member.

## Reading

`GET /operator-audit-events`, on operator-audit's public port, published through this gateway like any route. So each read is itself an event. Events come back newest first, filtered by actor, member acted as, node (an event on the node or anything below it), service, route, params, outcome, request id, created id and time. Shapes and filters: [`reference/api/operator-audit.md`](../reference/api/operator-audit.md).

Any operator can read the whole log until a decision service narrows it ([`TODO.md`](../../TODO.md)).

## Not here

- Request or response bodies, query strings, the token, or claims other than the actor fields, in an event
- 401s in the log
- Audit on for some routes or operators and off for others
- A client-chosen request id as an event's key
- Operator events in a Plat5 tree's log, or Plat5's audit service or database as this log's store
- This log shown to customers (deferred, [`TODO.md`](../../TODO.md))
- This gateway reading inside details, or a service header creating, moving or re-outcoming an event
- Changing an event after its outcome, or deleting events through an API
- Turning `pending` into anything else after the fact
- Trusted proxies, a reason for access, retention and purge, and export (deferred, [`TODO.md`](../../TODO.md))
