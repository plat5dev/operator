# Decisions

Why the operator plane is shaped the way it is, by topic, with the references each choice follows. These are reasons, not contracts: the rules live in [`concepts/`](../concepts/) and [`reference/`](../reference/), and where a reason and a contract differ, the contract wins.

| Topic | Why… |
|-------|------|
| [Front door](front-door.md) | A second gateway; ids and its own route list; the chain and other headers; acting as a member |
| [Audit](audit.md) | A separate operator log; every authenticated request; the chain on events; storage |
| [Operator identity](operator-identity.md) | One operator IdP, bearer tokens only, no checks in the gateway |

When a decision is settled, its rule goes into the contract first, and its reason into the topic here, in the same change.
