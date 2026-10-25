# End to end

Tests that drive the operator gateway against a running Plat5 stack: the tree by id, acting as a member, the chain sent to services, restore, and the operator audit log by node. Opt-in: they skip unless `OPERATOR_E2E=1`. Their own module, so the gateway's build never depends on them.

A fresh Plat5 stack has no config, and identity doesn't listen until it has one. The tests apply [`testdata/config.json`](testdata/config.json) through Plat5's control plane first, as Plat5's own e2e does: a dry run, then the apply with the plan's revision. It's the same config as Plat5's e2e (kinds organization → project, Plat5's example roles), so either suite runs on a stack the other has used, and a rerun applies nothing.

```bash
# In the plat5 repo: identity and audit, with what they need (Postgres, the control plane).
docker compose -f compose/docker-compose.yml up -d --build identity audit

# Here: the operator dev stack (compose/README.md), then the tests.
docker compose -f compose/docker-compose.yml up -d --build --wait
cd e2e && OPERATOR_E2E=1 go test ./... -count=1 -v
```

| Env | Default | |
|-----|---------|--|
| `OPERATOR_E2E` | unset | `1` runs the tests |
| `OPERATOR_URL` | `http://localhost:5410` | The gateway, as clients see it |
| `OPERATOR_AUTH_URL` | `http://localhost:5411` | Plat5 Auth in dev mode, for `POST /dev/token` |
| `CONTROL_PLANE_URL` | `http://localhost:5402` | Plat5's control plane admin API, for the apply |
| `ADMIN_TOKEN` | `dev-admin-token` | Its token |
| `E2E_WAIT` | `5m` | How long to wait for the control plane, then the gateway and identity, to answer (Plat5's dev images compile on first start) |

The tests make their own orgs, with a fresh suffix, so they can run again on the same stack.
