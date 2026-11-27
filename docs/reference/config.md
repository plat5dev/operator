# Config

This gateway's config file: `CONFIG_FILE` (default `operator.yml`, `/operator.yml` in the image). A missing or invalid file refuses boot. How the gateway uses it: [requests](../concepts/requests.md).

## Format

```yaml
# Optional: long-poll Plat5's control plane (GET /internal/gateway) for app services
# and upstream route templates.
control_plane:
  url: http://control-plane:5003

# Service registry. Explicit entries always exist and win over control-plane on name
# collision. `identity`, `audit`, and `operator-audit` automatically carry built-in
# route templates for node resolution and normalized audit routes.
services:
  identity:
    url: http://identity:3000
  audit:
    url: http://audit:3002
  operator-audit:
    url: http://operator-audit:5005
  # Optional: add operator-only node routes on a control-plane service (url may be omitted
  # when control_plane is configured), or declare an internal-only service:
  # api:
  #   routes:
  #     - path: /projects/{project_id}/force-rebuild
  #       node: project_id
  #       methods: [POST]
  # billing:
  #   url: http://billing:4000
  #   timeout: 15s

# Authorization (OpenID AuthZEN 1.0 PDP). Default mode is `authzen`.
authorizer:
  mode: authzen                       # authzen (default) | disabled
  url: http://topaz:9393              # required when mode is authzen
  timeout: 2s                         # default 2s
  token_env: OPERATOR_AUTHORIZER_TOKEN

# Optional dotted JWT claim paths from the operator IdP forwarded to AuthZEN as
# subject.properties.claims.
policy_claims: [groups, roles, amr]
```

Unknown fields refuse boot.

## Fields

### `control_plane` (optional)

| Field | Rule |
|-------|------|
| `url` | Base URL (`http://` or `https://`, host and port, no query or fragment) of Plat5's control plane internal port (e.g. `http://control-plane:5003`). Overridden by `CONTROL_PLANE_URL` env var when set |

When configured, the gateway long-polls `GET {url}/internal/gateway` (without `?replica=`, so it never registers as a customer gateway replica) in the background. It merges every service's `url` and expanded upstream routes into the active registry:

- A service's upstream route like `/projects/{project.id}/plans/{plan_id}/versions` with `scope: project` becomes operator template `/projects/{project_id}/plans/{plan_id}/versions` with `node: project_id` and its `plat5_permissions` attached.
- Explicit entries under `services:` stay reachable even before control plane's first apply or while control plane is down.

### `services`

Map of service name (`[a-z][a-z0-9-]*`, max 128 bytes) to service settings:

| Field | Rule |
|-------|------|
| `url` | `http://` or `https://`, host and port, no path/query/credentials. Required unless `control_plane.url` is configured (in which case `url` is inherited from control plane if present) |
| `timeout` | Optional positive duration overriding `UPSTREAM_TIMEOUT` for this service |
| `routes` | Optional list of extra route templates for node extraction and audit/policy normalization |

### Extra `routes` fields

| Field | Rule |
|-------|------|
| `path` | Template starting with `/`, literal segments and bare `{name}` params (`[a-z][a-z0-9_]*`). No dotted params |
| `methods` | List of verbs (`GET`, `HEAD`, `POST`, `PUT`, `PATCH`, `DELETE`) |
| `node` | Optional param name in `path` holding the Plat5 node id this route addresses |

No two routes in the same service's `routes` list may overlap on the same method. Route templates provide `route` normalization and `node` resolution—they are **not** an allowlist; unlisted paths on a known service forward unscoped (`route: null`, `nodes: []`) subject to AuthZEN.

### `authorizer`

| Field | Rule |
|-------|------|
| `mode` | `authzen` (default) or `disabled`. Overridden by `AUTHORIZER_MODE` env var when set |
| `url` | Base URL of an OpenID AuthZEN 1.0 PDP (`http://` or `https://`, optional path prefix without trailing slash). Required when `mode` is `authzen`. Overridden by `AUTHORIZER_URL` env var when set |
| `timeout` | Positive duration per AuthZEN request. Default `2s` |
| `token_env` | Optional environment variable name holding a bearer token sent as `Authorization: Bearer <value>` to the PDP. If named, the env var must be non-empty at boot |

### `policy_claims` (optional)

List of dotted JWT claim paths (each non-empty, up to 32) extracted from verified operator JWTs and forwarded to AuthZEN under `subject.properties.claims`.

## Built-in core templates

When `identity`, `audit`, or `operator-audit` are present in the service registry, the gateway automatically attaches built-in route templates for their documented service APIs (node chain params and normalized route templates), with lower priority than any explicit `routes` in `operator.yml` and control-plane routes.

## Identity

`IDENTITY_URL` (env var) or `services.identity.url` (in `operator.yml`) provides Plat5 identity's public base URL for node chain and member lookups. Required whenever any service or route uses node resolution (`IDENTITY_URL` takes precedence if both are set).

## Reloading

`CONFIG_FILE` can be reloaded without restarting via `POST /reload` on `INTERNAL_PORT` or `SIGHUP`. If invalid, the reload is refused (**422** on `POST /reload`) and the previous config stays active.
