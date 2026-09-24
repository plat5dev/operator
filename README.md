# Operator

Second front door for a Plat5 deployment. Operators sign in here and call the same private service APIs the customer gateway fronts.

The customer gateway derives `X-User-Id` or `X-Organization-Id` / `X-Member-Id` from the caller's credential. This gateway authenticates an operator, then injects those headers as the **target** the action applies to. Who performed the action is this plane's record, not a header the service branches on.

Not part of the plat5 repo. Does not call the customer gateway. Services stay off the public internet; this process is a front door of its own.

Contract: [`docs/`](docs/). Invariants: [`AGENTS.md`](AGENTS.md).

```bash
docker compose -f compose/docker-compose.yml --env-file compose/.env up -d --build
```

Listens on `127.0.0.1:5004`. The route list is `routes.yml`.

## License

MIT — see [LICENSE](LICENSE).

## Layout

| Path | Purpose |
|------|---------|
| `docs/` | Contract. Read this before adding code. |
| `accounts/` | Operator directory. |
| `gateway/` | Operator front door. |
| `console/` | Web UI. |

Those three are the first slice ([`docs/v1.md`](docs/v1.md)).

## Docs

| Doc | Contents |
|-----|----------|
| [`docs/model.md`](docs/model.md) | Planes, target headers, attribution |
| [`docs/v1.md`](docs/v1.md) | First slice, the hole it accepts, what waits |
