# Environment

Every variable each binary reads. Unset means the default; a blank value counts as unset.

## Gateway

`cmd/operator/`, image `ghcr.io/plat5dev/operator`.

| Variable | Default | |
|----------|---------|--|
| `ADDR` | `:5004` | API listener |
| `INTERNAL_PORT` | `8004` | `/health/live`, `/health/ready`, `/metrics` |
| `ROUTES_FILE` | `routes.yml` (`/routes.yml` in the image) | [`reference/routes.md`](routes.md) |
| `IDENTITY_URL` | | Plat5 identity's public base URL, e.g. `http://identity:3000`. Required when a route has `node` ([`reference/gateway.md`](gateway.md#identity)) |
| `AUTH_ISSUER`, `AUTH_JWKS_URI`, `AUTH_AUDIENCES` | | Required. [`reference/idp.md`](idp.md) |
| `AUTH_OPERATOR_ID_CLAIM` | `sub` | [`reference/idp.md`](idp.md) |
| `AUTH_OPERATOR_EMAIL_CLAIM` | `email` | [`reference/idp.md`](idp.md) |
| `ALLOWED_ORIGINS` | empty | Comma-separated `scheme://host[:port]`. No wildcard ([CORS](gateway.md#cors)) |
| `UPSTREAM_TIMEOUT_MS` | `30000` | Time to an upstream's response headers. Exceeded → **502** |
| `AUDIT_ENABLED` | `true` | `false` turns audit off for the deployment. Any other value refuses boot ([audit](../concepts/audit.md#the-model)) |
| `AUDIT_URL` | | Required unless audit is off. operator-audit's internal base URL, e.g. `http://operator-audit:8005`. A forgotten URL never turns audit off silently |
| `AUDIT_TOKEN` | | Required unless audit is off. Sent as `Authorization: Bearer`; operator-audit's `INTERNAL_TOKEN` |

## operator-audit

`cmd/operator-audit/`, image `ghcr.io/plat5dev/operator-audit`. Commands `migrate` and `serve` ([`internals/audit-storage.md`](../internals/audit-storage.md)).

| Variable | Default | |
|----------|---------|--|
| `ADDR` | `:5005` | Public listener: `GET /operator-audit-events` |
| `INTERNAL_PORT` | `8005` | `/health/*`, `/metrics`, `/internal/events` |
| `DATABASE_URL` | | Required. The owner role for `migrate`, the writer for `serve` |
| `INTERNAL_TOKEN` | | Required by `serve`. The bearer the gateway sends |
| `WRITER_ROLE` | | Required by `migrate`. The role it grants the writer's rights |
