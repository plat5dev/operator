# Requests

How this gateway decides a request: the credential, the order, the route node, acting as a member, and what is forwarded. The shapes are in [reference](../README.md#reference): [routes](../reference/routes.md), [headers](../reference/headers.md), [gateway](../reference/gateway.md).

## Credential

`Authorization: Bearer <jwt>` from the operator IdP, for a configured audience ([`reference/idp.md`](../reference/idp.md)). Nothing else: no cookies, no API keys.

## How a request is decided

Every request, in order:

1. **Preflight.** An `OPTIONS` preflight from an allowed origin is answered here. Never forwarded, never audited.
2. **Authenticate.** A missing or invalid token → **401**. Not audited.
3. **Match.** Check the path and match a route. Nothing is answered yet.
4. **Resolve.** On a route with a node: the node's chain from identity, and when acting as a member, the member in the chain's root. Nothing is answered yet.
5. **Audit intent**, when audit is on → **503** if it can't be written ([`concepts/audit.md`](audit.md#delivery)).
6. **Answer what can't be forwarded.** A path this gateway could read differently from the service → **400**. No route for the method and path → **404**. The route node not found → **404**. The member not active in the node's tree → **400**. Identity unreachable → **503**.
7. **Forward**, with the headers below.
8. **Outcome**, in the background, when audit is on.

Authentication comes first: an unauthenticated caller learns nothing about the route list. With audit on, every request past authentication is an event, including a **400** or a **404**, and its intent is written before anything answers it.

There is no authorization step. Any authenticated operator may call any route ([`TODO.md`](../../TODO.md)).

## The route node

A route that addresses a Plat5 node names the param holding it (`node`, [`reference/routes.md`](../reference/routes.md#route-fields)). For such a route this gateway asks identity for the node and each ancestor, root first: the chain.

- **Sent as Plat5 sends it.** The service gets `X-Plat5-<Kind>-Id` for each node in the chain ([`reference/headers.md`](../reference/headers.md)). A service written for Plat5's gateway reads node ids from the path or those headers; through this gateway, both are there.
- **Recorded.** The chain is the audit event's `nodes`, so the operator log can be read for a node and everything below it.
- **Not found** → **404** `NOT_FOUND`. Deleted and purged nodes are found: their chain is sent like any other, and the service applies its own state rules. Restoring a deleted node goes through here.
- **Nothing else is checked.** The node's status, kind and the operator's reach are not this gateway's to judge.

A route without `node` is forwarded with no chain, as a Plat5 user route is: `GET /nodes`, `/users/{user_id}/…`, `/members/{member_id}/user`, `/roots/{kind}`.

## Acting as a member

A service that records authors needs `X-Plat5-Member-Id` (Plat5 `docs/guides/write-a-service.md`). An operator supplies one by acting as a member, and only that way: the path says so.

```
POST /as/01jbyb0d…/nodes/01jbx9q3…/children/project
→ POST /nodes/01jbx9q3…/children/project
  X-Plat5-Organization-Id: 01jbx9q3…
  X-Plat5-Member-Id: 01jbyb0d…
  X-Plat5-Principal: person
  X-Plat5-Operator-Id: 00u1abc
```

- **Only on routes that allow it.** A verb with `as_member: true`, its own or its route's, is also served at `/as/{member}` followed by the route's path. This gateway strips the prefix and forwards the rest exactly. Without `/as/`, the same route is forwarded with no member.
- **The member must be active in the node's tree.** Identity's member lookup in the chain's root must answer `status: active`. Anything else is **400**: a removed member, a member of another tree, an id that isn't a member. Suspension is per person in Plat5 and isn't checked here, as identity doesn't check it on a member id it is sent.
- **The service is told the member and its principal.** `X-Plat5-Member-Id`, and `X-Plat5-Principal` as identity names the member's principal: `person` or `service_account`.
- **Access is the operator's, never the member's.** The member's bindings and labels are never read or applied. A filtered list asked as a member shows what that member can read.
- **Both are recorded.** The operator audit log has the operator as the actor and the member as `acting_as` ([`concepts/audit.md`](audit.md)).

Without `/as/`, a request carries no member id and no principal. A service that needs one refuses it as malformed; that's correct, and the operator retries as a member if the change should be authored. Identity's filtered lists are the full list without one, as `GET /nodes` is.

## Forwarding

The path is forwarded exactly as received, after the `/as/{member}` prefix is stripped on a route that allows it. The query string and the body are forwarded unchanged; the body isn't read.

Because the matched path is the path the service sees, this gateway refuses paths it could read differently from the service: dot segments (`.`, `..`), empty segments (`//`), and an encoded `/` or `\` in a segment. Those are **400**.

Before forwarding, this gateway strips `Authorization`, `Cookie`, `X-API-Key` and every incoming `X-Plat5-*` header, then sets its own. A client can't name a node, a member or an operator, and the service never sees an operator's credential.

Upstream status and body pass through untranslated. An upstream that can't be reached, times out, or fails mid-response → **502**.

## Request id and tracing

This gateway generates a request id for every request. A client's `X-Request-ID` is not accepted; the generated id replaces it. The id is sent upstream and to identity, returned on every response, and is the audit event's key. `traceparent` passes through.

## Lists

A service never filters a response by operator, and this gateway never filters a response body. Which customers an operator may see is a decision service's question, per request, from the path, once there is one ([`TODO.md`](../../TODO.md)).

## Errors

The Plat5 envelope (Plat5 `docs/reference/errors.md`): clients branch on `code` and HTTP status, and `message` is safe to show. Every case: [`reference/gateway.md`](../reference/gateway.md#errors).
