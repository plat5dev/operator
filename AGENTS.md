# Operator: agent contract

Headless gateway for operators: a second front door onto the private service APIs a Plat5 deployment runs, plus `operator-audit` for the operator audit log. Imports no Plat5 code.

## Where truth lives

- **Contracts:** `docs/concepts/` and `docs/reference/`.
- **Internals:** `docs/internals/audit-storage.md`.
- **Index:** [`docs/README.md`](docs/README.md).

## Working here

1. Read `docs/concepts/model.md` and the relevant concept/reference doc before editing.
2. Don't implement past the contract. Propose doc changes first for any principle in `concepts/model.md` or item under `Not in the operator plane` in `concepts/boundaries.md`.
3. Describe the current contract, not past shapes or migrations.
4. Don't commit unless asked.
