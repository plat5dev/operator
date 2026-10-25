# Headers

Every header this gateway sets, strips or reads. Plat5 defines `X-Plat5-*` headers other than `X-Plat5-Operator-Id` (Plat5 `docs/reference/headers.md`); this gateway sends them with Plat5's meaning and values. Why: [requests](../concepts/requests.md).

## To services

| Header | When | Value |
|--------|------|-------|
| `X-Plat5-Operator-Id` | Every request | The operator id claim ([`reference/idp.md`](idp.md)). This plane's own header |
| `X-Plat5-<Kind>-Id` | The route has `node` | One per node in its chain, as Plat5 sends them: the kind in title case, `_` becoming `-` (`project` → `X-Plat5-Project-Id`) |
| `X-Plat5-Member-Id` | Acting as a member | The member from `/as/{member}`, active in the chain's root |
| `X-Plat5-Principal` | Acting as a member | The member's principal as identity names it: `person` or `service_account` |
| `X-Request-ID` | Every request | Generated here. A client's is replaced |
| `traceparent` | When the client sent one | Passed through |

Never sent: the operator's token, the operator's email or claims, an `X-Plat5-Principal` without a member, the member's bindings or labels.

## Stripped

| Header | Direction | Why |
|--------|-----------|-----|
| `Authorization`, `Cookie`, `X-API-Key` | Request | The operator's credential, or one that isn't accepted here. Never reaches a service |
| Every `X-Plat5-*` | Request, before this gateway sets its own | A client can't name a node, a member or an operator |
| `X-Request-ID` | Request and response | This gateway owns the request id |
| Every `X-Plat5-Audit-*` | Response | What the service tells the audit log ([audit](../concepts/audit.md#what-a-service-adds)). Read first when audit is on; stripped on every route, audit on or off |
| `Access-Control-*` | Response | This gateway owns CORS |

## From services

| Header | Read |
|--------|------|
| `X-Plat5-Audit-Details` | Into the event's `details` |
| `X-Plat5-Audit-Created` | Into the event's `created_id` |
| `X-Plat5-Audit-Root`, `X-Plat5-Audit-Member` | Not read. Stripped |
