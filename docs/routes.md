# Routes

This gateway's own route list. `ROUTES_FILE` (default `routes.yml`). A missing or invalid file refuses boot. Upstream addresses and paths are copied from the deployment's services, by id: this gateway never reads the customer route map and never resolves slugs.

```yaml
upstreams:
  identity:
    url: http://identity:3000
    routes:
      - path: /nodes
        methods: [GET]
      - path: /nodes/{node_id}
        methods: [GET, PATCH, DELETE]
      - path: /nodes/{node_id}/restore
        methods: [POST]
      - path: /nodes/{node_id}/release-slug
        methods: [POST]
      - path: /nodes/{node_id}/bindings
        methods: [GET, POST]
      - path: /members/{member_id}/status
        methods: [PATCH]
  e10s:
    url: http://e10s:3000
    routes:
      - path: /projects/{project_id}/plans/{key}/versions
        methods: [GET, POST]
        as_member: true
        node: project_id
```

An upstream is one service address and the routes forwarded to it.

| Upstream field | |
|----------------|--|
| key | Name. `[a-z][a-z0-9-]*`. Unique. Used in logs, and as an audit event's `upstream` |
| `url` | Scheme, host, and port. No path. The request path is appended unchanged |
| `routes` | At least one |

| Route field | |
|-------------|--|
| `path` | Template. Literal segments and `{name}` parameters. A parameter matches exactly one non-empty segment. The service's own path |
| `methods` | HTTP methods. `OPTIONS` isn't allowed; preflight is answered here |
| `as_member` | Optional, default `false`. `true` also serves the route at `/as/{member}` + `path`, which sends `X-Plat5-Member-Id` ([`model.md`](model.md#acting-as-a-member)). The plain path stays served, without a member |
| `node` | Required with `as_member`. The path param holding the node id whose tree the member must be in |
| `action` | Optional. Authz action name. See [`authz.md`](authz.md) |
| `resource` | Optional. Authz resource. See [`authz.md`](authz.md) |

## Matching

A request matches a route when every segment matches and the method is listed. A method that isn't listed is **404**, the same as a missing path.

A path starting with `/as/{member}/` matches only routes with `as_member: true`, against the rest of the path. `as` is reserved as a first segment: a route whose path starts with `/as/` refuses boot.

Boot refuses a file where one request could match two routes, in the same upstream or different ones. That includes a literal and a parameter in the same position, e.g. `/nodes/search` beside `/nodes/{node_id}`. There is no precedence rule to learn.

Parameter names are unique within a path and match `[a-z][a-z0-9_]*`. `member` is reserved for the `/as/` prefix. They are the names in audit events, the request log, and authz requests.

Boot also refuses `as_member` without `node`, or a `node` that isn't a param of the path.

## Identity

Acting as a member needs Plat5 identity: `IDENTITY_URL` (identity's public base URL, e.g. `http://identity:3000`) is required when any route has `as_member: true`. This gateway calls `GET /nodes/{node_id}` for the root, then `GET /nodes/{root_id}/members/{member}` for the member's status, on every such request. Both are Plat5 identity's public service API.

## Reserved

`/health/live`, `/health/ready`, and `/metrics` are served on `INTERNAL_PORT`, not the API port. On the API port, `/as/` is the only reserved prefix. Every other path is a route or a **404**.
