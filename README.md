# Operator

Headless gateway for a Plat5 deployment's operators. Operators authenticate with your operator IdP and call the same private service APIs Plat5's gateway fronts, by node id.

Plat5's gateway resolves slugs and sends services the caller's member id. This gateway checks an operator JWT, looks up the route node's chain in Plat5 identity, records the request in the operator audit log, then forwards the service's own path with the chain and `X-Plat5-Operator-Id`. An operator can act as a member (`/as/{member}/…`) on routes that allow it, so what they write is authored by a real member of the tree. The record of who did what is the operator audit log, which `operator-audit` (also in this repo) keeps in the operator plane's Postgres.

Not part of the plat5 repo, and imports none of its code. Doesn't call Plat5's gateway. Serves no UI; a console is a separate client.

Contract: [`docs/`](docs/README.md). How to work here: [`AGENTS.md`](AGENTS.md). Next and deferred: [`TODO.md`](TODO.md). Run it: [`compose/`](compose/README.md).

```bash
# With Plat5's compose running (compose/README.md)
cd compose
docker compose up --build
```

Gateway API on `:5004` (`:5410` in dev compose), health and metrics on `:8004`. operator-audit on `:5005` (reached through the gateway) and `:8005` (health, metrics, the gateway's writes). The route list is `routes.yml`: Plat5 identity's service API, Plat5's audit reads and the operator log read, at Plat5's compose addresses; mount a deployment copy in prod. Images: `ghcr.io/plat5dev/operator` and `ghcr.io/plat5dev/operator-audit` on `v*` tags.

## Layout

| Path | Purpose |
|------|---------|
| `docs/` | Contract. Read this before adding code |
| `cmd/operator/` | Gateway binary and config |
| `cmd/operator-audit/` | Audit service binary: `migrate` and `serve` |
| `internal/auth/` | Operator JWT validation |
| `internal/routes/` | Route file and matching, `/as/{member}` included |
| `internal/identity/` | Plat5 identity client: node chains and member checks |
| `internal/gateway/` | Request order, forwarding, request log |
| `internal/audit/` | The gateway's audit writes: intent before forward, outcome after |
| `internal/events/` | operator-audit's store, write API, and read route |
| `internal/db/` | operator-audit's Postgres: migrations, owner and writer roles |
| `internal/metrics/` | Prometheus text format for `/metrics` |
| `internal/apierr/` | Plat5 error envelope |
| `compose/` | Dev (with Plat5 Auth as the operator IdP, Postgres, an echo service, on Plat5's network) and prod compose |
| `e2e/` | End-to-end tests against a running stack, opt-in |

## Tests

Store and role tests need Postgres and skip without it. Each makes its own database and roles, then drops them.

```bash
OPERATOR_AUDIT_TEST_DATABASE_URL=postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable go test ./...
```

The URL must be a superuser. End to end, against Plat5 and the dev stack: [`e2e/`](e2e/README.md).

## License

MIT — see [LICENSE](LICENSE).
