# Model

> The operator plane's concepts and principles. When another doc disagrees with this page, the other doc is wrong.

The operator plane is a second front door onto the services a Plat5 deployment runs. Services do no auth, so any front door on the private network can call them (Plat5 `docs/concepts/model.md`, principle 6). This one is for operators: it checks a token from the operator IdP, resolves any Plat5 node chain on the route, records the request in the operator audit log, asks an OpenID AuthZEN 1.0 authorizer whether the operator may call it, and forwards the service's own path, by node id.

```
operator token  →  operator gateway  →  /services/{service}/{path}  →  service
customer cred   →  Plat5 gateway     →  /orgs/{slug}/…              →  same service, same API
```

Neither gateway calls the other. This gateway serves no UI, stores no accounts, and stores nothing about customers.

## Concepts

| Concept | What it is |
|---|---|
| **Operator** | A person or automation running the deployment, holding a token from the operator IdP. Its id is a claim of that token, unique within the issuer ([`reference/idp.md`](../reference/idp.md)) |
| **Operator IdP** | The one JWT issuer this gateway trusts. Never the customer IdP |
| **Service** | A named upstream reached at `/services/{service}/{path…}`, declared in `operator.yml` and/or discovered read-only from Plat5's control plane ([`reference/config.md`](../reference/config.md)) |
| **Route template** | An optional metadata template for a service's path (built-in for `identity`, `audit`, `operator-audit`; discovered from Plat5's control plane for app services; or declared in `operator.yml` for operator-only node routes). Templates normalize `route` in audit/policy and identify the route's `node` param; they are not an allowlist |
| **Route node** | The path param holding the Plat5 node a matched route template addresses (`node`) |
| **Chain** | The route node and its ancestors, root first, as Plat5 identity has them. The service gets it as `X-Plat5-<Kind>-Id` headers, as from Plat5's gateway |
| **Acting as a member** | `X-Plat5-As-Member: <member_id>` on a node-scoped request. The service is told that member's id, so what the request writes is authored by a real member of the tree ([requests](requests.md#acting-as-a-member)) |
| **Reason** | Optional `X-Plat5-Operator-Reason` header (e.g. a ticket or incident id), recorded in the operator audit log and passed to the authorizer |
| **Authorizer** | An external OpenID AuthZEN 1.0 PDP (such as Topaz or Cerbos) deciding every request live, with no cache, failing closed. May be set to `mode: disabled` explicitly for local development or external enforcement |
| **Operator audit log** | One event per authenticated request, in the operator plane's own Postgres ([`concepts/audit.md`](audit.md)). Not a Plat5 log |
| **Console** | A client of this API, in its own repo |

## Actor and target

| | Operator plane | Plat5 gateway |
|--|----------------|---------------|
| Who is calling | An operator | A person or service account |
| How the target is named | `/services/{service}/{upstream_path…}`, by node id | Slugs or ids in the edge path, rewritten to the service's path |
| Who may call | Decided live by the AuthZEN authorizer (or any authenticated operator when `authorizer.mode: disabled`) | The ancestor rule over the caller's bindings, or customer AuthZEN PDP |
| What the service is told | The upstream path; the chain, when the route names a node; `X-Plat5-Operator-Id`; the member and its principal only when acting as one | The rewritten path, the chain, the member and its principal; on a user route, the user id |
| Where it's recorded | The operator audit log | The tree's audit log |

The operator is the actor. The service and path are the target. The service implements the action against the path. It may record who acted, from the headers, but it never decides access from them: access is the front door's.

The operator id is not a customer member, not a principal, and not a customer IdP subject. It's never written into the path.

## Principles

1. **The operator is the actor; `/services/{service}/{path}` is the target.** Node ids in the path, never slugs. The operator id is never in the path.
2. **One-way dependency onto Plat5.** Plat5 knows nothing about the operator plane. This gateway never calls Plat5's customer gateway; it reads Plat5's control-plane feed optionally for service/route discovery and calls Plat5 identity's documented service API for node chains and member checks.
3. **Services are told what Plat5's gateway would tell them, and no more.** The route node's chain; a member id and its principal only when acting as a member; and the operator in this plane's own header, `X-Plat5-Operator-Id`.
4. **A member id only by saying so.** `X-Plat5-As-Member`, on a node-scoped request, for a member active in the route node's tree. Access is still the operator's (decided by the authorizer), never the member's.
5. **Every authenticated request is recorded before it's answered.** Audit is on or off for the deployment, and on by default.
6. **The operator IdP is the only source of who is an operator.** No accounts, passwords, or sessions here.
7. **Authorization lives in an external AuthZEN PDP.** No role, group, or allow-list logic is hardcoded in this gateway. Every request is evaluated live (no cache) when `authorizer.mode: authzen`, failing closed.

## Where each decision lives

| Question | Decided by |
|---|---|
| Who is this operator? | The operator IdP, from the token |
| Which service and upstream path? | `/services/{service}/{path…}`, resolved against `operator.yml` and optional control-plane discovery |
| Which node? | The matched route template's `node` param; Plat5 identity, for its chain |
| May this operator call this route, act as a member, or omit a reason? | The external OpenID AuthZEN 1.0 authorizer (or allowed when `authorizer.mode: disabled`) |
| Who authored a write? | The member acted as, when the request sends `X-Plat5-As-Member`; otherwise nobody |
| Who did it, and why? | The operator audit log (`actor`, `acting_as`, `reason`) |
| Is it valid for the resource's current state? | The service |

## Next

| To understand | Read |
|---|---|
| How a request is decided, routing, acting as a member, authorization, forwarding | [`concepts/requests.md`](requests.md) |
| What the operator audit log records | [`concepts/audit.md`](audit.md) |
| What this plane won't do | [`concepts/boundaries.md`](boundaries.md) |
