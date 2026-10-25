# Model

> The operator plane's concepts and principles. When another doc disagrees with this page, the other doc is wrong. Why each decision was made: [decisions](../decisions/README.md), by topic.

The operator plane is a second front door onto the services a Plat5 deployment runs. Services do no auth, so any front door on the private network can call them (Plat5 `docs/concepts/model.md`, principle 6). This one is for operators: it checks a token from the operator IdP, records the request in the operator audit log, and forwards the service's own path, by node id.

```
operator token  →  operator gateway  →  service
customer cred   →  Plat5 gateway     →  same service, same API
```

Neither gateway calls the other. This gateway serves no UI, stores no accounts, and stores nothing about customers.

## Concepts

| Concept | What it is |
|---|---|
| **Operator** | A person or automation running the deployment, holding a token from the operator IdP. Its id is a claim of that token, unique within the issuer ([`reference/idp.md`](../reference/idp.md)) |
| **Operator IdP** | The one OIDC issuer this gateway trusts. Never the customer IdP |
| **Route** | A service path this gateway forwards, from its own route list ([`reference/routes.md`](../reference/routes.md)) |
| **Route node** | The path param holding the Plat5 node a route addresses (`node`) |
| **Chain** | The route node and its ancestors, root first, as Plat5 identity has them. The service gets it as `X-Plat5-<Kind>-Id` headers, as from Plat5's gateway |
| **Acting as a member** | `/as/{member}` before a route's path. The service is told that member's id, so what the request writes is authored by a real member of the tree ([requests](requests.md#acting-as-a-member)) |
| **Operator audit log** | One event per authenticated request, in the operator plane's own Postgres ([`concepts/audit.md`](audit.md)). Not a Plat5 log |
| **Console** | A client of this API, in its own repo |

## Actor and target

| | Operator plane | Plat5 gateway |
|--|----------------|---------------|
| Who is calling | An operator | A person or service account |
| How the target is named | Node ids in the path: the service's own path | Slugs or ids in the edge path, rewritten to the service's path |
| Who may call | Any authenticated operator, today ([`TODO.md`](../../TODO.md)) | The ancestor rule over the caller's bindings |
| What the service is told | The path; the chain, when the route names a node; `X-Plat5-Operator-Id`; the member and its principal only when acting as one | The rewritten path, the chain, the member and its principal |
| Where it's recorded | The operator audit log | The tree's audit log |

The operator is the actor. The path is the target. The service implements the action against the path. It may record who acted, from the headers, but it never decides access from them: access is the front door's.

The operator id is not a customer member, not a principal, and not a customer IdP subject. It's never written into the path.

## Principles

1. **The operator is the actor; the path is the target.** Node ids in the path, never slugs. The operator id is never in the path.
2. **A second front door, not a mode of Plat5.** This gateway dials services with its own route list. It doesn't call Plat5's gateway or control plane, and it uses Plat5 only through its public, documented service APIs.
3. **Services are told what Plat5's gateway would tell them, and no more.** The route node's chain; a member id and its principal only when acting as a member; and the operator in this plane's own header, `X-Plat5-Operator-Id`.
4. **A member id only by saying so.** `/as/{member}`, on a route that allows it, for a member active in the route node's tree. Access is still the operator's, never the member's.
5. **Every authenticated request is recorded before it's answered.** Audit is on or off for the deployment, and on by default.
6. **The operator IdP is the only source of who is an operator.** No accounts, passwords, or sessions here.
7. **Narrowing who may call is a decision service's job.** Any authenticated operator may call any route today. No role, group, or allow-list check goes into this gateway ([`TODO.md`](../../TODO.md)).

## Where each decision lives

| Question | Decided by |
|---|---|
| Who is this operator? | The operator IdP, from the token |
| Which node? | The path; Plat5 identity, for its chain |
| May this operator call this route? | Any authenticated operator may, today |
| Who authored a write? | The member acted as, when the path says `/as/{member}`; otherwise nobody |
| Who did it? | The operator audit log |
| Is it valid for the resource's current state? | The service |

## Next

| To understand | Read |
|---|---|
| How a request is decided, acting as a member, forwarding | [`concepts/requests.md`](requests.md) |
| What the operator audit log records | [`concepts/audit.md`](audit.md) |
| What this plane won't do | [`concepts/boundaries.md`](boundaries.md) |
