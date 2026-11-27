# Boundaries

What the operator plane is for, and what it won't do.

## What it's for

Operator work on a Plat5 deployment's customers: browsing the tree, fixing data during an incident, restoring a deleted project, suspending a person, recovering an org that lost its last admin. Each of these is a call to a service's own API at `/services/{service}/{path…}` by node id, authorized by an external AuthZEN PDP and recorded in the operator audit log.

Support that a customer should see and consent to is a binding in the customer's tree, not this plane: a time-limited or revocable support role the customer binds (Plat5 `docs/concepts/access.md`). This plane is for what happens without the customer in the loop.

## Not in the operator plane

Do not add these because they would be convenient:

- Serving UI or static files
- Operator accounts, passwords, cookies, or sessions
- API keys
- Built-in role, group, or allow-list rules in this gateway. Narrowing who may call (including when `X-Plat5-As-Member` or `X-Plat5-Operator-Reason` is allowed or required) is the external AuthZEN authorizer's job
- Guessing node ids by scanning arbitrary path segments for ULIDs
- A member id without `X-Plat5-As-Member`, or applying the member's bindings or permissions to the request
- Operator headers, roles, endpoints, or rules in Plat5, or values for Plat5's headers that Plat5 doesn't define. This plane's own headers are `X-Plat5-Operator-Id` (to services) and `X-Plat5-Operator-Reason` (from clients, never forwarded)
- The customer IdP, or a customer member or principal, as an operator
- Calling Plat5's customer gateway, or writing to Plat5's control plane
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
- A dependency of Plat5 (Plat5 never depends on the operator plane)
- A place that stores customer people, orgs, or members
- The system that decides a member's permissions inside an org

## Costs accepted knowingly

- Every authenticated request waits on one internal audit intent write before it's answered. That's the price of "no request reaches a service unrecorded".
- When `authorizer.mode: authzen`, every valid request waits on a live, uncached AuthZEN evaluation before forwarding. Operator traffic is low-volume and high-privilege; instant revocation matters more than caching.
- A route that names a node waits on identity for the node's chain on its first request. Chains are cached for good after that, since nodes never move.
- Ids in a query string or a body aren't seen by this gateway. A route whose body names a second customer is recorded on its path alone.
