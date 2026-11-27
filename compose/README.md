# Compose

In a Plat5 project, `plat5 start` runs operator for you: its operator module runs these images on the project's Postgres and Plat5's network, with an operator IdP and the operator console. The files here run operator on its own: dev beside Plat5's compose, and prod.

## Dev

The gateway, Plat5 Auth as the operator IdP (`operator-auth`), an echo service that answers with the request it received, and the operator audit log: Postgres and operator-audit. `operator-audit-migrate` runs once as the schema owner; operator-audit serves as the writer role, which can't change or delete an event.

The gateway reaches Plat5's control plane, identity, and audit on Plat5's compose network, so start Plat5 first.

```bash
# In the plat5 repo
docker compose -f compose/docker-compose.yml up -d --build control-plane identity audit

# Here
docker compose up --build
```

`PLAT5_NETWORK` names Plat5's network when it isn't `plat5_plat5`.

| URL | |
|-----|--|
| `http://localhost:5410` | Gateway API (`OPERATOR_PORT` to change the host port) |
| `http://localhost:8004/health/ready` | Health. `/metrics` beside it |
| `http://localhost:8005/metrics` | operator-audit metrics |
| `http://localhost:5411` | Operator IdP (Plat5 Auth). User `operator@example.com`, password `password` |

Get a token:

```bash
TOKEN=$(curl -s -X POST http://localhost:5411/dev/token \
  -H 'Content-Type: application/json' \
  -d '{"email":"operator@example.com","client_id":"operator-console"}' | jq -r .access_token)
op() { curl -s -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' "$@"; }
```

Make an org, add a person, and act as them (every service is called under `/services/{service}/{path}`):

```bash
ORG=$(op -X POST http://localhost:5410/services/identity/roots/organization -d '{"display_name":"Acme"}' | jq -r .id)
DANA=$(op -X POST http://localhost:5410/services/identity/nodes/$ORG/members \
  -d '{"idp":{"issuer":"http://localhost:5401","subject":"dana"},"role":"member"}' | jq -r .id)

# Created as Dana: she gets creator_role on the project.
PROJECT=$(op -X POST -H "X-Plat5-As-Member: $DANA" -H "X-Plat5-Operator-Reason: DEV-1" \
  http://localhost:5410/services/identity/nodes/$ORG/children/project -d '{"display_name":"Apps"}' | jq -r .id)
op "http://localhost:5410/services/identity/nodes/$PROJECT/bindings?inherited=false" | jq

# What a service is sent: the chain, the member, the operator. No credential or reason header.
op -H "X-Plat5-As-Member: $DANA" http://localhost:5410/services/echo/nodes/$PROJECT/echo

# Read the operator audit log for the org and everything below it:
op "http://localhost:5410/services/operator-audit/operator-audit-events?node_id=$ORG" | jq
```

The end-to-end tests run against this stack: [`e2e/`](../e2e/README.md).

## Prod

```bash
cp .env.template .env   # set OPERATOR_VERSION, AUTH_*, AUTHORIZER_URL (or AUTHORIZER_MODE=disabled), and the Postgres passwords
docker compose -f docker-compose.prod.yml --env-file .env up -d
```

Mount your deployment's `operator.yml` (`CONFIG_FILE`) with your services, optional `control_plane.url`, and `authorizer` settings ([`docs/reference/config.md`](../docs/reference/config.md)).
