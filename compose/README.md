# Compose

## Dev

Gateway, Dex as the staff IdP, an echo upstream that prints the request it received, and the staff audit log: Postgres and operator-audit. `operator-audit-migrate` runs once as the schema owner; operator-audit serves as the writer role, which cannot change or delete an event.

```bash
docker compose up --build
```

| URL | |
|-----|--|
| `http://localhost:5004` | Gateway API (`OPERATOR_PORT` to change the host port) |
| `http://localhost:8004/health/ready` | Health. `/metrics` beside it |
| `http://localhost:8005/metrics` | operator-audit metrics |
| `http://localhost:5556/dex` | Dex. User `staff@example.com`, password `password` |

Get a token and call a route:

```bash
TOKEN=$(curl -s -u operator-cli:operator-cli-secret \
  -d grant_type=password -d username=staff@example.com -d password=password \
  -d 'scope=openid email' http://localhost:5556/dex/token | jq -r .id_token)

curl -i -H "Authorization: Bearer $TOKEN" http://localhost:5004/organizations/org_1/members
```

The echo body shows what a service sees: the same path, a request id, no credential.

Every authenticated call is in the staff audit log, including reads of the log:

```bash
curl -s -H "Authorization: Bearer $TOKEN" 'http://localhost:5004/operator-audit-events?limit=5' | jq
```

Stop operator-audit (`docker compose stop operator-audit`) and every authenticated call is **503** until it is back.

`AUTH_ISSUER` is `http://localhost:5556/dex` because that is the `iss` Dex writes into tokens fetched from the host. `AUTH_JWKS_URI` is `http://dex:5556/dex/keys` because the gateway reaches Dex on the compose network.

## Prod

```bash
cp .env.template .env   # set OPERATOR_VERSION, AUTH_*, AUDIT_TOKEN, and the Postgres passwords
docker compose -f docker-compose.prod.yml --env-file .env up -d
```

No IdP and no upstream in this file. Point `AUTH_*` at the staff IdP and mount a route file with the deployment's upstream addresses. The API binds `127.0.0.1:5004`; put a tunnel or TLS proxy in front.

The file runs the operator plane's Postgres with a named volume, and operator-audit as its writer role. To use a Postgres you already run, create an owner and a writer login role and the `audit` schema owned by the owner (as `postgres-init.sh` does), then point the two `DATABASE_URL`s at it. Back it up: it is the audit record.

Audit off: set `AUDIT_ENABLED=false`, drop the `operator-audit` upstream from the route file, and start only the gateway: `docker compose -f docker-compose.prod.yml --env-file .env up -d operator`.
