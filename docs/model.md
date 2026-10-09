# Model

Two front doors, one service contract. Neither gateway calls the other.

```
staff token     →  operator gateway  →  service
customer cred   →  customer gateway  →  same service, same path
```

Services are not on the public internet. A gateway is the front door. This one dials the service address directly. It does not proxy through the customer gateway and does not read the customer route map.

This gateway is an API. It serves no UI. A console is a client of it, like a script or a CLI, and lives somewhere else.

## Actor and target

| | Operator plane | Customer plane |
|--|----------------|----------------|
| Who is calling | A staff member or staff automation, holding a token from the staff IdP | A customer credential, authenticated at the customer gateway |
| What the service is told | The path | The path |
| What the path means | The user, org, or member the action applies to | The same. There the caller and the target are the same, because the gateway derived the path from the credential |

The operator is the actor. The path is the target. The service implements the action against the path. It does not decide which plane was allowed to ask. Access is the front door's job.

The operator id is not a customer `user_id`, not a member, and not a customer IdP subject. It is never written into the path. No actor header is injected. A service that branches on "was this an operator?" has left this model.

## Who is an operator

Whoever holds a valid token from the configured staff issuer, for a configured audience. See [`idp.md`](idp.md).

This gateway stores no accounts, passwords, or sessions. Adding or removing staff is done in the IdP, by assigning or unassigning the application. The customer IdP is never the staff IdP.

The operator id is the `sub` claim unless configured otherwise.

## Credential

`Authorization: Bearer <jwt>`. Nothing else. No cookies, no API keys.

Before forwarding, this gateway strips `Authorization`, `Cookie`, and `X-API-Key`. The upstream never sees a staff credential.

## Request order

1. `OPTIONS` preflight from an allowed origin → answered here. Never forwarded. Not audited.
2. Missing or invalid token → **401** `UNAUTHORIZED`. Not audited.
3. Check the path and match a route. Nothing is answered yet.
4. When audit is on, write the audit intent → **503** `SERVICE_UNAVAILABLE` if it cannot be written. See [`audit.md`](audit.md).
5. Path not acceptable (see below) → **400** `INVALID_REQUEST`. No configured route for this method and path → **404** `NOT_FOUND`.
6. Authorization, when configured → see [`authz.md`](authz.md).
7. Forward.
8. When audit is on, write the audit outcome, in the background.

Authentication comes before route matching. An unauthenticated caller learns nothing about the route list. With audit on, every request past step 2 is an audit event, including a 400 or a 404.

## Forwarding

The path is forwarded exactly as received. The query string is forwarded unchanged. The body is forwarded unchanged and not read.

Because the matched path is the path the service sees, this gateway refuses paths it could read differently from the service: dot segments (`.`, `..`), empty segments (`//`), and encoded `/` or `\` in a segment. Those are **400**.

Upstream status and body pass through untranslated. Upstream dial failure or timeout → **503** `SERVICE_UNAVAILABLE`. That is this gateway's rejection, not a downstream status.

`X-Plat5-Audit-Details` is stripped on every route: from the request before upstream, and from the response before the client. This gateway reads it from the response first ([`audit.md`](audit.md#details)).

## Request id and tracing

This gateway generates a request id for every request. A client's `X-Request-ID` is not accepted; the generated id replaces it. The id is forwarded upstream, returned on every response, and is the audit event's key. `traceparent` passes through.

## Audit

With audit on (the default), every authenticated request is an event in the staff audit log, written before the service is called. The event is the record of who did what. See [`audit.md`](audit.md). Services already log `X-Request-ID`, and the two join on it.

This gateway also writes one log line per request, for operations. The line carries `request_id`, `method`, `route`, `params`, `status`, `duration_ms`, and, once authenticated, `operator_id` and `operator_email`. With audit on, the line is not the audit record. With audit off, it is the only record.

## Lists

A service never filters a response by operator. This gateway never filters a response body. Which customers an operator may see is decided per request from the path, as in [`authz.md`](authz.md).

## Errors

Plat5 envelope: `error.type`, `error.code`, `error.message`, `error.request_id`. Clients branch on `code` and HTTP status. `message` is safe to show.

| Case | HTTP / code |
|------|-------------|
| Missing or invalid token | **401** `UNAUTHORIZED`, with `WWW-Authenticate: Bearer` |
| Unacceptable path | **400** `INVALID_REQUEST` |
| No configured route | **404** `NOT_FOUND` |
| Denied by authz | **403** `FORBIDDEN` |
| Staff IdP keys never fetched, audit intent not written, upstream unreachable, or authz service unreachable | **503** `SERVICE_UNAVAILABLE` |

## Browser clients

A browser client gets its own token from the IdP (authorization code with PKCE) and sends it as a bearer. `ALLOWED_ORIGINS` lists the origins that may call. Empty means no CORS headers: only same-origin and non-browser clients work. There is no wildcard. Credentials mode is not needed and not allowed.

## What this is not

- A UI host
- An IdP, an account store, or a session store
- A dependency of the customer gateway, or a mode of it
- A place that stores customer users, orgs, or members
- The system that decides a member's permissions inside an org
