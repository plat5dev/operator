# Operator docs

`docs/` is law. Concepts hold the rules, reference holds the shapes. When two disagree, the concept doc wins, and [`concepts/model.md`](concepts/model.md) wins over the rest. Plat5's docs are law for Plat5's side of every contract here: its service APIs, headers and error envelope.

## Start here, by what you're doing

| You're | Read |
|--------|------|
| Learning the operator plane | [model](concepts/model.md), then [requests](concepts/requests.md) |
| Running it | [config](reference/config.md), [env](reference/env.md), [`compose/`](../compose/README.md) |
| Building a console or an operator tool | [requests](concepts/requests.md), [gateway](reference/gateway.md#route-checks), [operator-audit API](reference/api/operator-audit.md) |
| Writing a service operators call | Plat5's write-a-service guide, then [headers](reference/headers.md) |

## Concepts

| Doc | What it decides |
|-----|-----------------|
| [model](concepts/model.md) | Operators, services, the chain, acting as a member, authorization; the principles |
| [requests](concepts/requests.md) | The credential, how a request is decided, service routing, node resolution, acting as a member, authorization, forwarding |
| [audit](concepts/audit.md) | What the operator log records, delivery, what a service adds |
| [boundaries](concepts/boundaries.md) | What this plane is for and won't do |

## Reference

| Doc | |
|-----|--|
| [config](reference/config.md) | `operator.yml`: services, control-plane discovery, AuthZEN authorizer, policy claims, built-in templates |
| [headers](reference/headers.md) | Every header set, stripped or read |
| [gateway](reference/gateway.md) | Identity calls and cache, AuthZEN, route checks, trusted proxies, CORS, security headers, errors, request log, metrics |
| [idp](reference/idp.md) | The operator JWT |
| [env](reference/env.md) | Both binaries' environment |
| [api/operator-audit](reference/api/operator-audit.md) | Operator audit events, the read API, the gateway's writes |

## Internals

| Doc | |
|-----|--|
| [audit storage](internals/audit-storage.md) | Tables, roles, runtime |
