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
