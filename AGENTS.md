# Operator: agent contract

`docs/` is law. If code and a contract disagree, fix the one that's wrong, usually the code. Don't invent a third story in chat.

How I decide: `my-principles` skill. This file is how to work here, not what the operator plane is; it restates no rules, so it can't drift from them.

## What this is

A headless gateway for staff: a second front door onto the services a Plat5 deployment runs. Staff JWT in, audit event written, path out. This repo also holds `operator-audit`, the service that stores the staff audit log.

## Where the rules live

| What | Where |
|------|-------|
| The model and its principles | [`docs/concepts/model.md`](docs/concepts/model.md) |
| The rules, one concept per doc | [`docs/concepts/`](docs/concepts/): requests, audit |
| What this plane won't do (stop conditions) | [`docs/concepts/boundaries.md`](docs/concepts/boundaries.md#not-in-the-operator-plane) |
| Shapes: route file, headers, APIs, env | [`docs/reference/`](docs/reference/) |
| How the staff log is stored | [`docs/internals/`](docs/internals/) |
| Why each decision was made, by topic | [`docs/decisions/`](docs/decisions/README.md) |
| The map of all of it | [`docs/README.md`](docs/README.md) |
| What's next, and deferred work and when it's ready | [`TODO.md`](TODO.md) |

## Siblings

Plat5 is the customer runtime (gateway, control plane, identity, audit), and its docs are law for its side of every contract here. This repo imports no Plat5 code: it dials service addresses and uses Plat5's public, documented service APIs and headers as Plat5 defines them. Auth is an IdP, used here as the local staff IdP. A console is a separate repo and a client of this API.

## Working here

1. Read `docs/concepts/model.md`, then the concept and reference docs for the area you're touching, before editing.
2. Don't implement past the contract. If the work needs a contract change, stop and propose the doc change first.
3. A change that touches a principle in `concepts/model.md` or an item under "Not in the operator plane" in `concepts/boundaries.md` needs explicit sign-off. Say which one, and why, before writing code.
4. A rule belongs in exactly one concept doc, a shape in exactly one reference doc. Don't restate anything here.
5. Docs describe how things are now: no slices, versions or changelogs. Reasons go into `docs/decisions/`, upcoming work into `TODO.md`.
6. Don't commit unless asked.
