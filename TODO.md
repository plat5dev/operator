# To do

## Next

The docs describe the plane against Plat5's tree of kinds; the code follows in this order.

- **Routes** ([`docs/routes.md`](docs/routes.md)). `as_member` and `node`, the reserved `/as/` prefix and `{member}` param, boot checks. Rewrite `routes.yml` and `compose/routes.yml` for Plat5's identity and audit paths (`/nodes/{node_id}/…`, `/members/{member_id}/…`).
- **Gateway** ([`docs/model.md`](docs/model.md)). Strip every incoming `X-Plat5-*`; send `X-Plat5-Principal: operator` and `X-Plat5-Operator-Id`; match `/as/{member}`, check the member against identity (`IDENTITY_URL`), strip the prefix, send `X-Plat5-Member-Id`.
- **Audit** ([`docs/audit.md`](docs/audit.md)). `acting_as` on the intent, stored and filterable; the 400 member-check event.
- **End to end.** Against a local Plat5 stack: browse the tree, restore a deleted project, publish a plan version as a member and see the member as its author.
- **In `plat5 start`.** The CLI's local stack runs an operator module once the above lands.

# Deferred

| Work | Ready when |
|------|------------|
| Authz | Slice 3. Contract is [`docs/authz.md`](docs/authz.md) |
| Trusted proxies for an audit event's `ip` | An event's `ip` is needed as evidence |
| A reason or ticket on staff requests | A policy or an auditor requires a stated reason for access. Acting as a member may be that trigger |
| Staff access shown to customers | A customer needs to see staff access to their org |
| Configurable audit actor claims | Staff auth is fleshed out, or the IdP names the client in a claim other than `azp` or `client_id` |
| Audit retention and purge | A retention policy must be enforced, or storage growth hurts |
| Audit export to WORM storage or a SIEM | Tamper evidence beyond the database role is required, or security monitoring needs the events |
| API keys | A staff caller can't get a token from the IdP |
| Authz on query or body ids | A policy can't be written without them |
| Decision caching | Decision service latency is measured as a problem |
| Shared route source with the customer control plane | Hand-copied upstream URLs cause a real mismatch |
| Customer login provisioning | Operators must create customer logins |
| Request-and-approve access, like Google Cloud's Access Approval | A customer requires approval per staff access |
| Plat5 audit event by request id | Support works from tickets carrying a `request_id` (Plat5 `TODO.md`) |
