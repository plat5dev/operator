# Operator

Headless gateway for a Plat5 deployment's operators. Operators authenticate with your operator IdP and call private service APIs at `/services/{service}/{upstream_path…}`, by node id.

Plat5's gateway resolves slugs and sends services the caller's member id. This gateway checks an operator JWT, resolves any route node's chain in Plat5 identity, records the request in the operator audit log, evaluates live OpenID AuthZEN 1.0 authorization (or runs with `authorizer.mode: disabled`), strips `/services/{service}`, and forwards the service's own path with the chain and `X-Plat5-Operator-Id`. An operator can act as a member (`X-Plat5-As-Member: <member_id>`) on node-scoped routes and attach a ticket/incident reference (`X-Plat5-Operator-Reason`). The record of who did what (and why) is the operator audit log, which `operator-audit` (also in this repo) keeps in the operator plane's Postgres.

Plat5 never depends on the operator plane, and this repo imports no Plat5 code. It never calls Plat5's customer gateway, and optionally reads Plat5's control-plane feed (`GET /internal/gateway`) for automatic app service and route template discovery. Serves no UI; a console is a separate client.

Contract: [`docs/`](docs/README.md). How to work here: [`AGENTS.md`](AGENTS.md). Run it: [`compose/`](compose/README.md).

In a Plat5 project, `plat5 start` runs it locally as the CLI's operator module: this gateway at `:5410` on `operator.yml`, operator-audit, an operator IdP at `:5411`, and the operator console at `:5412`, on Plat5's network. On its own:

```bash
# With Plat5's compose running (compose/README.md)
cd compose
docker compose up --build
```

Gateway API on `:5004` (`:5410` in dev compose), health and metrics on `:8004`. operator-audit on `:5005` (reached at `/services/operator-audit/operator-audit-events`) and `:8005` (health, metrics, the gateway's writes). Config is `operator.yml`: core Plat5 services (`identity`, `audit`, `operator-audit`), optional `control_plane` discovery, `authorizer`, and `policy_claims`. Images: `ghcr.io/plat5dev/operator` and `ghcr.io/plat5dev/operator-audit` on `v*` tags.

## Layout

| Path | Purpose |
|------|---------|
| `docs/` | Contract. Read this before adding code |
| `cmd/operator/` | Gateway binary and config |
| `cmd/operator-audit/` | Audit service binary: `migrate` and `serve` |
| `internal/auth/` | Operator JWT validation and `policy_claims` extraction |
| `internal/authorizer/` | OpenID AuthZEN 1.0 client (single and batch evaluation) |
| `internal/routes/` | `operator.yml` config, control-plane feed poller, built-in core templates, `/services/{service}` registry |
| `internal/identity/` | Plat5 identity client: node chains and member checks |
| `internal/gateway/` | Request order, `/access/routes`, `TRUSTED_PROXIES`, forwarding, signing, request log |
| `internal/audit/` | The gateway's audit writes: intent before forward, outcome after |
| `internal/events/` | operator-audit's store, write API, and read route |
| `internal/db/` | operator-audit's Postgres: migrations, owner and writer roles |
| `internal/metrics/` | Prometheus text format for `/metrics` |
| `internal/httpx/` | Shared HTTP transport, request ids, status/recorder helpers |
| `internal/apierr/` | Plat5 error envelope |
| `compose/` | Dev and prod compose |
| `e2e/` | End-to-end tests against a running stack, opt-in |

## Tests

Store and role tests need Postgres and skip without it. Each makes its own database and roles, then drops them.

```bash
OPERATOR_AUDIT_TEST_DATABASE_URL=postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable go test ./...
```

The URL must be a superuser. End to end, against Plat5 and the dev stack: [`e2e/`](e2e/README.md).

## License

MIT — see [LICENSE](LICENSE).
