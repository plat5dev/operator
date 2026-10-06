# Routes

This gateway's own route list. `ROUTES_FILE` (default `routes.yml`). Missing or invalid file refuses boot. Upstream addresses are copied from the deployment, not discovered from the customer route map.

```yaml
routes:
  - path: /organizations
    methods: [GET]
    upstream: http://identity:3000
  - path: /organizations/{organization_id}/members
    methods: [GET, POST]
    upstream: http://identity:3000
```

| Field | |
|-------|--|
| `path` | Template. Literal segments and `{name}` parameters. A parameter matches exactly one non-empty segment |
| `methods` | HTTP methods. `OPTIONS` is not allowed; preflight is answered here |
| `upstream` | Scheme, host, and port. No path. The request path is appended unchanged |
| `action` | Optional. Authz action name. See [`authz.md`](authz.md) |
| `resource` | Optional. Authz resource. See [`authz.md`](authz.md) |

## Matching

A request matches a route when every segment matches and the method is listed. A method that is not listed is **404**, the same as a missing path.

Boot refuses a file where one request could match two routes. That includes a literal and a parameter in the same position, e.g. `/organizations/new` beside `/organizations/{organization_id}`. There is no precedence rule to learn.

Parameter names are unique within a path and match `[a-z][a-z0-9_]*`. They are the names in the attribution log and in authz requests.

## Reserved

`/health/live` and `/health/ready` are served on `INTERNAL_PORT`, not the API port. Nothing on the API port is reserved. Every path there is a route or a **404**.
