# Operator: agent contract

`docs/` is law. If code and a contract disagree, fix the one that's wrong. Don't invent a third story in chat.

How I decide: `my-principles` skill. This file is how to work here, not what the operator plane is; it restates no rules, so it can't drift from them.

## What this is

A headless gateway for staff: a second front door onto the private APIs a Plat5 deployment already fronts. Staff JWT in, audit event written, path out. This repo also holds `operator-audit`, the service that stores the staff audit log.

## Where the rules live

| What | Where |
|------|-------|
| The model and its rules | [`docs/model.md`](docs/model.md#rules) |
| What this plane won't do (stop conditions) | [`docs/model.md#not-in-the-operator-plane`](docs/model.md#not-in-the-operator-plane), and each doc's "Not here" |
| Every wire contract | [`docs/README.md`](docs/README.md) |
| What's next, and deferred work and when it's ready | [`TODO.md`](TODO.md) |

## Siblings

Plat5 is the customer runtime (gateway, control plane, identity, audit). Auth is the customer IdP. This repo imports neither. It dials service addresses and uses Plat5's public, documented service APIs, the same way it reads `X-Plat5-Audit-Details` as Plat5 defines it. A console is a separate repo and a client of this API.

## Working here

1. Read `docs/model.md`, then the docs for the area you're touching, before editing.
2. Don't implement past the contract. If the work needs a contract change, stop and propose the doc change first.
3. A change that touches a rule or an item under "Not in the operator plane" needs explicit sign-off. Say which one, and why, before writing code.
4. A rule belongs in exactly one doc. Link to it; don't restate it elsewhere, including here.
5. Don't commit unless asked.
