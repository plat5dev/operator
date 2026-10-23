# Plat5 v2 cutover

> Transitional. Delete once the operator plane runs against Plat5 v2.

Plat5 v2 replaces fixed realms with a tree of kinds from config, members with bindings on any node, and services that see node ids and the caller's member id (Plat5 `docs/concepts/model.md`). Services still do no auth, so this plane keeps calling them directly. What changes here:

| Area | Before | After |
|------|--------|-------|
| Targets | `/organizations/{organization_id}/…`, `/users/{user_id}/…`, `/members/{member_id}/…` | Node ids in the service's own paths: `/nodes/{node_id}/…`, `/projects/{project_id}/…` |
| Headers to services | None. "No actor header" | `X-Plat5-Principal: operator` and `X-Plat5-Operator-Id` on every request; `X-Plat5-Member-Id` when acting as a member |
| Authoring | Impossible: nothing supplied a caller | `/as/{member}` on routes with `as_member: true`, checked against identity |
| Incoming `X-Plat5-*` | Only `X-Plat5-Audit-Details` stripped | Every `X-Plat5-*` stripped |
| Audit event | Actor only | Actor, plus `acting_as` |
| Plat5 dependencies | Service addresses only | Also identity's public API for the member check (`IDENTITY_URL`) |
| Operator actions Plat5 v2 relies on | — | Tree browser (`GET /nodes`), restore a node, release a slug, suspend a person, recover an org with no admin (a binding by id), restore a removed kind (a control-plane apply, out of this gateway) |

Unchanged: the staff JWT as the only credential, no accounts or sessions, own route list, no proxying through the customer gateway, audit before forward, AuthZEN or nothing, no response filtering.

## Build order

One session per step, each starting from the docs named.

1. **Routes** ([`routes.md`](routes.md)). `as_member` and `node`, the reserved `/as/` prefix and `{member}` param, boot checks. Rewrite `routes.yml` and `compose/routes.yml` for Plat5 v2's identity and audit paths.
2. **Gateway** ([`model.md`](model.md)). Strip every incoming `X-Plat5-*`; send the operator headers; match `/as/{member}`, check the member against identity, strip the prefix, send `X-Plat5-Member-Id`. Request order step 6. `IDENTITY_URL`.
3. **Audit** ([`audit.md`](audit.md)). `acting_as` on the intent, stored and filterable; the 400 member-check event.
4. **End to end.** Against a local Plat5 v2 stack: browse the tree, restore a deleted project, publish a plan version as a member and see the member as its author.

Authz (slice 3) builds to the updated [`authz.md`](authz.md).
