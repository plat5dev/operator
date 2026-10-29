# End to end

Tests that drive the operator gateway against a running Plat5 stack: the tree by id, acting as a member, the chain sent to services, restore, and the operator audit log by node. Opt-in: they skip unless `OPERATOR_E2E=1`. Their own module, so the gateway's build never depends on them.

A fresh Plat5 stack has no config, and identity doesn't listen until it has one. The tests apply a config through Plat5's control plane first, as Plat5's own e2e does: a dry run, then the apply with the plan's revision. A rerun applies nothing.

The config is built when the tests run: Plat5's e2e config, `e2e/testdata/config.json` in the plat5 repo, with operator's additions, [`testdata/additions.json`](testdata/additions.json), merged in. The plat5 repo is `../plat5` beside this repo, or `PLAT5_DIR`. Without it the tests fail and name the path they looked for.

The merge: where both files have a key and both values are objects, they merge the same way, key by key. Otherwise the additions' value replaces Plat5's whole, arrays included: an array is never merged element by element. Keys in only one file are kept. The merge has unit tests here that run without a stack or `OPERATOR_E2E`.

The additions are only what these tests need beyond Plat5's config:

| Addition | Why |
|----------|-----|
| `kinds.project.bind_creator: true` | A project created acting as a member gives that member `creator_role` on it, which is how the tests see the member authored the write. Plat5's config turns it off for projects |

Plat5's e2e applies its own config, so either suite runs on a stack the other has used.

```bash
# In the plat5 repo (../plat5, or PLAT5_DIR): identity and audit, with what they need (Postgres, the control plane).
docker compose -f compose/docker-compose.yml up -d --build identity audit

# Here: the operator dev stack (compose/README.md), then the tests.
docker compose -f compose/docker-compose.yml up -d --build --wait
cd e2e && OPERATOR_E2E=1 go test ./... -count=1 -v
```

| Env | Default | |
|-----|---------|--|
| `OPERATOR_E2E` | unset | `1` runs the tests |
| `PLAT5_DIR` | `../plat5` | The plat5 repo, for its e2e config. Relative to this repo's root |
| `OPERATOR_URL` | `http://localhost:5410` | The gateway, as clients see it |
| `OPERATOR_AUTH_URL` | `http://localhost:5411` | Plat5 Auth in dev mode, for `POST /dev/token` |
| `CONTROL_PLANE_URL` | `http://localhost:5402` | Plat5's control plane admin API, for the apply |
| `ADMIN_TOKEN` | `dev-admin-token` | Its token |
| `E2E_WAIT` | `5m` | How long to wait for the control plane, then the gateway and identity, to answer (Plat5's dev images compile on first start) |

The tests make their own orgs, with a fresh suffix, so they can run again on the same stack.
