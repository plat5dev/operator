# Compose

## Dev

The gateway, Plat5 Auth as the staff IdP (`staff-auth`), an echo service that answers with the request it received, and the staff audit log: Postgres and operator-audit. `operator-audit-migrate` runs once as the schema owner; operator-audit serves as the writer role, which can't change or delete an event.

The gateway reaches Plat5's identity and audit on Plat5's compose network, so start Plat5 first:

```bash
# In the plat5 repo
docker compose -f compose/docker-compose.yml up -d --build

# Here
docker compose up --build
```

`PLAT5_NETWORK` names Plat5's network when it isn't `plat5_plat5` (Plat5's compose project `plat5`).

| URL | |
|-----|--|
| `http://localhost:5410` | Gateway API (`OPERATOR_PORT` to change the host port) |
| `http://localhost:8004/health/ready` | Health. `/metrics` beside it |
| `http://localhost:8005/metrics` | operator-audit metrics |
| `http://localhost:5411` | Staff IdP (Plat5 Auth). User `staff@example.com`, password `password` |

Get a token. Plat5 Auth's dev mode mints one without the browser flow:

```bash
TOKEN=$(curl -s -X POST http://localhost:5411/dev/token \
  -H 'Content-Type: application/json' \
  -d '{"email":"staff@example.com","client_id":"operator-console"}' | jq -r .access_token)
op() { curl -s -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' "$@"; }
```

Make an org, add a person, and act as them:

```bash
ORG=$(op -X POST http://localhost:5410/roots/organization -d '{"label":"Acme"}' | jq -r .id)
DANA=$(op -X POST http://localhost:5410/nodes/$ORG/members \
  -d '{"idp":{"issuer":"http://localhost:5401","subject":"dana"}}' | jq -r .id)

# Created as Dana: she gets creator_role on the project.
PROJECT=$(op -X POST http://localhost:5410/as/$DANA/nodes/$ORG/children/project -d '{"label":"Apps"}' | jq -r .id)
op http://localhost:5410/nodes/$PROJECT/bindings | jq

# What a service is sent: the chain, the member, the operator. No credential.
op http://localhost:5410/as/$DANA/nodes/$PROJECT/echo
```

Every authenticated call is in the staff audit log, including reads of the log. Read it for the org and everything below it:

```bash
op "http://localhost:5410/operator-audit-events?node_id=$ORG" | jq
```

Stop operator-audit (`docker compose stop operator-audit`) and every authenticated call is **503** until it's back.

`AUTH_ISSUER` is `http://localhost:5411` because staff-auth pins its `iss` there (`PUBLIC_ISSUER_URL`). `AUTH_JWKS_URI` is `http://staff-auth:5000/.well-known/jwks.json` because the gateway reaches it on the compose network. Plat5 Auth puts the user id at `properties.user_id` and, with `AUTH_TOKEN_EMAIL=true`, the email at `properties.email`, so the gateway sets `AUTH_OPERATOR_ID_CLAIM` and `AUTH_OPERATOR_EMAIL_CLAIM` to those. Its tokens' `aud` is the client id, `operator-console`.

The end-to-end tests run against this stack: [`e2e/`](../e2e/README.md).

## Prod

```bash
cp .env.template .env   # set OPERATOR_VERSION, IDENTITY_URL, AUTH_*, AUDIT_TOKEN, and the Postgres passwords
docker compose -f docker-compose.prod.yml --env-file .env up -d
```

No IdP and no services in this file. Point `AUTH_*` at the staff IdP, put the gateway on the network where the deployment's services and Plat5 identity are reachable, and mount a route file with their addresses ([`docs/reference/routes.md`](../docs/reference/routes.md)). The API binds `127.0.0.1:5004`; put a tunnel or TLS proxy in front.

The file runs the operator plane's Postgres with a named volume, and operator-audit as its writer role. To use a Postgres you already run, create an owner and a writer login role and the `audit` schema owned by the owner (as `postgres-init.sh` does), then point the two `DATABASE_URL`s at it. Back it up: it's the audit record.

Audit off: set `AUDIT_ENABLED=false`, drop the `operator-audit` service from the route file, and start only the gateway: `docker compose -f docker-compose.prod.yml --env-file .env up -d operator`.
