# Front door

Why the operator plane is its own gateway, why it addresses nodes by id with its own route list, and why it tells services what Plat5's gateway would. The rules: [`concepts/model.md`](../concepts/model.md), [`concepts/requests.md`](../concepts/requests.md), [`reference/routes.md`](../reference/routes.md), [`reference/headers.md`](../reference/headers.md).

## A second front door

| Decision | Why |
|----------|-----|
| Operators reach services through their own gateway, never through Plat5's | Services do no auth, so any front door can call them (Plat5 principle 6). Plat5 keeps operator headers, roles and rules out of its own code (Plat5 `concepts/boundaries.md`), so an operator path through its gateway would need exactly those |
| This gateway serves no UI and stores no accounts or sessions | A console is a client like a script or a CLI. Operator identity is the operator IdP's ([operator identity](operator-identity.md)) |
| Its own route list, copied from each service's API, not read from Plat5's control plane | Plat5's routes describe the customer edge: slugs, scopes and permissions. Operators call the services' own paths. A shared source is deferred until a hand copy causes a real mismatch ([`TODO.md`](../../TODO.md)) |
| A route's `methods` is a list or a map of verb to settings, as Plat5's, with `as_member` the one setting | One shape for routes in both front doors' files. A path's verbs can differ: a write authored as a member beside a read that needs none |
| Params are bare; nothing in the path is filled in | This gateway forwards the service's own path. A dotted placeholder is a value Plat5's gateway resolves, and nothing here resolves one |
| Node ids in the path, never slugs | Identity's API addresses everything by id so any front door can call it without slugs (Plat5 `decisions/tree-and-kinds.md`). An operator works from ids in tickets, logs and audit events |
| Authentication before route matching; **404** only to an authenticated caller | Plat5's gateway answers **404** first because its edge routes are public knowledge. This route list names internal service paths, which a stranger shouldn't learn |
| Paths this gateway could read differently from the service are **400** | The matched path is the path the service sees, so the match must mean what the service will read |

## What services are told

| Decision | Why |
|----------|-----|
| A route names its node (`node`); this gateway sends the node's chain as `X-Plat5-<Kind>-Id`, as Plat5's gateway does | A service may read node ids from the path or those headers, and may document the headers as required (Plat5 `guides/write-a-service.md`, `concepts/access.md#other-front-doors`). A front door that leaves them out breaks a correct service |
| The chain comes from identity's `GET /nodes/{node_id}`, walking `parent_id` | It's identity's public API for other front doors. Nothing else is needed: a node holds its kind and parent |
| Chains are cached for the life of the process; member lookups aren't | A node keeps its parent for life and ids are never reused (Plat5 `concepts/model.md`), so a chain can't go stale. Membership can change at any moment |
| A route node that isn't found is **404**; deleted and purged nodes are found | Restoring a deleted node is operator work, and a purged node's tombstone keeps its place. The service applies its own state rules, as it would for Plat5's gateway |
| `X-Plat5-Principal` is sent only with a member, with the member's own principal; there is no `operator` value | Plat5 defines the header's values as `person` and `service_account`, both describing the caller. A third value would be an operator rule in Plat5's header. Whether an operator acted is `X-Plat5-Operator-Id`, this plane's own header, which Plat5 names as the example (Plat5 `reference/headers.md`) |
| Every incoming `X-Plat5-*` header is stripped before this gateway sets its own | A client can't name a node, a member or an operator. Plat5's gateway strips them for the same reason |
| Upstream failure is **502**, identity or audit failure **503** | Plat5's gateway answers an upstream failure **502** with the `SERVICE_UNAVAILABLE` code, so a console handles both front doors' errors alike. **503** stays for this gateway's own dependencies |
| Security headers on every API response | Plat5's gateway sets the same three, and a console may render error bodies |

## Acting as a member

Plat5 sends services the caller's member id because "authorship needs the caller, and operators need to act as someone" (Plat5 `decisions/access.md#what-services-see`). Kubernetes lets an authorized user act as another user (`kubectl --as`); the request runs as the impersonated user and the audit log records both ([user impersonation](https://kubernetes.io/docs/reference/access-authn-authz/authentication/#user-impersonation)). Acting as a member is that, with the access check left with the operator.

| Decision | Why |
|----------|-----|
| A member id reaches a service only through `/as/{member}`, on a verb with `as_member: true` | Authored records must name a real member of the tree (Plat5 `concepts/model.md#where-a-field-goes`). Putting it in the path makes it visible in the route, the log and the audit event, never implied by a header a client could forge |
| The member must be active in the route node's tree, checked against identity on every request | Plat5's closed world: a tree's data names only its own members. Identity refuses a member from another tree with **400** too; checking here keeps the refusal at the front door for services that don't check |
| The member's bindings are never applied | Access is the front door's decision, and here it's the operator's. Narrowing operators belongs to a decision service, not to whichever member they pick |
| Suspension isn't checked | Plat5 suspends a person, not a member, and identity accepts a suspended person's member id. Operators may need to act for a suspended person, for example to clean up |
| Filtered lists asked as a member show what that member can read | Identity filters by the member it's sent. "What does this customer see?" is a support question |
