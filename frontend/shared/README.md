# Shared RBAC UI module (`@shared`)

One source of truth for the React UI components shared across the control-plane apps
(`myidsan`, `myseliasan`, and future apps) so a component is written/edited **once**
instead of hand-copied per app (which drifted — different table logic, different icon
sets, a toast tone fixed twice).

## How it's consumed
No npm package, no build step. Each app's webpack resolves `@shared` to this folder's
`src/` and resolves React from the app's own `node_modules`:

```js
// apps/<app>/views/react-webpack/webpack.config.js
resolve: {
  alias: { '@shared': path.resolve(__dirname, '../../../../frontend/shared/src') },
  modules: [path.resolve(__dirname, 'node_modules'), 'node_modules'],
},
```

Then in app code:

```js
import { DataTable, ToastStack, Ico } from '@shared';
```

Each app's babel-loader transpiles these files (they're plain ESM React, outside any
`node_modules`).

## Components
- `DataTable` — client-side filter/sort/page grid. Columns: `{ key, label, render?, filterable?, filterType? }`. (Renders `.table-surface`/`.pager`/`.filter-*` classes — styled by the app's table CSS.)
- `ToastStack` — floating transient notifications. The app owns `toasts` state + a `pushToast(text, kind)`; `kind` ∈ `success | error | info`. Ships its own tokenized CSS.
- `Ico` / `icoSvg` — the shared icon vocabulary. Add new glyphs here, never per-app. `Ico` defaults to `sz=14`.

## Theming: `--ui-*` design tokens
Shared CSS references only `--ui-*` variables (see `src/styles/tokens.css`). Each app
maps them to its own palette in its theme blocks (light + dark). Never hardcode colors
or app-specific vars in shared CSS.
