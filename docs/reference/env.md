# Environment

Every variable each binary reads. Unset means the default; a blank value counts as unset.

## Gateway

`cmd/operator/`, image `ghcr.io/plat5dev/operator`.

| Variable | Default | |
|----------|---------|--|
| `ADDR` | `:5004` | API listener |
| `INTERNAL_PORT` | `8004` | `/health/live`, `/health/ready`, `/metrics`, `POST /reload` |
| `CONFIG_FILE` | `operator.yml` (`/operator.yml` in the image) | [`reference/config.md`](config.md) |
| `CONTROL_PLANE_URL` | | Optional override for `control_plane.url` in `CONFIG_FILE` |
| `IDENTITY_URL` | | Plat5 identity's public base URL, e.g. `http://identity:3000`. Defaults to `services.identity.url` in `CONFIG_FILE` when unset |
| `AUTHORIZER_MODE` | | Optional override for `authorizer.mode` (`authzen` or `disabled`) |
| `AUTHORIZER_URL` | | Optional override for `authorizer.url` |
| `AUTH_ISSUER`, `AUTH_JWKS_URI`, `AUTH_ALLOWED_AUDIENCES` | | Required. [`reference/idp.md`](idp.md) |
| `AUTH_OPERATOR_ID_CLAIM` | `sub` | [`reference/idp.md`](idp.md) |
| `AUTH_OPERATOR_EMAIL_CLAIM` | `email` | [`reference/idp.md`](idp.md) |
| `TRUSTED_PROXIES` | empty | Comma-separated IPs/CIDRs trusted for `X-Forwarded-For` ([client IP](gateway.md#client-ip)) |
| `ALLOWED_ORIGINS` | empty | Comma-separated `scheme://host[:port]`. No wildcard ([CORS](gateway.md#cors)) |
| `UPSTREAM_TIMEOUT` | `30s` | Default duration until an upstream's response headers. Exceeded → **502** |
| `AUDIT_ENABLED` | `true` | `false` turns audit off for the deployment. Any other value refuses boot ([audit](../concepts/audit.md#the-model)) |
| `AUDIT_URL` | | Required unless audit is off. operator-audit's internal base URL, e.g. `http://operator-audit:8005`. A forgotten URL never turns audit off silently |
| `UPSTREAM_SIGNING_KEY` | | Optional 32-byte Ed25519 seed encoded as base64url without padding. When set, every upstream request carries `X-Plat5-Signature` ([`reference/headers.md`](headers.md#to-services)); invalid refuses boot |
| `UPSTREAM_SIGNING_KEY_ID` | | Optional `kid` (`1–64` of `[A-Za-z0-9._-]`) for `X-Plat5-Signature`. Defaults to the first 16 hex chars of `SHA-256(public_key)`. Refuses boot if set without `UPSTREAM_SIGNING_KEY` |

## operator-audit

`cmd/operator-audit/`, image `ghcr.io/plat5dev/operator-audit`. Commands `migrate`, `serve`, and `prune` ([`internals/audit-storage.md`](../internals/audit-storage.md)).

| Variable | Default | |
|----------|---------|--|
| `ADDR` | `:5005` | Public listener: `GET /operator-audit-events` |
| `INTERNAL_PORT` | `8005` | `/health/*`, `/metrics`, `/internal/events` (writes and export feed) |
| `DATABASE_URL` | | Required. The owner role for `migrate` and `prune`, the writer for `serve` |
| `WRITER_ROLE` | | Required by `migrate`. The role it grants the writer's rights |
