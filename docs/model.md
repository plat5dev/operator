# Model

Two front doors, one service contract. Neither gateway calls the other.

```
operator account  →  operator gateway  →  service
customer cred     →  customer gateway  →  same service, same headers
```

Services are not on the public internet. A gateway is the front door. This one dials the service address directly. It does not use the customer gateway as a proxy, and it does not read the customer route map.

Both gateways are trusted injectors. Header trust is the same perimeter Plat5 already has: whoever can reach the service port can forge the headers. Reaching the service through this gateway does not make the headers mean something else.

## Actor and target

| | Operator plane | Customer plane |
|--|----------------|----------------|
| Who is calling | An operator account, authenticated here | A customer credential, authenticated at the customer gateway |
| What the service is told | `X-User-Id`, or `X-Organization-Id` + `X-Member-Id` | The same headers |
| What those headers mean | The user or member the action applies to | The same. On that plane the caller and the target are the same person, because the gateway derived the headers from the credential |

The service implements the action against that target. It does not decide which plane was allowed to ask. Access is the front door's job.

The operator id is not a customer `user_id`, not a member, and not a Plat5 Auth subject. It is never written into `X-User-Id`, `X-Organization-Id`, or `X-Member-Id`.

No actor header is injected. A service that branches on "was this an operator?" has left this model. Attribution stays in this plane.

## Injection

The operator client sends the target headers to **this** gateway, with an operator credential.

The browser credential is an httpOnly cookie. The cookie value is the bearer token. JavaScript does not read it. Any other client sends `Authorization: Bearer`. If both are present, the bearer is the credential.

1. Missing or bad operator credential → **401** `UNAUTHORIZED`. Target headers are not forwarded.
2. This gateway strips client identity headers, `Authorization`, and `Cookie`.
3. It re-injects only the target the authenticated operator declared, and only the headers the route requires.
4. The upstream does not see the operator credential.

| Route requires | Headers |
|----------------|---------|
| user | `X-User-Id` |
| organization | `X-Organization-Id`, `X-Member-Id` |
| none | no identity headers |

A required header the client did not declare → **400** `VALIDATION_ERROR`. That is this gateway's rejection. It is not a downstream 500.

When the path contains an organization id, it must equal `X-Organization-Id`. Mismatch → **400** `VALIDATION_ERROR`.

No configured route → **404** `NOT_FOUND`.

Upstream status and body are passed through. This gateway does not translate them.

Upstream dial failure → **503** `SERVICE_UNAVAILABLE`. That is this gateway's rejection. It is not a downstream status.

## Attribution

This gateway logs the operator id, `X-Request-ID`, and the target headers it injected. That log is who did it.

It generates `X-Request-ID` when the client did not send one, forwards it upstream, and returns it on the response. Services already log that id. The two logs join on it.

## Identity

Identity's public API is one of the APIs this gateway may front. It still enforces that user's role from `X-User-Id` and the path. Injecting a user's id does not grant more than that user can do. This plane does not override that.

## Errors

Plat5 envelope: `error.type`, `error.code`, `error.message`, `error.request_id`. Clients branch on `code` and HTTP status. `message` is safe to show.

| Case | HTTP / code |
|------|-------------|
| Bad or missing operator credential | **401** `UNAUTHORIZED` |
| Missing required target header, or path org mismatch | **400** `VALIDATION_ERROR` |
| No configured route | **404** `NOT_FOUND` |
| Upstream dial failure | **503** `SERVICE_UNAVAILABLE` |

## What this is not

- A dependency of the customer gateway, or a mode of it
- A second listener with a different service contract
- A place that stores customer users, orgs, or members
- The system that decides a member's role inside an org
