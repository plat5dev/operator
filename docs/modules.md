# Console modules

The console is a shell. A module is a page it loads from config. Identity is the default row, not a special case. Remove the row and the page is gone. The files may stay.

A module is static files this process serves. The session cookie is httpOnly and is not given to JavaScript. A module on another origin cannot send it. Deploy adds a page by mounting files and listing them. It does not fetch UI from a URL this process does not serve.

## Config

`MODULES_FILE` (default `modules.yml`). Missing file refuses boot. An empty list is valid: the service menu is empty.

```yaml
modules:
  - id: identity
    title: Identity
    base_path: /identity
    entry: /modules/identity/entry.js
```

| Field | |
|-------|--|
| `id` | Identifier. Unique. Not shown as the only label. |
| `title` | Service menu label. |
| `base_path` | Browser page. Not a gateway path. |
| `entry` | Same-origin script under `/modules/`. |

`base_path` must not be `/`, `/login`, `/account`, `/session`, `/logout`, `/health`, `/modules`, `/vendor`, `/assets`, `/users`, `/organizations`, `/members`, or a path under those. Two modules may not share a path or sit under each other's path.

`entry` is a path, not a URL. No scheme, no `//`, no `..`. `{MODULES_DIR}` plus the path after `/modules/` is the file. `MODULES_DIR` defaults to `modules/dist` in a local build and `/modules` in the image.

The shell reads the list from the page. It does not import a module. It does not branch on `id`. It mounts each component on `{base_path}/*` inside the existing router and loads `entry`.

## What a module exports

An ES module. Default export is a React component. No props. No second router.

The shell already has a router. Descendant `<Routes>` match under the base path. Relative links work. The component is not told its base path.

These specifiers are provided by the shell. Leave them external. Bundling another copy breaks the shell:

- `react`
- `react-dom`
- `react-dom/client`
- `react/jsx-runtime`
- `react/jsx-dev-runtime`
- `react-router-dom`

They are the versions the console was built with. A different major will not work. Cloudscape is not one of these. A module that wants it depends on it itself.

The shell loads `entry` and nothing else. A relative import from that script is served from the same directory. The shell does not load a stylesheet. Put styles in the script.

## Calls

Same origin. `credentials: "include"`. Paths are this gateway's routes (`/organizations`, `/users/…`, `/members/…`), not a service address and not the customer gateway.

The shell does not pass the operator id. Do not write it into a path or into `added_by`, `created_by`, or `created_by_user_id`. Those fields are customer user ids. Blank means null.

A module is trusted code. It runs on this origin and can call any route this operator can call.

## What this is not

- A remote loader, an import from another origin, or a page that is not a file this process serves
- A scan of a directory. The file is the list
- The route list. Routes and modules are separate config
- A sandbox
