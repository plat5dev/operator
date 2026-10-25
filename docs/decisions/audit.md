# Audit

Why the operator log is separate from Plat5's, records every authenticated request, and carries the node chain. The rules: [`concepts/audit.md`](../concepts/audit.md), [`reference/api/operator-audit.md`](../reference/api/operator-audit.md), [`internals/audit-storage.md`](../internals/audit-storage.md).

Google Cloud keeps a separate record of what its own people do to a customer's resources, Access Transparency, apart from the customer's audit logs ([Access Transparency](https://docs.cloud.google.com/assured-workloads/access-transparency/docs/overview)). The operator log is that record.

## A separate log

| Decision | Why |
|----------|-----|
| The operator log lives in the operator plane's own Postgres, never Plat5's audit service or a tree's log | A tree's log is the customer's record of its own members; Plat5 keeps other front doors' events out of it (Plat5 `concepts/audit.md`). Operator access shown to customers is a later, deliberate feature ([`TODO.md`](../../TODO.md)) |
| Every authenticated request is an event, reads included, with no per-route switch | Operators reading customer data is the access this log exists to show. Plat5 skips reads by default because members read their own data; operators don't |
| 401s aren't events | There's no trusted actor, and nothing bounds the rate. The request log and a metric count them |
| The intent is written before anything answers, including a **400** or **404** | "No request reaches a service unrecorded" needs the write first; recording refusals too shows operators probing paths |
| The intent comes after the lookups in identity | The event then names the nodes and the member, and an identity failure is still recorded as `rejected` |
| Two writes keyed by the request id, intent then outcome, with the same timing as Plat5's gateway | One delivery model across both front doors, so the guarantee and its failure modes are learned once (Plat5 `concepts/audit.md#delivery`) |

## What an event records

| Decision | Why |
|----------|-----|
| The route node's chain, as `nodes` and `root_id`, the same shape as Plat5's events | "Every operator access to this org" is one filter on the chain, as Plat5 reads a tree's log by node (Plat5 `decisions/audit.md`). A console renders both logs with one component |
| `acting_as` records the member and its principal | The member is who the service recorded as author. The event is where the operator and the member meet |
| `service` names the route's service | Plat5's events and routes files call it `service`; one word for one thing |
| `created_id` from `X-Plat5-Audit-Created`, as Plat5 records it | A service sends one set of audit headers to every front door |
| `X-Plat5-Audit-Root` and `-Member` are dropped | They name a tree for a Plat5 sign-in route's log. This log isn't per tree |
| No authz decision field | Nothing decides yet. The field comes with the decision service |
| `params` keep every path param, chain params included | Plat5 drops chain params because their values are in `nodes`; here the route's own param names are what an operator searches by |

## Storage

| Decision | Why |
|----------|-----|
| Monthly partitions, a GIN index on `node_ids` beside `nodes`, append-only by trigger, a writer role that can't delete | The same layout as Plat5's audit storage (Plat5 `internals/audit-storage.md`), for the same reasons: retention is dropping partitions, reads by node are an array containment |
| One migration; an older database is recreated | Plat5 keeps one `001_init.sql` per service while it's early. An operator log from before the chain can't be filled in after the fact |
