# Operator — agent contract

`docs/` is law. If code and a contract disagree, fix the one that is wrong. Do not invent a third story in chat.

How I decide: `my-principles` skill. This file is **what Operator is**. Do not copy philosophy here.

## What this is

A headless gateway for staff. A second front door onto the private APIs a Plat5 deployment already fronts. Staff JWT in, audit event written, path out.

This repo also holds `operator-audit`, the service that stores the staff audit log.

Not the customer gateway. Not an IdP. Not a UI host. Not a policy engine.

## Locked

Read the doc, don’t re-derive:

| Invariant | Where |
|-----------|--------|
| The operator is the actor. The path is the target | [`docs/model.md`](docs/model.md) |
| Operator id is never written into the path. No actor header | model |
| The only credential is a bearer JWT from the staff IdP, with a required audience | [`docs/idp.md`](docs/idp.md) |
| This gateway stores no accounts, passwords, or sessions | model |
| Path forwarded exactly as matched. Ambiguous paths are 400 | model |
| Own route list. No customer gateway, no route-registry | [`docs/routes.md`](docs/routes.md) |
| Audit is on or off for the whole deployment, and on by default. On, every authenticated request is an event, written before forward. Not written → 503 | [`docs/audit.md`](docs/audit.md) |
| This gateway generates every request id. A client's is not accepted | model |
| The staff log is in the operator plane's own Postgres, never Plat5's. Customers do not see it | audit |
| Events are append-only. `operator-audit` runs as a role that cannot delete | audit |
| `X-Plat5-Audit-Details` is Plat5's contract, stored as sent. Stripped both ways on every route | audit |
| Authz is an AuthZEN decision service or nothing. Fail closed | [`docs/authz.md`](docs/authz.md) |
| Lists are all-or-nothing actions. Nothing filters a response by operator | authz |
| Errors use the Plat5 envelope | model |

## Stop conditions

Do not add these because they would be convenient:

- Serving UI or static files
- Operator accounts, passwords, cookies, or sessions
- API keys (deferred, see below)
- Operator id, role, or an actor header on the upstream request
- The customer IdP, or a customer `user_id`, as an operator
- Proxying through the customer gateway, or publishing routes into its route map
- A built-in policy, role check, or groups check. Authz goes through AuthZEN
- Sending the staff token to the decision service
- Caching authz decisions
- Filtering a response body, or asking a service to filter by operator
- Changes to plat5 or Auth
- Minting customer credentials, or writing another product's database
- Audit on for some routes or operators and off for others
- 401s, query strings, bodies, tokens, or claims other than the actor fields in an audit event
- A client-chosen request id
- Staff events in a Plat5 org log, or Plat5's audit service or database as the staff log's store
- Updating or deleting audit events through an API, or running `operator-audit` as a role that can delete
- Reading inside `X-Plat5-Audit-Details`

## Deferred

| Work | Ready when |
|------|------------|
| Authz | Slice 3. Contract is [`docs/authz.md`](docs/authz.md) |
| Trusted proxies for an audit event's `ip` | An event's `ip` is needed as evidence |
| A reason or ticket on staff requests | A policy or an auditor requires a stated reason for access |
| Staff access shown to customers | A customer needs to see staff access to their org |
| Configurable audit actor claims | Staff auth is fleshed out, or the IdP names the client in a claim other than `azp` or `client_id` |
| Audit retention and purge | A retention policy must be enforced, or storage growth hurts |
| Audit export to WORM storage or a SIEM | Tamper evidence beyond the database role is required, or security monitoring needs the events |
| API keys | A staff caller cannot get a token from the IdP |
| Authz on query or body ids | A policy cannot be written without them |
| Decision caching | Decision service latency is measured as a problem |
| Shared route source with the customer route-registry | Hand-copied upstream URLs cause a real mismatch |
| Customer login provisioning | Operators must create customer logins |

## Siblings

Plat5 is the customer runtime (gateway, identity, route-registry). Auth is the customer IdP. This repo does not import either. It dials service addresses. It reads `X-Plat5-Audit-Details` as Plat5 defines it, the same way it forwards paths as Plat5's services define them.

A console is a separate repo and a client of this API.

## Working here

1. Read `docs/` before editing.
2. Do not implement past the signed-off contract.
3. Don’t commit unless asked.
