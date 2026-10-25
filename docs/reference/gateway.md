# Gateway

The operator gateway (`cmd/operator/`). How it decides a request: [requests](../concepts/requests.md). Headers: [`reference/headers.md`](headers.md). Environment: [`reference/env.md`](env.md#gateway).

## Identity

On a route with `node`, this gateway calls Plat5 identity's public service API at `IDENTITY_URL`, with the request's `X-Request-ID`:

| Call | For | Answer |
|------|-----|--------|
| `GET /nodes/{node_id}` | The route node, then each `parent_id` up to the root | **404**: not found. The route node missing is **404** to the client; an ancestor missing is identity failing |
| `GET /nodes/{root_id}/members/{member_id}` | Acting as a member, the member from `/as/{member}` | `status: active` passes. **404**, or any other status, is a member not active in the tree (**400** to the client) |

Any other answer, a timeout (2 seconds per call), or a transport error is identity unreachable: **503**.

### Cache

A node's kind and parent never change, and ids are never reused, so each node's place in the tree is cached for the life of the process, up to 100,000 nodes. A full cache is cleared and refilled. Status isn't cached: it isn't used. Not-found answers aren't cached.

Member lookups aren't cached: a removed member is refused on the next request.

## CORS

`ALLOWED_ORIGINS` lists the origins whose browser clients may call. An `Origin` in the list is echoed with `Vary: Origin`, and its preflight is answered here: **204**, `Access-Control-Allow-Methods`, `Access-Control-Allow-Headers` (`Authorization, Content-Type, X-Request-ID, traceparent`) and `Access-Control-Max-Age: 600`. Responses expose `X-Request-ID`. Empty means no CORS headers: only same-origin and non-browser clients work. There is no wildcard, and credentials mode isn't used.

A browser client gets its own token from the operator IdP (authorization code with PKCE) and sends it as a bearer.

## Security headers

On every API-port response, errors included:

| Header | Value |
|--------|-------|
| `X-Content-Type-Options` | `nosniff` |
| `X-Frame-Options` | `DENY` |
| `Referrer-Policy` | `strict-origin-when-cross-origin` |

## Boot and ready

Boot refuses a missing or invalid route file, missing operator IdP config, or audit on without `AUDIT_URL`. JWKS is fetched in the background ([`reference/idp.md`](idp.md#keys)).

`/health/ready` is **200** once JWKS has been fetched, otherwise **503**. Neither identity nor operator-audit is part of ready: while either is down, the requests that need it are **503** and the rest keep working.

## Errors

All use the Plat5 envelope (Plat5 `docs/reference/errors.md`).

| Case | HTTP / code |
|------|-------------|
| Missing or invalid token | **401** `UNAUTHORIZED`, with `WWW-Authenticate: Bearer` and `details.reason`: `missing`, `expired` or `invalid` |
| Unacceptable path, or a member not active in the node's tree | **400** `INVALID_REQUEST` |
| No route for the method and path | **404** `NOT_FOUND` |
| The route node not found | **404** `NOT_FOUND`, `details: { resource: "node", id }` |
| Operator IdP keys never fetched, audit intent not written, identity unreachable | **503** `SERVICE_UNAVAILABLE` |
| Upstream unreachable, timed out, or failing mid-response | **502** with the `SERVICE_UNAVAILABLE` code |

A client disconnect writes no body.

## Request log

One `request` line per request, JSON on stdout:

| Field | When |
|-------|------|
| `request_id`, `method`, `status`, `duration_ms` | Always |
| `operator_id`, `operator_email` | Authenticated; the email when the token has one |
| `service`, `route`, `params` | A route matched |
| `root_id`, `node_id` | The route node's chain was found |
| `member_id` | Acting as a member |

With audit on, the line isn't the audit record. With audit off, it's the only record.

## Metrics

Prometheus text format on `/metrics`, `INTERNAL_PORT`.

| Metric | Labels | |
|--------|--------|--|
| `operator_unauthenticated_total` | `reason` (`missing`, `expired`, `invalid`) | 401s, which aren't events |
| `operator_audit_writes_total` | `write` (`intent`, `outcome`), `result` (`ok`, `failed`, `dropped`, `not_found`) | |
| `operator_audit_intent_duration_seconds` | `result` | Histogram. Time the client waited on the intent |
| `operator_audit_outcome_queue` | | Outcomes waiting to be sent |

operator-audit reports `operator_audit_stale_pending`: events from the last 24 hours still `pending` after 10 minutes, checked every minute. Alert on `failed`, `dropped`, and stale pending events. A stale pending event is a request that may have reached its service and has no recorded outcome; `GET /operator-audit-events?outcome=pending` finds them all.
