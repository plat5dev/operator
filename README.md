# Operator

Headless staff gateway for a Plat5 deployment. Staff authenticate with your IdP and call the same private service APIs the customer gateway fronts.

The customer gateway fills the subject into the path. This gateway checks a staff JWT, then forwards the path the operator named. That path is the target. Who did it is this gateway's log.

Not part of the plat5 repo. Does not call the customer gateway. Serves no UI; a console is a separate client.

Contract: [`docs/`](docs/). Invariants: [`AGENTS.md`](AGENTS.md). Run it: [`compose/`](compose/).

```bash
cd compose
docker compose up --build
```

API on `:5004`, health on `:8004`. The route list is `routes.yml` (identity's paths; mount a deployment copy in prod). Image: `ghcr.io/plat5dev/operator` on `v*` tags.

## Layout

| Path | Purpose |
|------|---------|
| `docs/` | Contract. Read this before adding code |
| `cmd/operator/` | Binary and config |
| `internal/auth/` | Staff JWT validation |
| `internal/routes/` | Route file and matching |
| `internal/gateway/` | Request order, forwarding, attribution log |
| `internal/apierr/` | Plat5 error envelope |
| `compose/` | Dev (with Dex) and prod compose |

## License

MIT — see [LICENSE](LICENSE).
