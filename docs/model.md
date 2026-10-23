# Model

> The operator plane's model: what it is, the rules that hold everywhere, and what it won't do. The other docs are the wire; when they disagree with this page, they are wrong.

Two front doors, one service contract. Neither gateway calls the other.

```
staff token     →  operator gateway  →  service
customer cred   →  customer gateway  →  same service, same API
```

Services aren't on the public internet, and they do no auth: any front door on the private network can call them (Plat5 `docs/model.md`, principle 6). This gateway is one. It dials service addresses directly. It doesn't proxy through the customer gateway and doesn't read the customer route map.

This gateway is an API. It serves no UI. A console is a client of it, like a script or a CLI, and lives somewhere else.

## Actor and target

| | Operator plane | Customer plane |
|--|----------------|----------------|
| Who is calling | A staff member or staff automation, holding a token from the staff IdP | A person or service account, authenticated at the customer gateway |
| How the target is named | Node ids in the path, the service's own path | Slugs in the path, which the customer gateway resolves to ids |
| What the service is told | The path, `X-Plat5-Principal: operator`, `X-Plat5-Operator-Id`, and `X-Plat5-Member-Id` only when acting as a member | The path, the node ids, `X-Plat5-Member-Id`, `X-Plat5-Principal: person \| service_account` |

The operator is the actor. The path is the target. The service implements the action against the path. It may record who acted, from the headers, but it never decides access from them: access is the front door's job.

The operator id is not a customer member, not a principal, and not a customer IdP subject. It's never written into the path.

## Acting as a member

A service that records authors needs `X-Plat5-Member-Id` (Plat5 `docs/gateway-contract.md`, rules for services). An operator supplies one by acting as a member, and only that way: the path says so.

```
POST /as/{member}/projects/n_01J…/plans/pro/versions
→ POST /projects/n_01J…/plans/pro/versions
  X-Plat5-Member-Id: {member}
  X-Plat5-Operator-Id: 00u1abc
  X-Plat5-Principal: operator
```

- **Only on routes that allow it.** A route with `as_member: true` is served at `/as/{member}` followed by its path. This gateway strips the prefix and forwards the rest exactly. A route without it never sends a member id. See [`routes.md`](routes.md).
- **The member must be active in the node's tree.** The route names which param is its node (`node: project_id`). Before forwarding, this gateway asks Plat5 identity for that node's root and the member in it. A member not in that tree, or removed, is **400**.
- **Access is the operator's, never the member's.** Authz decides the request for the operator ([`authz.md`](authz.md)). The member's bindings and labels are never read or applied.
- **Both ids reach the service and the log.** The service sees the member and the operator. The staff audit log records both ([`audit.md`](audit.md)).

Without `/as/`, a request carries no member id. A service that needs one refuses it as malformed; that's correct, and the operator retries as a member if the change should be authored.

## Who is an operator

Whoever holds a valid token from the configured staff issuer, for a configured audience. See [`idp.md`](idp.md).

This gateway stores no accounts, passwords, or sessions. Adding or removing staff is done in the IdP, by assigning or unassigning the application. The customer IdP is never the staff IdP.

The operator id is the `sub` claim unless configured otherwise.

## Credential

`Authorization: Bearer <jwt>`. Nothing else. No cookies, no API keys.

Before forwarding, this gateway strips `Authorization`, `Cookie`, `X-API-Key`, and every incoming `X-Plat5-*` header. The upstream never sees a staff credential, and a client can't name a member or an operator.

## Request order

1. `OPTIONS` preflight from an allowed origin → answered here. Never forwarded. Not audited.
2. Missing or invalid token → **401** `UNAUTHORIZED`. Not audited.
3. Check the path and match a route. Nothing is answered yet.
4. When audit is on, write the audit intent → **503** `SERVICE_UNAVAILABLE` if it can't be written. See [`audit.md`](audit.md).
5. Path not acceptable (see below) → **400** `INVALID_REQUEST`. No configured route for this method and path → **404** `NOT_FOUND`.
6. Acting as a member: the member isn't active in the node's tree → **400** `INVALID_REQUEST`. Identity unreachable → **503**.
7. Authorization, when configured → see [`authz.md`](authz.md).
8. Forward, with the headers above.
9. When audit is on, write the audit outcome, in the background.

Authentication comes before route matching. An unauthenticated caller learns nothing about the route list. With audit on, every request past step 2 is an audit event, including a 400 or a 404.

## Forwarding

The path is forwarded exactly as received, after the `/as/{member}` prefix is stripped on a route that allows it. The query string is forwarded unchanged. The body is forwarded unchanged and not read.

Because the matched path is the path the service sees, this gateway refuses paths it could read differently from the service: dot segments (`.`, `..`), empty segments (`//`), and encoded `/` or `\` in a segment. Those are **400**.

Upstream status and body pass through untranslated. Upstream dial failure or timeout → **503** `SERVICE_UNAVAILABLE`.

`X-Plat5-Audit-Details` is stripped on every route: from the request before upstream, and from the response before the client. This gateway reads it from the response first ([`audit.md`](audit.md#details)).

## Request id and tracing

This gateway generates a request id for every request. A client's `X-Request-ID` is not accepted; the generated id replaces it. The id is forwarded upstream, returned on every response, and is the audit event's key. `traceparent` passes through.

## Audit

With audit on (the default), every authenticated request is an event in the staff audit log, written before the service is called. See [`audit.md`](audit.md). Services already log `X-Request-ID`, and the two join on it.

This gateway also writes one log line per request, for operations: `request_id`, `method`, `route`, `params`, `status`, `duration_ms`, and, once authenticated, `operator_id`, `operator_email`, and `member_id` when acting as a member. With audit on, the line is not the audit record. With audit off, it is the only record.

## Lists

A service never filters a response by operator. This gateway never filters a response body. Which customers an operator may see is decided per request from the path, as in [`authz.md`](authz.md). Plat5 identity's unfiltered node list (`GET /nodes`) is the tree browser.

## Errors

Plat5 envelope: `error.type`, `error.code`, `error.message`, `error.request_id`. Clients branch on `code` and HTTP status. `message` is safe to show.

| Case | HTTP / code |
|------|-------------|
| Missing or invalid token | **401** `UNAUTHORIZED`, with `WWW-Authenticate: Bearer` |
| Unacceptable path, or a member not active in the node's tree | **400** `INVALID_REQUEST` |
| No configured route | **404** `NOT_FOUND` |
| Denied by authz | **403** `FORBIDDEN` |
| Staff IdP keys never fetched, audit intent not written, identity or upstream unreachable, or authz service unreachable | **503** `SERVICE_UNAVAILABLE` |

## Browser clients

A browser client gets its own token from the IdP (authorization code with PKCE) and sends it as a bearer. `ALLOWED_ORIGINS` lists the origins that may call. Empty means no CORS headers: only same-origin and non-browser clients work. There is no wildcard. Credentials mode is not needed and not allowed.

## Rules

- **The operator is the actor; the path is the target.** Node ids in the path, never slugs.
- **The operator id is never in the path.** It's sent as `X-Plat5-Operator-Id`, with `X-Plat5-Principal: operator`, on every request.
- **A member id is sent only when the path says `/as/{member}`,** on a route that allows it, for a member active in the route node's tree.
- **The operator's role is never sent.** Neither are the member's bindings or labels, which this gateway never reads.
- **The only credential is a bearer JWT from the staff IdP,** with a required audience ([`idp.md`](idp.md)).
- **This gateway stores no accounts, passwords, or sessions.**
- **Own route list.** No customer gateway, no control plane ([`routes.md`](routes.md)).
- **Path forwarded exactly as matched,** after the `/as/{member}` prefix. Ambiguous paths are **400**.
- **This gateway generates every request id.** A client's isn't accepted.
- **Audit is on or off for the whole deployment, and on by default.** On, every authenticated request is an event, written before forward ([`audit.md`](audit.md)).
- **The staff log is in the operator plane's own Postgres, never Plat5's.** Customers don't see it. Events are append-only; `operator-audit` runs as a role that can't delete.
- **`X-Plat5-Audit-Details` is Plat5's contract,** stored as sent. Stripped both ways on every route.
- **Authz is an AuthZEN decision service or nothing.** Fail closed ([`authz.md`](authz.md)).
- **Lists are all-or-nothing actions.** Nothing filters a response by operator.
- **Errors use the Plat5 envelope.**

## Not in the operator plane

Do not add these because they would be convenient:

- Serving UI or static files
- Operator accounts, passwords, cookies, or sessions
- API keys (deferred, [`../TODO.md`](../TODO.md))
- The operator's role on the upstream request, or a member id without `/as/{member}`
- Applying the impersonated member's bindings or labels to the request
- The customer IdP, or a customer member or principal, as an operator
- Proxying through the customer gateway, or publishing routes into its route map
- Slug resolution. Operators address nodes by id
- A built-in policy, role check, or groups check. Authz goes through AuthZEN
- Sending the staff token to the decision service
- Caching authz decisions
- Filtering a response body, or asking a service to filter by operator
- Operator-specific rules in Plat5. This plane uses Plat5's public and documented APIs only
- Minting customer credentials, or writing another product's database
- Audit on for some routes or operators and off for others
- 401s, query strings, bodies, tokens, or claims other than the actor fields in an audit event
- A client-chosen request id
- Staff events in a Plat5 tree's log, or Plat5's audit service or database as the staff log's store
- Updating or deleting audit events through an API, or running `operator-audit` as a role that can delete
- Reading inside `X-Plat5-Audit-Details`

## What this is not

- A UI host
- An IdP, an account store, or a session store
- A dependency of the customer gateway, or a mode of it
- A place that stores customer people, orgs, or members
- The system that decides a member's permissions inside an org
