# To do

## Next

- **The operator console.** `template-operator-console` follows the route list here: the tree from `GET /nodes`, node pages by id, members by root, acting as a member, and the staff log by node. It still calls identity's earlier paths.
- **In `plat5 start`.** The CLI's local stack runs an operator module: this gateway, operator-audit, the staff IdP and their Postgres, on Plat5's network with `IDENTITY_URL` set. Plat5's `TODO.md` holds the same item.
- **Starters' staff routes.** Each starter's `routes.operator.yml` lists its API's own paths by node id, with `node` and `as_member` where they apply ([`docs/reference/routes.md`](docs/reference/routes.md)), and `plat5 init` merges them under `services:`.
- **Telemetry to Plat5's contract.** Plat5's log field names (`timestamp`, `message`), `http_requests_total` and `http_request_duration_seconds`, process metrics, and OTLP traces when `OTEL_EXPORTER_OTLP_ENDPOINT` is set (Plat5 `docs/reference/telemetry.md`).

# Deferred

## Authorization

Narrowing which operators may call which routes, on which customers. This gateway is the enforcement point and never the decision: it asks an [OpenID AuthZEN](https://openid.net/wg/authzen/) 1.0 decision service and enforces the answer. RBAC, ABAC or relationships are that service's choice.

- **Off or on.** `AUTHZ_URL` unset: every authenticated operator may call every route, as today. Set: every request past the lookups in identity is evaluated, after the audit intent. No built-in or fallback policy.
- **The request.** `POST {AUTHZ_URL}/access/v1/evaluation`. Subject: the operator id and configured token claims, never the token. Action: method and route template (`/as/{member}` included), or a name the route gives. Resource: the route node, with its chain and the path params. Context: the request id and the member acted as.
- **The answer.** `decision: true` forwards. `false` → **403** `FORBIDDEN`. Anything else, a timeout or an unreachable service → **503**. Fail closed. Not cached. Recorded on the audit event as its `decision`, which the event doesn't carry until then.
- **Lists.** A list is its own action, all or nothing. `GET /nodes` returns every customer; grant it only to operators who may see every customer. Operators scoped to some customers reach them by id, as from a ticket.
- **Not evaluated.** Ids in the query string or the body. Authz on them waits until a policy can't be written without them.
- **An example.** A decision service with a groups-to-roles policy, as a deployment to copy, not a dependency.

Ready: a deployment where not every operator may reach every customer.

## Smaller items

| Work | Ready when |
|------|------------|
| Trusted proxies for an audit event's `ip` | An event's `ip` is needed as evidence |
| A reason or ticket on staff requests | A policy or an auditor requires a stated reason for access. Acting as a member may be that trigger |
| Staff access shown to customers, like Google Cloud's Access Transparency | A customer needs to see staff access to their org. Events already carry the chain to filter by |
| Configurable audit actor claims | The IdP names the client in a claim other than `azp` or `client_id` |
| Audit retention and purge | A retention policy must be enforced, or storage growth hurts. Partitions are monthly, so retention is dropping them |
| Audit export to WORM storage or a SIEM | Tamper evidence beyond the database role is required, or security monitoring needs the events |
| API keys | A staff caller can't get a token from the IdP |
| A route source shared with Plat5's config | Hand-copied service URLs or paths cause a real mismatch |
| Customer login provisioning | Operators must create customer logins |
| Request-and-approve access, like Google Cloud's Access Approval | A customer requires approval per staff access |
| Plat5 audit event by request id | Support works from tickets carrying a `request_id` (Plat5 `TODO.md`) |
| Checking suspension when acting as a member | Plat5 identity refuses a suspended person's member id, or a policy forbids acting for one |
