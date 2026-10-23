# Authorization

**Slice 3.** Not built yet. This is the contract it will build to.

This gateway is the enforcement point. It does not decide. It asks a decision service over [OpenID AuthZEN](https://openid.net/wg/authzen/) (Authorization API 1.0) and enforces the answer. Any AuthZEN decision service works. RBAC, ABAC, or relationships are that service's choice, not this gateway's.

## Off or on

| `AUTHZ_URL` | Behaviour |
|-------------|-----------|
| unset | Every authenticated operator may call every route. Boot logs `authz: none` at warn level |
| set | Every request that reaches step 7 of the request order ([`model.md`](model.md#request-order)) is evaluated |

There is no built-in policy and no fallback policy. Off means off.

## Config

| Variable | |
|----------|--|
| `AUTHZ_URL` | Decision service base URL. This gateway calls `POST {AUTHZ_URL}/access/v1/evaluation` |
| `AUTHZ_TOKEN` | Optional. Sent as `Authorization: Bearer` to the decision service |
| `AUTHZ_TIMEOUT_MS` | Default `1000` |
| `AUTHZ_SUBJECT_CLAIMS` | Comma-separated token claims copied into `subject.properties`, e.g. `email,groups` |

The staff token itself is never sent to the decision service.

## Request

```json
{
  "subject":  { "type": "operator", "id": "00u1abc", "properties": { "email": "a@x.com", "groups": ["support"] } },
  "action":   { "name": "POST /projects/{project_id}/plans/{key}/versions" },
  "resource": { "type": "project", "id": "n_01J…",
                "properties": { "project_id": "n_01J…", "key": "pro", "root_id": "n_01J…" } },
  "context":  { "request_id": "…", "member_id": "m_01J…" }
}
```

| Field | Default | Route override |
|-------|---------|----------------|
| `action.name` | Method, a space, and the route template. Acting as a member, the template is prefixed `/as/{member}` | `action: plans.versions.publish` |
| `resource.type` | `route` | `resource: { type: project, id: project_id }` |
| `resource.id` | The route template | The named path parameter's value |
| `resource.properties` | Every path parameter by name, plus `root_id` on a route acting as a member | — |
| `context.member_id` | The member acted as, on `/as/{member}`. Absent otherwise | — |

Defaults mean a route needs no authz config to be evaluated. Overrides give policies stable names that survive a path change.

## Response

| Decision service answers | This gateway |
|--------------------------|--------------|
| `200` with `"decision": true` | Forwards |
| `200` with `"decision": false` | **403** `FORBIDDEN` |
| Anything else: non-200, timeout, unreachable, no boolean `decision` | **503** `SERVICE_UNAVAILABLE`. Fail closed |

The response `context` (reasons) is logged with the decision. It is not returned to the client.

With audit on, a denial or a decision-service failure is an audit event with outcome `rejected` ([`audit.md`](audit.md)). This slice defines the event's `decision` field.

Decisions are not cached. Revoking access takes effect on the next request.

`/health/ready` does not probe the decision service. A decision service outage is 503 per request, not a restart loop.

## Lists

Same as AWS: a list is its own action, all or nothing.

- A route whose path names a node (`/nodes/{node_id}/bindings`) is decided per node. A policy that scopes operators to some orgs needs the node's root, which the route's `resource` names or the policy looks up.
- A route that names none (`GET /nodes`) returns everything. Grant it only to operators who may see every customer.
- Operators scoped to some customers reach them by id, e.g. from a support ticket.

No service filters by operator. This gateway does not filter bodies.

## What is not evaluated

Only the path, method, and token claims. Ids in the query string or the body are not seen. A route whose body names a second customer is authorized on its path alone. Keep that in mind when choosing which routes to list.

## Default decision service

Slice 3 ships an example deployment of an AuthZEN decision service with a groups-to-roles policy. Which one is chosen then. It is an example to copy, not a dependency.
