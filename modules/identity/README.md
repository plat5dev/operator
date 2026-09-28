# identity

Default console module. Same shape as any other module: own package, own build, loaded from `modules.yml`.

Contract: [`docs/modules.md`](../../docs/modules.md).

```bash
npm ci && npm run build
```

Output is `modules/dist/identity/entry.js`. Remove the identity row from `modules.yml` to drop the page.
