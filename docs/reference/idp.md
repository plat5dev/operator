# Operator IdP

This gateway validates JWTs from one OIDC issuer: the operator IdP. Okta, Entra, Google Workspace, Keycloak, Plat5 Auth, or anything else that publishes a JWKS. Never the customer IdP.

How a caller gets a token is the IdP's business. People typically use authorization code with PKCE; automation typically uses client credentials. This gateway only checks the token.

## Config

| Variable | Required | |
|----------|----------|--|
| `AUTH_ISSUER` | yes | Must equal the token's `iss` exactly |
| `AUTH_JWKS_URI` | yes | Where this gateway fetches keys. Separate from the issuer so a container can reach JWKS on an internal address while `iss` stays public |
| `AUTH_AUDIENCES` | yes | Comma-separated. The token's `aud` must contain one. Empty refuses boot |
| `AUTH_OPERATOR_ID_CLAIM` | no | Dotted JSON path to the operator id. Default `sub` |
| `AUTH_OPERATOR_EMAIL_CLAIM` | no | Dotted JSON path to the operator's email, recorded in the operator audit log. Default `email`. Absent in a token, the operator has no email |

The audience is required, and there is no wildcard, unlike Plat5's gateway's `*`. An operator IdP issues tokens for many internal applications; without an audience check, a token for any of them would be an operator token.

## Token checks

All of these, or **401**:

- The signature verifies against a JWKS key matching the header `kid`
- `alg` is asymmetric (RS256, RS384, RS512, ES256, ES384, PS256). `none` and HMAC are rejected
- `iss` equals `AUTH_ISSUER`
- `aud` contains one of `AUTH_AUDIENCES`
- `exp` is present and in the future; `nbf`, when present, is in the past. 60 seconds of clock leeway
- The operator id claim is present and a non-empty string

The 401 message stays generic. `details.reason` says `expired`, `invalid` or `missing`. It never echoes the token.

## The actor

| Event field | From |
|-------------|------|
| `actor.issuer` | `iss` |
| `actor.operator_id` | `AUTH_OPERATOR_ID_CLAIM` |
| `actor.email` | `AUTH_OPERATOR_EMAIL_CLAIM`, or `null` |
| `actor.client_id` | `azp`, else `client_id`: the client the token was issued to, such as a console, a CLI or automation. `null` when neither is present |

## Keys

JWKS is fetched at boot and retried with backoff until it succeeds. Until then `/health/ready` is **503**, and a request carrying a token is **503**: the token can't be judged. Keys are cached and refreshed every 15 minutes. An unknown `kid` triggers a refetch, at most once every 30 seconds. A failed fetch keeps the cached keys.

## Local development

Compose runs [Plat5 Auth](https://github.com/plat5dev/auth) as the operator IdP: its own issuer and database, never the customer one. Dev mode seeds `operator@example.com` / `password` for the browser, and scripts mint a token with `POST /dev/token`. Both are local development only. Plat5 Auth puts its ids under `properties`, so compose sets `AUTH_OPERATOR_ID_CLAIM=properties.user_id` and `AUTH_OPERATOR_EMAIL_CLAIM=properties.email` (the issuer sets `AUTH_TOKEN_EMAIL=true`). See [`compose/README.md`](../../compose/README.md).

## Switching IdPs

Operator ids are whatever the claim holds, and switching IdPs changes them. Audit events keep the ids they were recorded with, beside the issuer that vouched for them. That's a deployment migration, not something this gateway maps.
