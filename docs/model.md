# Model

Two front doors, one service contract. Neither gateway calls the other.

```
operator account  →  operator gateway  →  service
customer cred     →  customer gateway  →  same service, same path
```

Services are not on the public internet. A gateway is the front door. This one dials the service address directly. It does not use the customer gateway as a proxy, and it does not read the customer route map.

The path names the subject. The customer gateway fills `{subject.*}` into `upstream` and proxies that path. This gateway forwards the path the operator named, after operator authn. The service sees the same path either way.

Whoever can reach the service port can write the path. Reaching the service through this gateway does not make the path mean something else.

## Actor and target

| | Operator plane | Customer plane |
|--|----------------|----------------|
| Who is calling | An operator account, authenticated here | A customer credential, authenticated at the customer gateway |
| What the service is told | The path | The path |
| What the path means | The user, org, or member the action applies to | The same. On that plane the caller and the target are the same person, because the gateway derived the path from the credential |

The service implements the action against that path. It does not decide which plane was allowed to ask. Access is the front door's job.

The operator id is not a customer `user_id`, not a member, and not a Plat5 Auth subject. It is never written into the path.

No actor header is injected. A service that branches on "was this an operator?" has left this model. Attribution stays in this plane.

## Forwarding

The operator client calls the service URL. The path names the target.

The browser credential is an httpOnly cookie. The cookie value is the bearer token. JavaScript does not read it. Any other client sends `Authorization: Bearer`. If both are present, the bearer is the credential.

1. Missing or bad operator credential → **401** `UNAUTHORIZED`.
2. This gateway strips `Authorization`, `Cookie`, and `X-API-Key`.
3. It forwards the path. The upstream does not see the operator credential.
4. No configured route → **404** `NOT_FOUND`.

Upstream status and body are passed through. This gateway does not translate them.

Upstream dial failure → **503** `SERVICE_UNAVAILABLE`. That is this gateway's rejection. It is not a downstream status.

## Attribution

This gateway logs the operator id, `X-Request-ID`, and the target ids in the path (`user_id`, `organization_id`, `member_id` when the route has them). That log is who did it.

It generates `X-Request-ID` when the client did not send one, forwards it upstream, and returns it on the response. Services already log that id. The two logs join on it.

## Identity

This gateway dials identity. The route list is identity's public URLs. Who may call is this proxy. Identity refuses illegal states only: slug uniqueness, one membership row per user per org, last member, the invite machine, and an address that does not exist.

Any authenticated operator may name any target on a configured route.

## Errors

Plat5 envelope: `error.type`, `error.code`, `error.message`, `error.request_id`. Clients branch on `code` and HTTP status. `message` is safe to show.

| Case | HTTP / code |
|------|-------------|
| Bad or missing operator credential | **401** `UNAUTHORIZED` |
| No configured route | **404** `NOT_FOUND` |
| Upstream dial failure | **503** `SERVICE_UNAVAILABLE` |

## What this is not

- A dependency of the customer gateway, or a mode of it
- A second listener with a different service contract
- A place that stores customer users, orgs, or members
- The system that decides a member's permissions inside an org
