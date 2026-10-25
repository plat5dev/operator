# Boundaries

What the operator plane is for, and what it won't do.

## What it's for

Operator work on a Plat5 deployment's customers: browsing the tree, fixing data during an incident, restoring a deleted project, suspending a person, recovering an org that lost its last admin. Each of these is a call to a service's own API by node id, recorded in the operator audit log.

Support that a customer should see and consent to is a binding in the customer's tree, not this plane: a support role the customer binds and removes again, since Plat5 bindings don't expire (Plat5 `docs/concepts/access.md`). This plane is for what happens without the customer in the loop.

## Not in the operator plane

Do not add these because they would be convenient:

- Serving UI or static files
- Operator accounts, passwords, cookies, or sessions
- API keys (deferred, [`TODO.md`](../../TODO.md))
- A role, group, or allow-list check in this gateway. Narrowing who may call is a decision service's job (deferred, [`TODO.md`](../../TODO.md))
- A member id without `/as/{member}`, or applying the member's bindings or labels to the request
- Operator headers, roles or rules in Plat5, or values for Plat5's headers that Plat5 doesn't define. This plane's own header is `X-Plat5-Operator-Id`
- The customer IdP, or a customer member or principal, as an operator
- Calling Plat5's gateway or control plane, or publishing routes into Plat5's config
- Slug resolution. Operators address nodes by id
- Filtering a response body, or asking a service to filter by operator
- Minting customer credentials, or writing another product's database
- Audit on for some routes or operators and off for others
- 401s, query strings, bodies, tokens, or claims other than the actor fields in an audit event
- A client-chosen request id
- Operator events in a Plat5 tree's log, or Plat5's audit service or database as the operator log's store
- Updating or deleting audit events through an API, or running `operator-audit` as a role that can delete
- Reading inside `X-Plat5-Audit-Details`

## What this is not

- A UI host
- An IdP, an account store, or a session store
- A dependency of Plat5's gateway, or a mode of it
- A place that stores customer people, orgs, or members
- The system that decides a member's permissions inside an org

## Costs accepted knowingly

- Every authenticated request waits on one internal write before it's answered. That's the price of "no request reaches a service unrecorded".
- A route that names a node waits on identity for the node's chain on its first request. Chains are cached for good after that, since nodes never move.
- Any authenticated operator may call any route until a decision service is wired in. Every call is recorded meanwhile.
- Ids in a query string or a body aren't seen by this gateway. A route whose body names a second customer is recorded on its path alone. Keep that in mind when choosing which routes to list.
