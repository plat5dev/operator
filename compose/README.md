# Compose

## Dev

Gateway, Dex as the staff IdP, and an echo upstream that prints the request it received.

```bash
docker compose up --build
```

| URL | |
|-----|--|
| `http://localhost:5004` | Gateway API (`OPERATOR_PORT` to change the host port) |
| `http://localhost:8004/health/ready` | Health |
| `http://localhost:5556/dex` | Dex. User `staff@example.com`, password `password` |

Get a token and call a route:

```bash
TOKEN=$(curl -s -u operator-cli:operator-cli-secret \
  -d grant_type=password -d username=staff@example.com -d password=password \
  -d 'scope=openid email' http://localhost:5556/dex/token | jq -r .id_token)

curl -i -H "Authorization: Bearer $TOKEN" http://localhost:5004/organizations/org_1/members
```

The echo body shows what a service sees: the same path, a request id, no credential. The gateway log line is the attribution record.

`AUTH_ISSUER` is `http://localhost:5556/dex` because that is the `iss` Dex writes into tokens fetched from the host. `AUTH_JWKS_URI` is `http://dex:5556/dex/keys` because the gateway reaches Dex on the compose network.

## Prod

```bash
cp .env.template .env   # set OPERATOR_VERSION and AUTH_*
docker compose -f docker-compose.prod.yml --env-file .env up -d
```

No IdP and no upstream in this file. Point `AUTH_*` at the staff IdP and mount a route file with the deployment's upstream addresses. The API binds `127.0.0.1:5004`; put a tunnel or TLS proxy in front.
