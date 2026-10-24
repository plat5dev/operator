# Operator docs

`docs/` is law. Concepts hold the rules, reference holds the shapes. When two disagree, the concept doc wins, and [`concepts/model.md`](concepts/model.md) wins over the rest. Plat5's docs are law for Plat5's side of every contract here: its service APIs, headers and error envelope.

## Start here, by what you're doing

| You're | Read |
|--------|------|
| Learning the operator plane | [model](concepts/model.md), then [requests](concepts/requests.md) |
| Running it | [env](reference/env.md), [routes](reference/routes.md), [`compose/`](../compose/README.md) |
| Building a console or a staff tool | [requests](concepts/requests.md), [operator-audit API](reference/api/operator-audit.md) |
| Writing a service staff call | Plat5's write-a-service guide, then [headers](reference/headers.md) |

## Concepts

| Doc | What it decides |
|-----|-----------------|
| [model](concepts/model.md) | Operators, routes, the chain, acting as a member; the principles |
| [requests](concepts/requests.md) | The credential, how a request is decided, the route node, acting as a member, forwarding |
| [audit](concepts/audit.md) | What the staff log records, delivery, what a service adds |
| [boundaries](concepts/boundaries.md) | What this plane is for and won't do |

## Reference

| Doc | |
|-----|--|
| [routes](reference/routes.md) | The route file |
| [headers](reference/headers.md) | Every header set, stripped or read |
| [gateway](reference/gateway.md) | Identity calls and cache, CORS, security headers, errors, request log, metrics |
| [idp](reference/idp.md) | The staff JWT |
| [env](reference/env.md) | Both binaries' environment |
| [api/operator-audit](reference/api/operator-audit.md) | Staff audit events, the read API, the gateway's writes |

## Internals

| Doc | |
|-----|--|
| [audit storage](internals/audit-storage.md) | Tables, roles, runtime |

## Decisions

- [Decisions](decisions/README.md): why, by topic, with references.
- [`TODO.md`](../TODO.md): what's next, and deferred work.
