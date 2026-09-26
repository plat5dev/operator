# Operator — agent contract

`docs/` is law. If code and a contract disagree, fix the one that is wrong. Do not invent a third story in chat.

How I decide: `my-principles` skill. This file is **what Operator is**. Do not copy philosophy here.

## What this is

A second front door onto the private APIs a Plat5 deployment already fronts. Operator accounts, a gateway, a console.

Not the customer gateway. Not an IdP. Not Plat5 identity. Not a module host.

## Locked

Read the doc, don’t re-derive:

| Invariant | Where |
|-----------|--------|
| Operator is the actor. The path is the target, not the actor | [`docs/model.md`](docs/model.md) |
| Operator id is never written into the path | model |
| Client names the target in the path. Gateway authenticates, strips the operator credential, forwards the path | model |
| Own route list of identity URLs. No customer gateway, no route-registry | model |
| Attribution is this gateway's log: operator id, request id, target ids from the path | model |
| Any authenticated operator may name any target on any configured route | [`docs/v1.md`](docs/v1.md) |
| Who may call identity is this proxy. Identity refuses illegal states only | model |
| Errors use the Plat5 envelope | model |

## Stop conditions

Do not add these because they would be convenient:

- Operator id, role, or an actor header on the upstream request
- Proxying through the customer gateway, or publishing routes into its route map
- Plat5 Auth, or a customer `user_id`, as an operator account
- RBAC, per-target grants, or a policy engine in the first slice
- A module host, remote UI loader, or customer-API admin packaged in this repo
- Changes to plat5 or Auth in the first slice
- Minting customer credentials, or writing another product's database
- Wrapping the customer route-registry admin token

## Deferred

| Work | Ready when |
|------|------------|
| RBAC | An operator must be unable to name some target or call some route. Until then every authenticated operator is allowed. |
| Module host | A customer page must appear from deploy config without a change to this repo. Until then the console serves only its own pages. |
| Shared route source with the customer route-registry | Hand-copied upstream URLs cause a real mismatch. Until then this gateway owns its list. |
| Customer login provisioning | Operators must create customer logins. No mechanism is reserved here. |

## Siblings

Plat5 is the customer runtime (gateway, identity, route-registry). Auth is the customer IdP. This repo does not import either. It dials service addresses.

Do not edit those repos for the first slice.

## Working here

1. Read `docs/` before editing.
2. Do not implement past the signed-off contract.
3. Don’t commit unless asked.
