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
- `Tabs` — THE standardized tab bar for every app (icon + label buttons, muted text that goes accent-colored with a 3px accent underline when active). Props: `tabs` (`{ id, label, icon?, disabled?, title? }[]`), `active`, `onChange`, `ariaLabel`, and an optional `className` for per-container horizontal alignment only — vertical spacing (a fixed 16px bar→content gap, uniform button height) is owned here so it stays standard across apps; the parent wrapper must not add its own `gap`. Ships its own tokenized CSS (`styles/tabs.css`).
- `CameraHero` (+ `statusTone` helper) — THE camera-page header: mymatasan's own camera detail page and myseliasan's Nodes → Camera page render the identical component, above their `Tabs` bar. Renders a breadcrumb trail (`crumbs`: `{ label, onClick? }[]`, last entry always plain text), a status-tinted camera avatar tile with a reachability dot, the camera name + optional description, and a row of status `chips` (`{ key, label, tone, icon?, capitalize? }[]`). `statusTone(status, pendingValue?)` maps a raw backend status string (`online`/`offline`/anything else, or a caller-named `pendingValue` such as `"resolved"`) onto the `online | offline | pending | unknown` tone the hero and its chips understand. Ships its own tokenized CSS (`styles/camera-hero.css`, `--ui-*` only, tints via `color-mix`).
- `DataTable` — filter/sort/page grid, client or server mode. Columns: `{ key, label, render?, filterable?, filterType? }` (`filterType: 'daterange'` renders a From/To date-range popover that emits two conditions, e.g. for a `createdAt` column). Default (`serverMode` unset/false) filters/sorts/pages in-memory over `rows`. Pass `serverMode` to make it a controlled view instead: `rows` is already the current page, `total` is the full filtered count, and `onQuery({ filters, sorters, offset, limit })` fires whenever filter/sort/page state changes so the parent can run the query against the backend's `filters`/`sorters` query-param contract (`domain/shared/apis.ParseListQueryOptions`) — used by mymatasan's Alert Log for true DB-side paging over large detection histories. Optional `pageSizeOptions` + a page-size selector in the pager; optional `initialFilters` seeds column filters at mount (remount via a `key` to apply a fresh seed, e.g. a "Today" preset). (Renders `.table-surface`/`.pager`/`.filter-*` classes — styled by the app's table CSS.)
- `ToastStack` — floating transient notifications. The app owns `toasts` state + a `pushToast(text, kind)`; `kind` ∈ `success | error | info`. Ships its own tokenized CSS.
- `Ico` / `icoSvg` — the shared icon vocabulary. Add new glyphs here, never per-app. `Ico` defaults to `sz=14`.
- `ManualProvider` / `ManualLibrary` / `ManualDrawer` / `HelpButton` / `useManual` — the built-in user manual. The app supplies only CONTENT (markdown embedded in its binary and served by `/api/manual`, see `domain/shared/manual`); this module supplies the reader, full-text search, the contextual slide-over, and the printed book. Wiring an app is: mount `<ManualProvider apiBase={apiBase()} lang={lang} appName="…">` around the tree (OUTSIDE the authenticated shell, so help works on the sign-in screen and in the first-run wizard), render `<ManualLibrary/>` on a Help page, and call `useManual().openHelp(slug, anchor)` from a `?` button. Ships its own tokenized CSS (`styles/manual.css`) including the `@media print` rules — "Print the whole manual" renders every article, with a table of contents, into a portal on `<body>` and calls `window.print()`, so Save-as-PDF works in every language (a server-side PDF could not: `domain/report`'s pure-Go writer is cp1252-only and cannot render Chinese or Arabic).
  - Contextual deep links must target a **hand-written** `{#anchor}` in the article, never a heading's generated id. Hand-written anchors are kept byte-identical across all four translations (enforced by `domain/shared/manual/manualcheck`); generated ids are positional and would drift.

## Theming: `--ui-*` design tokens
Shared CSS references only `--ui-*` variables (see `src/styles/tokens.css`). Each app
maps them to its own palette in its theme blocks (light + dark). Never hardcode colors
or app-specific vars in shared CSS.
