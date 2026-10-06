# Staff IdP contract

This gateway validates JWTs from one OIDC issuer: the staff IdP. Okta, Entra, Google Workspace, Keycloak, Dex, or anything else that publishes a JWKS. Never the customer IdP.

How a caller gets a token is the IdP's business. People typically use authorization code with PKCE. Automation typically uses client credentials. This gateway only checks the token.

## Config

| Variable | Required | |
|----------|----------|--|
| `AUTH_ISSUER` | yes | Must equal the token's `iss` exactly |
| `AUTH_JWKS_URI` | yes | Where this gateway fetches keys. Separate from the issuer so a container can reach JWKS on an internal address while `iss` stays public |
| `AUTH_AUDIENCES` | yes | Comma-separated. The token's `aud` must contain one. Empty refuses boot |
| `AUTH_OPERATOR_ID_CLAIM` | no | Dotted JSON path to the operator id. Default `sub` |

Audience is required, unlike the customer gateway. A staff IdP issues tokens for many internal applications. Without an audience check, a token for any of them would be an operator token.

## Token checks

All of these, or **401**:

- Signature verifies against a JWKS key matching the header `kid`
- `alg` is an asymmetric algorithm (RS256, RS384, RS512, ES256, ES384, PS256). `none` and HMAC are rejected
- `iss` equals `AUTH_ISSUER`
- `aud` contains one of `AUTH_AUDIENCES`
- `exp` is present and in the future; `nbf`, when present, is in the past. 60 seconds of clock leeway
- The operator id claim is present and a non-empty string

The `401` message stays generic. `details.reason` may say `expired`, `invalid`, or `missing`. It never echoes the token.

## Keys

JWKS is fetched at boot, retried with backoff until it succeeds. Until then `/health/ready` is **503** and a request carrying a token is **503** `SERVICE_UNAVAILABLE`: the token cannot be judged. Keys are cached and refreshed every 15 minutes. An unknown `kid` triggers a refetch, at most once every 30 seconds. A failed fetch keeps the cached keys.

## Local development

Compose runs Dex as the staff IdP with a static user and a static client (`compose/dex.yml`). Scripts get a token from Dex with the password grant against its local connector. That grant is for local development only. See [`compose/README.md`](../compose/README.md).

## Switching IdPs

Operator ids are whatever the claim holds. Switching IdPs changes them, which changes the attribution log and any authz data keyed by operator id. That is a deployment migration, not something this gateway maps.
