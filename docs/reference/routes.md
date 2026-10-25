# Routes

This gateway's route list: `ROUTES_FILE` (default `routes.yml`). A missing or invalid file refuses boot. How the gateway uses it: [requests](../concepts/requests.md).

Each route is a service's own path, by id, copied from that service's API. This gateway never reads Plat5's routes files or control plane and never resolves slugs.

## Format

```yaml
services:
  identity:
    url: http://identity:3000
    routes:
      - path: /nodes
        methods: [GET]
      - path: /nodes/{node_id}
        node: node_id
        methods: [GET, PATCH, DELETE]
      - path: /nodes/{node_id}/children/{kind}
        node: node_id
        as_member: true
        methods: [GET, POST]
      - path: /users/{user_id}/status
        methods: [PATCH]
  e10s:
    url: http://e10s:3000
    routes:
      # A verb's settings override the route's: versions are authored, reads aren't.
      - path: /projects/{project_id}/plans/{plan_id}/versions
        node: project_id
        methods:
          GET:
          POST: { as_member: true }
```

### Service fields

| Field | Rule |
|-------|------|
| service name | The key under `services`. `[a-z][a-z0-9-]*`, unique. In the request log and as an audit event's `service` |
| `url` | `http://` or `https://`, host and port. No path, query or credentials. The request path is appended unchanged |
| `routes` | At least one |

### Route fields

| Field | Rule |
|-------|------|
| `path` | Template. Literal segments and `{name}` params; a param matches exactly one non-empty segment. The service's own path |
| `methods` | A list of verbs, or a map of verb to `null` or its [settings](#method-fields), as Plat5's `methods` (Plat5 `docs/reference/config/services.md`). A list is a map with no overrides. `GET`, `HEAD`, `POST`, `PUT`, `PATCH`, `DELETE`, each once. `OPTIONS` isn't allowed: preflight is answered here |
| `node` | Optional. The param holding the Plat5 node id this route addresses. Its chain is looked up in identity, sent as headers and recorded ([the route node](../concepts/requests.md#the-route-node)) |
| `as_member` | Optional, default `false`. `true` also serves the route's verbs at `/as/{member}` + `path` ([acting as a member](../concepts/requests.md#acting-as-a-member)). The plain path stays served, without a member |

### Method fields

| Field | Rule |
|-------|------|
| `as_member` | Optional. Overrides the route's for this verb |

A verb served at `/as/{member}` needs the route's `node`: the member is checked in the node's tree. Unknown fields, in a route or a verb's settings, refuse boot.

## Matching

A request matches a route when every segment matches and the method is listed. A method that isn't listed is **404**, the same as a missing path.

A path starting with `/as/{member}/` matches only a route whose verb has `as_member: true`, against the rest of the path. `as` is reserved as a first segment: a route whose path starts with `/as/` refuses boot.

Boot refuses a file where one request could match two routes, in the same service or different ones. That includes a literal and a param in the same position, e.g. `/nodes/search` beside `/nodes/{node_id}`. There is no precedence rule to learn.

Param names are unique within a path and match `[a-z][a-z0-9_]*`. `member` is reserved for the `/as/` prefix. They are the names in audit events and the request log. Params are bare (`{node_id}`), never dotted (`{project.id}`): in Plat5 a dot is a value its gateway fills in, and this gateway forwards the path unchanged and fills nothing.

## Identity

`IDENTITY_URL` (identity's public base URL, e.g. `http://identity:3000`) is required when any route has `node`. The calls this gateway makes there: [`reference/gateway.md`](gateway.md#identity).

## The shipped file

The image's `/routes.yml` is this repo's [`routes.yml`](../../routes.yml): Plat5 identity's service API for other front doors, Plat5's audit reads by node and by person, and the operator audit log read, at the addresses of Plat5's compose. A deployment mounts its own copy with its addresses and its services' routes.

Left out on purpose: creating keys (`POST …/keys`), which mints customer credentials. Identity's other routes are in.

## Reserved

`/health/live`, `/health/ready` and `/metrics` are served on `INTERNAL_PORT`, not the API port. On the API port, `/as/` is the only reserved prefix. Every other path is a route or a **404**.
