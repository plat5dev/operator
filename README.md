# Operator

Second front door for a Plat5 deployment. Operators sign in here and call the same private service APIs the customer gateway fronts.

The customer gateway fills the subject into the path. This gateway authenticates an operator, then forwards the path the operator named. That path is the target. Who performed the action is this plane's record, not something the service branches on.

Not part of the plat5 repo. Does not call the customer gateway. Services stay off the public internet; this process is a front door of its own.

Contract: [`docs/`](docs/). Invariants: [`AGENTS.md`](AGENTS.md). Run it: [`compose/`](compose/).

```bash
cd compose
docker compose up --build
```

Listens on `:5004`. The route list is `routes.yml` (mount a deployment copy in prod). Image: `ghcr.io/plat5dev/operator` on `v*` tags.

The console is served by this process. The image builds it. For a local binary, build `console/` (`npm ci && npm run build`) and run from the repo root so `console/dist` is found.

## License

MIT — see [LICENSE](LICENSE).

## Layout

| Path | Purpose |
|------|---------|
| `docs/` | Contract. Read this before adding code. |
| `accounts/` | Operator directory. |
| `gateway/` | Operator front door. |
| `console/` | Web UI. |
| `compose/` | Dev and prod compose. |

Those first three are the first slice ([`docs/v1.md`](docs/v1.md)).

## Docs

| Doc | Contents |
|-----|----------|
| [`docs/model.md`](docs/model.md) | Planes, the path as the target, attribution |
| [`docs/v1.md`](docs/v1.md) | First slice, the hole it accepts, what waits |
