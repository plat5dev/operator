# Operator

Headless staff gateway for a Plat5 deployment. Staff authenticate with your IdP and call the same private service APIs the customer gateway fronts.

The customer gateway fills the subject into the path. This gateway checks a staff JWT, records the request in the staff audit log, then forwards the path the operator named. That path is the target. Who did it is the audit log, which `operator-audit` (also in this repo) keeps in the operator plane's Postgres.

Not part of the plat5 repo. Does not call the customer gateway. Serves no UI; a console is a separate client.

Contract: [`docs/`](docs/). Invariants: [`AGENTS.md`](AGENTS.md). Run it: [`compose/`](compose/).

```bash
cd compose
docker compose up --build
```

Gateway API on `:5004`, health and metrics on `:8004`. operator-audit on `:5005` (reached through the gateway) and `:8005` (health, metrics, the gateway's writes). The route list is `routes.yml` (identity's paths, the org audit log read, and the staff audit log read; mount a deployment copy in prod). Images: `ghcr.io/plat5dev/operator` and `ghcr.io/plat5dev/operator-audit` on `v*` tags.

## Layout

| Path | Purpose |
|------|---------|
| `docs/` | Contract. Read this before adding code |
| `cmd/operator/` | Gateway binary and config |
| `cmd/operator-audit/` | Audit service binary: `migrate` and `serve` |
| `internal/auth/` | Staff JWT validation |
| `internal/routes/` | Route file and matching |
| `internal/gateway/` | Request order, forwarding, request log |
| `internal/audit/` | The gateway's audit writes: intent before forward, outcome after |
| `internal/events/` | operator-audit's store, write API, and read route |
| `internal/db/` | operator-audit's Postgres: migrations, owner and writer roles |
| `internal/metrics/` | Prometheus text format for `/metrics` |
| `internal/apierr/` | Plat5 error envelope |
| `compose/` | Dev (with Dex and Postgres) and prod compose |

## Tests

Store and role tests need Postgres and skip without it. Each makes its own database and roles, then drops them.

```bash
OPERATOR_AUDIT_TEST_DATABASE_URL=postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable go test ./...
```

The URL must be a superuser.

## License

MIT — see [LICENSE](LICENSE).
