# Compose

In a Plat5 project, `plat5 start` runs operator for you: its operator module runs these images on the project's Postgres and Plat5's network, with an operator IdP and the operator console ([cli: what `plat5 start` runs](https://github.com/plat5dev/cli#what-plat5-start-runs)). The files here run operator on its own: dev beside Plat5's compose, and prod.

## Dev

The gateway, Plat5 Auth as the operator IdP (`operator-auth`), an echo service that answers with the request it received, and the operator audit log: Postgres and operator-audit. `operator-audit-migrate` runs once as the schema owner; operator-audit serves as the writer role, which can't change or delete an event.

The gateway reaches Plat5's identity and audit on Plat5's compose network, so start Plat5 first. Identity listens once Plat5 has a config applied (Plat5 `compose/README.md`); the [e2e](../e2e/README.md) applies one with kinds organization → project and Plat5's example roles, which the examples below use.

```bash
# In the plat5 repo
docker compose -f compose/docker-compose.yml up -d --build identity audit

# Here
docker compose up --build
```

`PLAT5_NETWORK` names Plat5's network when it isn't `plat5_plat5` (Plat5's compose project `plat5`).

| URL | |
|-----|--|
| `http://localhost:5410` | Gateway API (`OPERATOR_PORT` to change the host port) |
| `http://localhost:8004/health/ready` | Health. `/metrics` beside it |
| `http://localhost:8005/metrics` | operator-audit metrics |
| `http://localhost:5411` | Operator IdP (Plat5 Auth). User `operator@example.com`, password `password` |

Get a token. Plat5 Auth's dev mode mints one without the browser flow:

```bash
TOKEN=$(curl -s -X POST http://localhost:5411/dev/token \
  -H 'Content-Type: application/json' \
  -d '{"email":"operator@example.com","client_id":"operator-console"}' | jq -r .access_token)
op() { curl -s -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' "$@"; }
```

Make an org, add a person, and act as them:

```bash
ORG=$(op -X POST http://localhost:5410/roots/organization -d '{"display_name":"Acme"}' | jq -r .id)
DANA=$(op -X POST http://localhost:5410/nodes/$ORG/members \
  -d '{"idp":{"issuer":"http://localhost:5401","subject":"dana"},"role":"member"}' | jq -r .id)

# Created as Dana: she gets creator_role on the project.
PROJECT=$(op -X POST http://localhost:5410/as/$DANA/nodes/$ORG/children/project -d '{"display_name":"Apps"}' | jq -r .id)
op "http://localhost:5410/nodes/$PROJECT/bindings?inherited=false" | jq

# The person behind the member.
op http://localhost:5410/members/$DANA/user | jq

# What a service is sent: the chain, the member, the operator. No credential.
op http://localhost:5410/as/$DANA/nodes/$PROJECT/echo
```

Every authenticated call is in the operator audit log, including reads of the log. Read it for the org and everything below it:

```bash
op "http://localhost:5410/operator-audit-events?node_id=$ORG" | jq
```

Stop operator-audit (`docker compose stop operator-audit`) and every authenticated call is **503** until it's back.

`AUTH_ISSUER` is `http://localhost:5411` because operator-auth pins its `iss` there (`PUBLIC_ISSUER_URL`). `AUTH_JWKS_URI` is `http://operator-auth:5000/.well-known/jwks.json` because the gateway reaches it on the compose network. Plat5 Auth puts the user id at `properties.user_id` and, with `AUTH_TOKEN_EMAIL=true`, the email at `properties.email`, so the gateway sets `AUTH_OPERATOR_ID_CLAIM` and `AUTH_OPERATOR_EMAIL_CLAIM` to those. Its tokens' `aud` is the client id, `operator-console`.

The end-to-end tests run against this stack: [`e2e/`](../e2e/README.md).

## Prod

```bash
cp .env.template .env   # set OPERATOR_VERSION, IDENTITY_URL, AUTH_*, and the Postgres passwords
docker compose -f docker-compose.prod.yml --env-file .env up -d
```

No IdP and no services in this file. Point `AUTH_*` at the operator IdP, put the gateway on the network where the deployment's services and Plat5 identity are reachable, and mount a route file with their addresses ([`docs/reference/routes.md`](../docs/reference/routes.md)). The API binds `127.0.0.1:5004`; put a tunnel or TLS proxy in front.

The file runs the operator plane's Postgres with a named volume, and operator-audit as its writer role. To use a Postgres you already run, create an owner and a writer login role and the `audit` schema owned by the owner (as `postgres-init.sh` does), then point the two `DATABASE_URL`s at it. Back it up: it's the audit record.

Audit off: set `AUDIT_ENABLED=false`, drop the `operator-audit` service from the route file, and start only the gateway: `docker compose -f docker-compose.prod.yml --env-file .env up -d operator`.
