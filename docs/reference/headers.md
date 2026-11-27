# Headers

Every header this gateway sets, strips or reads. Plat5 defines `X-Plat5-*` headers other than `X-Plat5-Operator-Id` and `X-Plat5-Operator-Reason` (Plat5 `docs/reference/headers.md`); this gateway sends Plat5's headers with Plat5's meaning and values. Why: [requests](../concepts/requests.md).

## From clients

| Header | When | Value |
|--------|------|-------|
| `X-Plat5-As-Member` | Optional, on node-scoped routes | Member id to act as, active in the route node's root tree. Read by this gateway, passed to AuthZEN, never forwarded |
| `X-Plat5-Operator-Reason` | Optional, on any service route | Support ticket or incident reference (trimmed valid UTF-8, no control chars, 1–256 characters, single header). Recorded in `operator-audit`, passed to AuthZEN, never forwarded |

## To services

| Header | When | Value |
|--------|------|-------|
| `X-Plat5-Operator-Id` | Every request | The operator id claim ([`reference/idp.md`](idp.md)). This plane's own header |
| `X-Plat5-<Kind>-Id` | The matched route template has `node` | One per node in its chain, as Plat5 sends them: the kind in title case, `_` becoming `-` (`project` → `X-Plat5-Project-Id`) |
| `X-Plat5-Member-Id` | Acting as a member | The member from `X-Plat5-As-Member`, active in the chain's root |
| `X-Plat5-Principal` | Acting as a member | The member's principal as identity names it: `person` or `service_account` |
| `X-Request-ID` | Every request | Generated here. A client's is replaced |
| `traceparent` | When the client sent one | Passed through |
| `X-Plat5-Signature` | When `UPSTREAM_SIGNING_KEY` is set | Ed25519 request signature in Plat5's `v1` format (`v1,kid=<kid>,t=<unix_seconds>,sig=<base64url_nopad>`), signing `v1`, `kid`, `t`, uppercase method, `path_and_query` (stripped of `/services/{service}`), `X-Request-ID`, and every outgoing `X-Plat5-*` header (including `X-Plat5-Operator-Id`) sorted by lowercase name |

Never sent: the operator's token, the operator's email or claims, `X-Plat5-Operator-Reason`, an `X-Plat5-Principal` without a member, `X-Plat5-User-Id`, the member's bindings.

## Stripped

| Header | Direction | Why |
|--------|-----------|-----|
| `Authorization`, `Cookie`, `X-API-Key` | Request | The operator's credential, or one that isn't accepted here. Never reaches a service |
| Every `X-Plat5-*` | Request, before this gateway sets its own | A client can't name a node, a member, a reason, or an operator (`X-Plat5-As-Member` and `X-Plat5-Operator-Reason` are read here first) |
| `X-Request-ID` | Request and response | This gateway owns the request id |
| Every `X-Plat5-Audit-*` | Response | What the service tells the audit log ([audit](../concepts/audit.md#what-a-service-adds)). Read first when audit is on; stripped on every route, audit on or off |
| `Access-Control-*` | Response | This gateway owns CORS |

## From services

| Header | Read |
|--------|------|
| `X-Plat5-Audit-Details` | Into the event's `details` |
| `X-Plat5-Audit-Created` | Into the event's `created_id` |
| `X-Plat5-Audit-Nodes`, `X-Plat5-Audit-Member` | Not read. Stripped |
