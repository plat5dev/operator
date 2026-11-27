# Requests

How this gateway decides a request: the credential, the order, service routing, route templates and node resolution, acting as a member, reason headers, AuthZEN authorization, and forwarding. The shapes are in [reference](../README.md#reference): [config](../reference/config.md), [headers](../reference/headers.md), [gateway](../reference/gateway.md).

## Credential

`Authorization: Bearer <jwt>` from the operator IdP, for a configured audience ([`reference/idp.md`](../reference/idp.md)). Nothing else: no cookies, no API keys.

## How a request is decided

Every request, in order:

1. **Preflight.** An `OPTIONS` preflight from an allowed origin is answered here. Never forwarded, never audited.
2. **Authenticate.** A missing or invalid token → **401** (not audited). Operator IdP keys never fetched → **503** (not audited).
3. **Route checks.** `POST /access/routes` runs batch AuthZEN checks for a console UI and returns without auditing or forwarding ([`reference/gateway.md`](../reference/gateway.md#route-checks)).
4. **Parse and match.** Validate the path and parse `/services/{service}/{upstream_path…}`. Look up `{service}` in the merged service registry (`operator.yml` + optional control-plane snapshot). Match `(method, upstream_path)` against that service's route templates (optional: unlisted paths on a known service are valid unscoped requests). Parse `X-Plat5-As-Member` and `X-Plat5-Operator-Reason`. Nothing is answered yet.
5. **Resolve.** On a matched route template with `node`: fetch the node's chain from identity, and when acting as a member, check that the member is active in the chain's root. Nothing is answered yet.
6. **Audit intent**, when audit is on → **503** if it can't be written ([`concepts/audit.md`](audit.md#delivery)).
7. **Answer what can't be forwarded.**
   - A path this gateway could read differently from the service → **400**.
   - Path not under `/services/{service}/{path…}`, or `{service}` unknown → **404**.
   - An invalid, repeated, or unscoped `X-Plat5-As-Member` header, or an invalid/repeated `X-Plat5-Operator-Reason` header → **400**.
   - The route node not found → **404**.
   - The member not active in the node's tree → **400**.
   - Identity unreachable → **503**.
8. **Authorize.** When `authorizer.mode` is `authzen` (default), call the external OpenID AuthZEN 1.0 PDP live (no cache):
   - `decision: false` → **403** `FORBIDDEN` (with optional `details.reason` from PDP `context.reason`, also recorded on the audit outcome as `details.policy_reason`).
   - PDP timeout, error, or invalid response → **503** `SERVICE_UNAVAILABLE` (fail closed).
   When `authorizer.mode` is `disabled`, every authenticated request that reached step 8 is permitted.
9. **Forward**, stripping `/services/{service}` from the path and setting the headers below.
10. **Outcome**, in the background, when audit is on.

Authentication comes first: an unauthenticated caller learns nothing about the service list. With audit on, every request past authentication (other than `POST /access/routes`) is an event, including **400**, **403**, **404**, and **503**, and its intent is written before anything answers it.

## Service routing and route templates

Requests address an upstream service explicitly:

```
GET /services/identity/nodes/01jbx9q3…
GET /services/api/projects/01jbx9q3…/tasks
GET /services/operator-audit/operator-audit-events
```

The gateway strips `/services/{service}` and forwards `/{upstream_path…}` unchanged (with the original query string) to that service's `url`.

For a known service, the gateway checks its route templates (in priority order: explicit routes in `operator.yml`, upstream routes discovered from Plat5's control plane, and built-in templates for `identity`, `audit`, and `operator-audit`):

- **Matched template:** gives the normalized `route`, extracted `params`, optional `node` param for chain resolution, and optional `plat5_permissions` (from control plane).
- **Unlisted path on a known service:** forwarded unscoped (`route: null`, `path: "/…"`, `nodes: []`, no `X-Plat5-<Kind>-Id` headers). AuthZEN still authorizes the request using `service`, `method`, and `path`.

Templates never act as an allowlist or denylist; authorization belongs solely to the AuthZEN PDP.

## The route node

When a matched route template names `node`, this gateway asks identity for the node and each ancestor, root first: the chain.

- **Sent as Plat5 sends it.** The service gets `X-Plat5-<Kind>-Id` for each node in the chain ([`reference/headers.md`](../reference/headers.md)).
- **Recorded.** The chain is the audit event's `nodes`, so the operator log can be read for a node and everything below it.
- **Sent to AuthZEN.** The leaf node becomes `resource` (`{ type: kind, id }`) with `root_id`, `<kind>_id`, and `nodes` in `resource.properties`.
- **Not found** → **404** `NOT_FOUND`. Deleted and purged nodes are found: their chain is sent like any other, and the service applies its own state rules.

## Acting as a member

A service that records authors needs `X-Plat5-Member-Id` (Plat5 `docs/guides/write-a-service.md`). An operator supplies one by sending `X-Plat5-As-Member: <member_id>`, and only that way.

```
POST /services/identity/nodes/01jbx9q3…/children/project
X-Plat5-As-Member: 01jbyb0d…
X-Plat5-Operator-Reason: ZD-4821
→ POST /nodes/01jbx9q3…/children/project
  X-Plat5-Organization-Id: 01jbx9q3…
  X-Plat5-Member-Id: 01jbyb0d…
  X-Plat5-Principal: person
  X-Plat5-Operator-Id: 00u1abc
```

- **Requires a resolved node.** Sending `X-Plat5-As-Member` on a request with no `node` (unscoped route or unlisted path), or sending an empty, malformed, or repeated value, is **400** `INVALID_REQUEST`.
- **The member must be active in the node's tree.** Identity's member lookup in the chain's root must answer `status: active`. Anything else is **400**.
- **Authorized by AuthZEN.** Whether an operator may act as a member on a given route (and whether a reason is required) is decided by the AuthZEN PDP (`action.properties.acting_as` is set).
- **The service is told the member and its principal.** `X-Plat5-As-Member` is stripped; the service gets `X-Plat5-Member-Id`, and `X-Plat5-Principal` (`person` or `service_account`).
- **Access is the operator's, never the member's.** The member's bindings are never read or applied.
- **Both are recorded.** The operator audit log has the operator as `actor` and the member as `acting_as`.

## Operator reason

Clients may send `X-Plat5-Operator-Reason` (for example, a support ticket or incident id like `ZD-4821` or `INC-412`, visible UTF-8, trimmed, at most 256 characters, single header). It is:

- recorded on the operator audit event (`reason`, filterable with `?reason=…`),
- passed to AuthZEN (`action.properties.reason`) so policies can require a reason (or specific ticket format) for writes, impersonation, or sensitive reads, and
- stripped before forwarding to services.

## Forwarding

The upstream path (with `/services/{service}` stripped), query string, and body are forwarded unchanged; the body isn't read.

This gateway refuses paths it could read differently from the service: dot segments (`.`, `..`), empty segments (`//`), backslashes, and encoded `/` or `\` in a segment. Those are **400**.

Before forwarding, this gateway strips `Authorization`, `Cookie`, `X-API-Key` and every incoming `X-Plat5-*` header, then sets its own. A client can't name a node, a member, or an operator, and the service never sees an operator's credential or reason header.

Upstream status and body pass through untranslated. An upstream that can't be reached, times out, or fails mid-response → **502**.

## Request id and tracing

This gateway generates a request id for every request. A client's `X-Request-ID` is not accepted; the generated id replaces it. The id is sent upstream, to identity, and to AuthZEN, returned on every response, and is the audit event's key. `traceparent` passes through.

## Lists

A service never filters a response by operator, and this gateway never filters a response body. Which customers an operator may see is the AuthZEN authorizer's question, per request.

## Errors

The Plat5 envelope (Plat5 `docs/reference/errors.md`): clients branch on `code` and HTTP status, and `message` is safe to show. Every case: [`reference/gateway.md`](../reference/gateway.md#errors).
