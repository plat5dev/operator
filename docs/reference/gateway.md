# Gateway

The operator gateway (`cmd/operator/`). How it decides a request: [requests](../concepts/requests.md). Config: [`reference/config.md`](config.md). Headers: [`reference/headers.md`](headers.md). Environment: [`reference/env.md`](env.md#gateway).

## Identity

On a matched route template with `node`, this gateway calls Plat5 identity's public service API at `IDENTITY_URL` (or `services.identity.url`), with the request's `X-Request-ID`:

| Call | For | Answer |
|------|-----|--------|
| `GET /nodes/{node_id}` | The route node and its `ancestors`, root first | **404**: not found (**404** to the client). An invalid node or inconsistent ancestors is identity failing |
| `GET /nodes/{root_id}/members/{member_id}` | Acting as a member, the member from `X-Plat5-As-Member` | `status: active` passes. **404**, or any other status, is a member not active in the tree (**400** to the client) |

Any other answer, a timeout (2 seconds per call), or a transport error is identity unreachable: **503**.

### Cache

A node's kind and parent never change, and ids are never reused, so each node's place in the tree is cached for the life of the process, up to 100,000 nodes. A full cache is cleared and refilled. Status isn't cached. Not-found answers aren't cached.

Member lookups aren't cached: a removed member is refused on the next request.

## Authorizer (OpenID AuthZEN 1.0)

When `authorizer.mode` is `authzen` (default), every valid service request calls `POST {url}/access/v1/evaluation` live (no caching, fail closed):

- **Subject:** `{ "type": "operator", "id": "<operator_id>", "properties": { "issuer": "…", "email"?: "…", "client_id"?: "…", "claims"?: { … } } }`.
- **Action:** `{ "name": "<METHOD>", "properties": { "service": "…", "method": "…", "path": "<upstream_path>", "route"?: "<template>", "acting_as"?: "<member_id>", "acting_as_principal"?: "person"|"service_account", "reason"?: "…" } }`.
- **Resource:**
  - When a node chain was resolved: `{ "type": "<leaf_kind>", "id": "<leaf_id>", "properties": { "service": "…", "root_id": "…", "<kind>_id": "…", "nodes": […], "params"?: {…}, "plat5_permissions"?: […] } }`.
  - When unscoped: `{ "type": "service", "id": "<service>", "properties": { "service": "…", "path": "<upstream_path>", "route"?: "<template>", "params"?: {…} } }`.
- **Context:** `{ "client_ip": "…", "request_id": "…" }`.

`decision: true` forwards; `decision: false` answers **403** `FORBIDDEN` (passing a string `context.reason` from the PDP into `error.details.reason` and recording `{ "policy_reason": "…" }` in the audit outcome). Transport errors, timeouts, redirects, non-2xx responses, or invalid JSON answer **503** `SERVICE_UNAVAILABLE`.

When `authorizer.mode` is `disabled`, the gateway logs `authorizer: disabled` at warn level at boot and permits every authenticated request.

## Route checks

`POST /access/routes` lets an operator console hide controls the logged-in operator cannot use. Authenticated with the operator JWT; not audited and never forwards.

```json
POST /access/routes
Authorization: Bearer …
{
  "checks": [
    { "method": "GET", "path": "/services/identity/nodes/01jbx…" },
    { "method": "POST", "path": "/services/api/projects/01jbx…/tasks", "as_member": "01jby…", "reason": "ZD-4821" }
  ]
}
→ 200
{
  "checks": [
    { "allowed": true },
    { "allowed": false, "reason": "ticket reason required for writes" }
  ]
}
```

- `checks`: 1 to 100 items, body up to 256 KiB (**413** over cap, **422** on invalid body/items).
- Each item validates the `/services/{service}/{path…}` target, service existence, node chain (and `as_member` if set), and batches valid items into a single AuthZEN `POST {url}/access/v1/evaluations` call (`evaluations_semantic: "execute_all"`). Unmatched services, invalid `as_member`, or missing nodes answer `{ "allowed": false }`.
- When `authorizer.mode` is `disabled`, valid items answer `{ "allowed": true }`.
- Identity or authorizer unavailability answers **503**.

## Client IP

What an audit event records as `ip`, and what AuthZEN receives in `context.client_ip`.

The TCP peer, unless the peer is in `TRUSTED_PROXIES` (comma-separated addresses and CIDRs). Then `X-Forwarded-For` is walked from the right, past trusted proxies, and the first address that isn't one is the client. Nothing left of an untrusted hop is read.

`TRUSTED_PROXIES` empty (the default): `X-Forwarded-For` is ignored.

## CORS

`ALLOWED_ORIGINS` lists the origins whose browser clients may call. An `Origin` in the list is echoed with `Vary: Origin`, and its preflight is answered here: **204**, `Access-Control-Allow-Methods`, `Access-Control-Allow-Headers` (`Authorization, Content-Type, X-Plat5-As-Member, X-Plat5-Operator-Reason, X-Request-ID, traceparent`) and `Access-Control-Max-Age: 600`. Responses expose `X-Request-ID`. Empty means no CORS headers. There is no wildcard, and credentials mode isn't used.

## Security headers

On every API-port response, errors included:

| Header | Value |
|--------|-------|
| `X-Content-Type-Options` | `nosniff` |
| `X-Frame-Options` | `DENY` |
| `Referrer-Policy` | `strict-origin-when-cross-origin` |

## Boot, reload and ready

Boot refuses a missing or invalid `CONFIG_FILE`, missing operator IdP config, invalid `TRUSTED_PROXIES`, invalid signing key config, `authorizer.mode: authzen` without `url` (or missing `token_env` value), or audit on without `AUDIT_URL`. JWKS and (when configured) the control-plane feed run in the background.

`CONFIG_FILE` can be reloaded without restarting via `POST /reload` on `INTERNAL_PORT` or `SIGHUP`. If invalid, reload is refused (**422** on `POST /reload`) and the previous config stays active.

`/health/ready` is **200** once JWKS has been fetched, otherwise **503**. Neither identity, control plane, authorizer, nor operator-audit is part of ready.

## Errors

All use the Plat5 envelope (Plat5 `docs/reference/errors.md`).

| Case | HTTP / code |
|------|-------------|
| Missing or invalid token | **401** `UNAUTHORIZED`, with `WWW-Authenticate: Bearer` and `details.reason`: `missing`, `expired` or `invalid` |
| Unacceptable path, invalid/unscoped `X-Plat5-As-Member`, member not active, or invalid `X-Plat5-Operator-Reason` | **400** `INVALID_REQUEST` |
| AuthZEN denied | **403** `FORBIDDEN` (with `details.reason` when PDP returned `context.reason`) |
| Path not under `/services/{service}/…`, unknown service, or route node not found | **404** `NOT_FOUND` (node adds `details: { resource: "node", id }`) |
| `/access/routes` body over 256 KiB | **413** `PAYLOAD_TOO_LARGE` |
| Bad `/access/routes` body | **422** `VALIDATION_ERROR` |
| Operator IdP keys never fetched, audit intent not written, identity or authorizer unreachable | **503** `SERVICE_UNAVAILABLE` |
| Upstream unreachable, timed out, or failing mid-response | **502** with the `SERVICE_UNAVAILABLE` code |

## Request log

One `request` line per request, JSON on stdout:

| Field | When |
|-------|------|
| `request_id`, `method`, `status`, `duration_ms` | Always |
| `operator_id`, `operator_email` | Authenticated; the email when the token has one |
| `service`, `route`, `params` | Known service / matched template |
| `root_id`, `node_id` | The route node's chain was found |
| `member_id` | Acting as a member |
| `reason` | `X-Plat5-Operator-Reason` sent |

## Metrics

Prometheus text format on `/metrics`, `INTERNAL_PORT`.

| Metric | Labels | |
|--------|--------|--|
| `operator_unauthenticated_total` | `reason` (`missing`, `expired`, `invalid`) | 401s, which aren't events |
| `operator_authorizer_evaluations_total` | `decision` (`allow`, `deny`, `error`) | AuthZEN evaluations |
| `operator_authorizer_duration_seconds` | `decision` | Histogram of AuthZEN latency |
| `operator_audit_writes_total` | `write` (`intent`, `outcome`), `result` (`ok`, `failed`, `dropped`, `not_found`) | |
| `operator_audit_intent_duration_seconds` | `result` | Histogram. Time the client waited on the intent |
| `operator_audit_outcome_queue` | | Outcomes waiting to be sent |

operator-audit reports `operator_audit_stale_pending`: events from the last 24 hours still `pending` after 10 minutes, checked every minute.
