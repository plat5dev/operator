# Operator compose

Operator gateway + console. Own Docker network. Dials service addresses from `routes.yml`; it does not join Plat5's network by default.

## Quick start (dev)

```bash
cd compose
docker compose up --build
```

| URL | Service |
|-----|---------|
| `http://localhost:5004` | Console + `/api` gateway |

Sample routes in the image target `http://identity:3000`. Attach this project to a Plat5 network (or mount a route list with reachable upstreams) before calling `/api`.

## Prod

Pull the published image (`ghcr.io/plat5dev/operator:${OPERATOR_VERSION}`; tags from this repo’s `v*` releases):

```bash
cp .env.template .env   # set OPERATOR_VERSION=v0.1.0 and any bootstrap/routes
docker compose -f docker-compose.prod.yml --env-file .env up -d
```

Build the image from this checkout instead of pulling:

```bash
docker compose -f docker-compose.prod.yml -f docker-compose.prod.build.yml --env-file .env up --build -d
```

Upstream URLs are deployment config. Mount a route file over `/routes.yml` or set `ROUTES_FILE`. Do not publish `:5004` on the public internet; terminate TLS in front (the host overlay binds localhost).
