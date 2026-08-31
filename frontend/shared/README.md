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

- `DeploymentPanel` (`Deployment.js`, styles `styles/deployment.css`) — the shared deployment-mode/readiness surface, rendered by every app's setup wizard and/or Settings page over the identical `GET /api/deployment/preflight` + `GET`/`POST /api/deployment/mode` payloads (`domain/shared/apis/deployment.go.md`). Props: `api` (the app's own fetch adapter, resolving to `{ok, body}`/`{ok, message}` — apps whose own helper throws instead wrap it, see `apps/myidsan/views/react-webpack/src/views/components/setup.js`'s `deploymentApi`), `appLabel`, and optionally `operatorSteps`/`caveats` (translated string arrays — the operator checklist and known-limitations list, app-specific prose supplied by the caller since the component itself carries no text of its own beyond labels), `labels` (per-check-id label overrides, e.g. myseliasan's `llmMode` row), `readOnly` (hides the mode picker/Save action — unused by any current caller, since a Tier B app's `clusterable: false` response already renders the fixed appliance notice instead), `onToast`, `onSaved` (fires with the saved mode after a successful `POST`). On a Tier B (appliance) app the preflight response reports `clusterable: false` with a reason code, and the panel renders that reason instead of a `standalone`/`clustered` choice — the same component serves both a real declaration UI and a fixed read-only notice, driven entirely by what the server answers.

- `useStickyTab` / `readStickyTab` / `writeStickyTab` / `clearStickyTab` (`stickyTab.js`) — keeps an app's active section across a page refresh. `useStickyTab(key, fallback, allowed)` is a drop-in replacement for `useState('dashboard')`: it seeds from `sessionStorage[key]` (per-tab, never sent to the server, cleared on tab close) and writes through on every `setTab`. `allowed` (array or `Set` of reachable section ids) is the load-bearing argument — pass the app's own permission-derived list once it is known, and a restored section the current user can't reach falls back instead of rendering; pass a falsy `allowed` (not an empty list) while permissions are still loading, since an empty list means "may reach nothing" and would bounce the operator every time. Call `clearStickyTab(key)` on sign-out so the next person on the same terminal starts at the landing page rather than inside the last operator's work. Consumed by `mymatasan`, `myseliasan`, `myiotsan`, and `mypintusan`; `myidsan` intentionally keeps its own pre-existing cookie-backed mechanism (`myidsan_active_section` + a dedicated `UnauthorizedPage`) instead of switching to this.

## Theming: `--ui-*` design tokens
Shared CSS references only `--ui-*` variables (see `src/styles/tokens.css`). Each app
maps them to its own palette in its theme blocks (light + dark). Never hardcode colors
or app-specific vars in shared CSS.
