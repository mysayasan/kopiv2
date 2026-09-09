# MySeliaSan — Plan Editor Overhaul

Status: **P1 SHIPPED** (#250, 2026-09-09). **P2 BUILT, in review** — built and live-benched.
Phases P3–P5 remain planned; **P6 (coverage occlusion) is
planned but deliberately sequenced last** and is a separate decision to take when P1–P5 are in.

The plan editor is `FloorEditor` — `apps/myseliasan/views/react-webpack/src/views/components/floor_editor.js`.
It is the surface an operator authors a site on: walls, openings, stairs, raised floors, parking,
and the camera/appliance markers pinned to them. It is opened from the fleet map's twin tree for a
**building** (many storeys) or an **outdoor area** (one ground surface). A **point asset** never
reaches it — a junction is a pole, not a floor.

---

## 1. What this is for

> **A site-authoring tool for security installers — not a CAD package and not Blender.**

The plan exists to answer one question: *where is this camera, and what can it see?* Everything
authored on it earns its place by changing that answer. A wall blocks a view. A window does not,
above its sill. A tree blocks a view, and then it grows. A parking bay is a thing you aim at.

Three complaints motivate this work, and they are different complaints:

1. **It is a modal dialog.** A serious authoring tool gets a window of its own. Living inside a
   modal over the map is what caps how much toolbar, outliner and status bar it can ever carry,
   and it is the single largest reason the thing reads as a web form rather than an application.
2. **It looks like a toy.** The chrome reads as a school worksheet, not a viewport, and there is
   no way to type a number anywhere in it.
3. **It cannot describe outdoors.** An outdoor area gets fences, gates and parking bays. It has no
   roads and no trees, which are the two things an outdoor site is mostly made of.

### What it is not

These are the directions scope will pull, and all four should be refused:

- **Not a 3D modeller.** The 3D tab stays a *view*. Authoring geometry in 3D is a large cost for a
  floor plan that is inherently 2D, and it is the single biggest way this turns into a project
  without an end.
- **Not Blender's keymap.** Blender's modal interaction is famously hostile to newcomers. Our users
  are security installers. `G`/`R`/`S` ship as **accelerators advertised in the status bar**, never
  as the only way to move something.
- **Not a landscape/BIM tool.** Trees get a canopy, a height and a clear stem because those three
  numbers decide occlusion. They do not get species catalogues, seasons, or growth simulation.
- **Not a map.** Roads drawn here are the site's own roads on its own plan. The geographic basemap
  in the fleet map already has the world's roads and stays the place for those.

### The test for "just enough"

Every item below is justified by a thing an installer does on site. If a feature cannot be traced
to one, it is not in scope.

---

## 2. What exists today

The bones are better than the surface suggests, and this plan **keeps almost all of it**. Already
working in `floor_editor.js`:

- Tools `select · wall · room · round · door · window · stairs · platform · erase`, with per-kind
  faces so `wall` reads "Fence" and `door` reads "Gate" on an outdoor site (`TOOL_FACE`).
- Multi-select: box-select, shift-click, `Ctrl+A`, copy/cut/paste, bulk delete.
- Transform handles: eight resize handles plus a rotate knob (`plan_geometry.js` — `HANDLES`,
  `frameOf`, `handleWorld`, `rotateKnobWorld`), applied as one affine step about a pivot so a
  mixed selection transforms together.
- Undo/redo, 100 deep, snapshotting the whole model so one edit touching four arrays undoes in one
  step.
- Dockable panels: drag a panel's grip to float it, drop near an edge to dock left or right. This
  is already a professional-editor idea, half-built.
- Grid snap at `GRID_SUBDIV = 4` steps per metre cell.
- Live metre labels while drawing a wall, room or ellipse.
- Debounced autosave (700 ms) of the whole model plus scale and wall height.
- 2D ⇄ 3D toggle; `floor_3d.js` extrudes the same model through the *shared* `plan_geometry.js`,
  so 2D and 3D cannot drift on where a door cuts a wall.

### What actually makes it read as a toy

| # | Symptom | Where |
|---|---|---|
| 1 | Paper-coloured viewport — `#e9edf2` surface, `#f1f5f9` canvas, 7–10px rounded cards throughout | `fleet-map.css:387,397` |
| 2 | **Every** numeric property is a slider — door width, sill, head, rise, aim, FOV, mount height, steps, bays. You cannot type `0.90 m`. | `floor_editor.js` inspector |
| 3 | No object list. The inspector's resting state is a *tally* ("Walls 42, Doors 3"), so nothing on the plan can be found, hidden, locked or named. | `inspectorBody` |
| 4 | A selected object has no X / Y / rotation / dimensions | — |
| 5 | Pan is **scrollbars**. Zoom is +/− buttons and `Ctrl`+wheel, centred on the viewport middle. No middle-mouse pan, no zoom-to-cursor, no frame-all, no frame-selected. | `floor_editor.js:289–320` |
| 6 | Snap is invisible and compulsory — hardcoded, no magnet, no vertex/edge/midpoint modes, no way off it | `GRID_SUBDIV` |

---

## 3. Two structural facts that shape everything below

### 3.1 The model has four renderers

The `grid` JSON on `entities.FloorPlan` is drawn independently by:

| Renderer | File | Language |
|---|---|---|
| The editor canvas | `floor_editor.js` | JS / Canvas2D |
| The read-only floor view | `node_floor_view.js` | JS / SVG |
| The 3D scene | `floor_3d.js` | JS / three.js |
| The PDF report compositor | `services/report_floorgrid.go` | **Go** |

**Every new object type costs four implementations in two languages.** Adding roads, trees, hedges
and ground surfaces naively is sixteen new drawing routines, and history says the fourth one (Go)
is the one that silently falls behind — the PDF then quietly omits geometry the operator drew.
This is why P3 (§6.3) comes before P5, and why P5 is cheap once P3 is done.

### 3.2 Coverage is decorative — nothing computes occlusion

The only `Raycaster` in `floor_3d.js` is for mouse picking. Camera cones pass **straight through
walls** in both 2D and 3D. Today the plan is a picture; it does not tell you what a camera can
actually see, and it does not do so for walls either — this is not a gap that trees would
introduce.

This matters for the trees decision. The operational argument for drawing a tree at all is that it
blocks a camera. That argument only pays off in P6. Trees still earn their place in P5 without it —
an installer needs to see the obstruction on the plan to reason about it, and the survey drawing is
the artefact they hand the customer — but we should be honest that P5 draws trees and P6 is what
makes them *mean* something.

---

## 4. The scale trap (read before designing any numeric field)

`FloorPlan.Scale` is metres-per-pixel, and **it defaults to 0 = unset**. The existing model is
inconsistent about which side of that line each field sits on:

- `doors[].w`, `windows[].w` — **pixels**
- `windows[].sill`, `windows[].head`, `stairs[].height`, `platforms[].rise` — **metres**

A numeric inspector forces this question. You cannot type "0.90 m" for a door width on a plan with
no scale set, because there is no conversion. Two consequences, both scoped in:

1. **A "Set scale" tool ships in P2.** Drag a line across a dimension you know, type its real
   length, done. It is a standard pro-editor feature, it is cheap, and every metre field in the
   product is dead without it. It also improves the 3D view and the capacity estimate for free.
2. **Every new object type in P5 stores real-world dimensions in metres natively**, never pixels.
   Positions stay image-space pixels, matching the existing convention.
3. Where scale is unset, a metre field renders as a **px field with a "set scale" affordance**,
   rather than showing a fake metre number. Never fabricate a unit we do not have.

---

## 5. The forward-compatibility trap

`modelJSON()` rebuilds the saved object from the editor's known refs:

```js
const modelJSON = () => ({
  version: 2, unit, segments: …, stairs: …, doors: …, windows: …, parking: …, platforms: …,
});
```

Unrecognised top-level keys are **not carried through**. So once P5 bumps the model to `version: 3`,
an older build opening a v3 plan would load it, ignore `roads`/`trees`, and then **silently delete
them on its next autosave** — 700 ms after the operator so much as nudges a wall.

**Required in P3, before any new array exists:** load-time capture of every unrecognised key into an
`extraRef`, spread back into `modelJSON()` on save. Cheap, and it makes every later version bump
safe by construction. This is a genuine data-loss bug waiting to be introduced, not a hypothetical.

---

## 6. Phases

### P1 — The editor becomes a workspace (its own browser tab) — **BUILT, in review**

**This goes first**, because every piece of chrome P2 designs — dark viewport, menu row, status
bar, dock zones — is designed for a full window. Building it inside a modal and then moving it
means styling and benching it twice.

**The app has no URL routing at all.** `activeTab` is React state persisted per-tab in
`sessionStorage` (`useStickyTab`, `TAB_KEY = 'myseliasan_active_tab'`); every screen is a state
branch in `App.js`. A browser tab starts from a URL and nothing else, so this phase introduces the
app's **first route**. Keep it narrow — one route for the editor, not a router migration.

```
/plan/{siteId}                          the site's plan workspace, on its first area
/plan/{siteId}?area={floorId}           …on a specific area
/plan/{siteId}?area=3&pick=n1::7        …arriving already carrying camera 7 of node n1
```

Keyed on the **site**, not the floor: the workspace is a site editor with an area bar, so the
header names the site and the tabs are its areas. A site with no areas yet still has an address,
which a floor-keyed route could not express. `?area=` is rewritten (`replaceState`, not
`pushState`) as the operator switches tabs, so the address always describes what is on screen —
flicking through five areas should not bury the map five entries deep in the back button.

**No Go change is needed**, and the two traps that usually bite a deep route are already avoided:

- `spaHandler` (`infra/apphost/run.go:58-73`) already serves `index.html` for any unmatched path,
  so `/plan/123` reaches the SPA.
- webpack's `publicPath: '/'` emits **absolute** asset URLs (`src="/index.<hash>.js"`), so a deep
  path does not break chunk loading. A relative `publicPath` would have served `index.html` as
  JavaScript and produced a blank page with a syntax error.

**Open it as a real link.** `<a href="/plan/123" target="_blank">`, not a JS `window.open`. Ctrl-click,
middle-click, "open in new window", "copy link address" and the back button then all work the way
they do in real software, and it is the accessible answer as well.

**Getting the shell right** is the actual substance of the request: no `SideNav`, no
`WorkspaceHeader`. The route renders a full-viewport workspace — menu/tool row, stage, status bar,
dock zones — and nothing else. The surrounding app chrome is what makes the current editor read as
a web page with a drawing in it.

Details that have to be got right:

- **The URL beats the sticky tab.** `window.open`/`target="_blank"` gives the new tab a *copy* of
  the opener's `sessionStorage`, so without this the editor tab would restore whatever screen the
  opener happened to be on. Route wins at mount.
- **Document title** = `Ground floor — Head Office`. Two editor tabs are otherwise
  indistinguishable in the tab strip, which defeats the point of being able to open two.
- **Escape changes meaning.** The editor's capture-phase handler currently lets Escape *bubble* on
  purpose so the host dialog can close (`floor_editor.js`, the `hadDraft || hadSel` branch). In a
  standalone tab there is no dialog to close: Escape must cancel a draft or a selection and then
  stop. It must never navigate away.
- **The parent tab has to learn about changes.** Today `onChanged` refreshes the map inside one
  React tree. Across tabs, publish edits on a `BroadcastChannel` — a same-origin browser API, no
  server round trip, air-gap safe — with a refetch on `visibilitychange` as the fallback.
- **The drag-to-area gesture survives.** Dragging a camera from the map's not-placed tray onto an
  area currently opens the dialog already holding the pick (`initialPick` / `initialFloorId`).
  Cross-tab drag is not a thing, so that gesture opens the tab with `?pick=` instead.
  `BuildingEditorDialog` already owns its palette, area tabs and placement CRUD, so the workspace
  inherits a complete surface — this phase moves a shell, it does not restructure logic.
- **Autosave versus closing the tab — a data-loss risk this phase *introduces*.** The model
  autosaves on a 700 ms debounce. Nobody closes a modal by accident; people close tabs constantly.
  Flush any pending save on `pagehide`/`visibilitychange`, and guard with `beforeunload` while one
  is still in flight.
- **Session expiry and first run.** A tab opened with a dead session lands on the login screen, and
  a fresh install lands on the setup wizard, which blocks navigation. After either, the route must
  be honoured rather than silently replaced by the dashboard.

**Do we keep the dialog too? Recommend no — one surface.** Two hosts for the same editor means
every phase after this is styled, translated and benched twice, and the dialog is precisely what
makes the tool feel small. Opening a plan from the map becomes "open the plan", in a window that
can hold a real toolbar.

The payoff is more than cosmetic: plans become **bookmarkable and shareable** ("here's the link to
the plan we're arguing about"), and an installer can put **two areas side by side on two monitors**
— which the modal can never do.

*Touches:* `App.js` (route detection ahead of the sticky tab), new `lib/plan_route.js` and
`components/plan_workspace.js`, `building_editor_dialog.js` **deleted**, `fleet_map.js` and
`map/inspector.js` (dialog → link), `floor_editor.js` (autosave flush), `fleet-map.css`,
`map-workspace.css`, a `chev-left` glyph in the shared icon set, i18n ×4. No Go change.

### P2 — Viewport and navigation — **BUILT, in review**

The "stops looking like a toy" pass. Mostly CSS and input handling; no model change.

**Chrome.** A dark viewport surface with its own token block, scoped to `.floor-editor` so it does
not leak into the rest of the app (which stays theme-aware — see the suite's light/dark rules). Flat
compact panels, square-ish corners, 24px icon buttons, a real hairline between docks and stage.

**A status bar** along the bottom of the stage: current tool, selection count, cursor position in
metres, active snap mode, and a live hint for what the mouse buttons do right now. This is where
the `G`/`R`/`S` accelerators get advertised — the discoverability answer to §1's "not Blender's
keymap".

**Navigation.**

| Input | Action |
|---|---|
| Middle-mouse drag | Pan |
| Space + drag | Pan (laptops without a middle button) |
| Wheel | Zoom **to cursor** |
| `Ctrl` + wheel | Keep as zoom (existing muscle memory) |
| `Home` | Frame all |
| `.` | Frame selected |

Pan must stop being scrollbars. The current implementation zooms about the viewport centre by
fixing up `scrollLeft`/`scrollTop` (`floor_editor.js:311–320`); this becomes a real pan/zoom
transform, which also removes the `overflow: auto` special-casing.

**Snapping.** A magnet toggle with modes: grid · vertex · edge · midpoint · off. Hold `Ctrl` to
invert the current setting temporarily. Snap indicator drawn at the snapped point so the operator
can see *what* it caught.

**Set scale tool** — §4.

**Modal accelerators** (`G` move, `R` rotate, `S` scale; `X`/`Y` to constrain; type a number for an
exact value; `Enter` confirms, `Esc` cancels). Advertised in the status bar. Dragging keeps working
unchanged — this is additive.

**Deviation, taken deliberately:** the chrome was to be scoped to `.floor-editor`. Doing only that
produced a half-lit window — a light header and a light node palette butted against a dark dock and
a dark viewport — which read as an unfinished screen rather than an application. The dark now covers
the whole `.pw-shell`: header, area bar, palette, status bar. It still does not leak, which is what
the original wording was protecting: every rule is scoped under `.pw-frame`, and the rest of
myseliasan stays theme-aware.

**Two defects this phase produced, both found only by measuring:** the status bar's background and
then its text lost the cascade to base rules of equal specificity that appear later in
`fleet-map.css`, giving first a light bar and then dark-on-dark text. Neither looks like a bug — an
unreadable strip reads as an empty one — so the bench measures contrast against the surface each
label actually sits on, and every override is scoped under `.pw-frame` rather than relying on
source order.

*Touches:* `floor_editor.js`, `plan_workspace.js` (renders the status bar), `fleet-map.css`,
i18n ×4.

### P3 — The object registry

The enabler. No user-visible feature of its own, which is exactly why it is worth doing before P4
and P5 rather than after.

One declarative module — `components/map/plan_objects.js` — where each object type states
everything about itself:

```js
export const PLAN_OBJECTS = {
  road: {
    array: 'roads',              // key in the grid JSON
    geometry: 'polyline',        // polyline | rect | point | opening | segment
    collection: 'outdoor',       // outliner grouping
    kinds: [KIND_OUTDOOR],       // which site kinds offer this tool
    tool:  { id: 'road', icon: 'road', key: 'grid.road', hint: 'grid.roadHint' },
    fields: [
      { key: 'width',    type: 'length', unit: 'm', min: 1, max: 30, step: 0.1, default: 6,
        label: 'grid.roadWidth' },
      { key: 'surface',  type: 'enum', options: ['asphalt','concrete','gravel','paved'],
        default: 'asphalt', label: 'grid.surface' },
      { key: 'markings', type: 'enum', options: ['none','centre','lanes'], default: 'centre',
        label: 'grid.markings' },
      { key: 'kerb',     type: 'bool', default: true, label: 'grid.kerb' },
    ],
    draw2d(ctx, o, view) { … },  // canvas
    svg2d(o, view) { … },        // read-only floor view
    build3d(o, view) { … },      // three.js
    hit(pt, o, view) { … },
    bounds(o) { … },
  },
  // …
};
```

**Why the `fields` descriptor is the centrepiece:** it drives the numeric inspector, the outliner's
secondary label, *and* the Go parity test's expected field list — from one declaration. Migrating
the existing types (door, window, stair, parking, platform) onto it is what kills the
"sliders-only" problem across the whole product at once, rather than type by type.

`field.type` covers `length` (metre-aware, scale-dependent — §4), `angle`, `count`, `bool`, `enum`.
The inspector renders a **typed number input with an optional slider beside it** — never a slider
alone.

Also in P3:
- The `extraRef` passthrough of unrecognised model keys (§5).
- The three JS renderers switch to consuming the registry.
- **A parity test** that fails when the JS registry declares a type or field the Go compositor does
  not handle. Go cannot share the JS drawing code, so the honest fix is not to pretend it can — it
  is to make drift *loud*. The test reads the registry's array names and field keys and asserts the
  Go `floorGrid` struct covers them.

*Touches:* new `plan_objects.js`; `floor_editor.js`, `node_floor_view.js`, `floor_3d.js` refactored
onto it; new parity test; `report_floorgrid.go` unchanged this phase.

### P4 — Outliner and numeric inspector

The "is precise" pass. Depends on P3, because the outliner has to enumerate types generically.

**Outliner** — a docked tree of everything on the plan, grouped by the registry's `collection`:

```
Structure        Walls (42) · Doors (3) · Windows (7) · Stairs (1) · Raised floors (2)
Outdoor          Roads (2) · Trees (14) · Hedges (3) · Ground (1)      [P5]
Devices          Cameras (9) · Appliances (2)
```

Per-row and per-collection: **visibility** and **lock**. Click selects and syncs with the canvas;
selecting on the canvas reveals and scrolls the row. Visibility/lock are **view state, not model
state** — they are per-operator working preferences, so they live in `localStorage` keyed by floor
id, never in the saved grid. (Wrapped in try/catch; a browser blocking site data must still render
everything visible and unlocked.)

**Numeric inspector** — the N-panel. For any single selection: X, Y, rotation, width, height, all
typed, all in metres where scale allows (§4). Plus the type's own `fields` from the registry. For a
multi-selection: the fields common to every member, editable together.

*Touches:* new outliner component + CSS; `floor_editor.js` inspector replaced by a
registry-driven renderer; i18n ×4.

### P5 — The outdoor kit

Four new types. After P3 this is four registry entries plus their Go counterparts, not sixteen
drawing routines.

Model becomes `version: 3`. All arrays additive; a missing array reads as empty, which
`parseList()` already does correctly.

```jsonc
{
  "version": 3,
  "roads": [
    { "pts": [{"x":120,"y":80},{"x":540,"y":80},{"x":540,"y":420}],
      "width": 6.0, "surface": "asphalt", "markings": "centre", "kerb": true }
  ],
  "trees": [
    { "x": 300, "y": 200, "canopy": 4.5, "height": 8.0, "stem": 2.2, "species": "broadleaf" }
  ],
  "hedges": [
    { "pts": [ … ], "width": 0.8, "height": 1.6 }
  ],
  "ground": [
    { "pts": [ … ], "surface": "grass" }
  ]
}
```

Positions (`pts`, `x`, `y`) are image-space pixels, matching every existing type. Real-world
dimensions (`width`, `canopy`, `height`, `stem`) are **metres** — §4.

**Road.** A polyline centreline with a width, rendered as a ribbon. It reuses the wall tool's
click-to-add-corners interaction exactly (`draftRef.pts`, double-click or `Enter` to finish), so
there is no new interaction to learn. One shape covers road, driveway, footpath and cycle lane —
the difference is width and surface. In 3D it is a flat ribbon a few centimetres above the ground
slab, with the kerb raising its edges 0.12 m when set. In the PDF it is a thick grey stroke with a
dashed centreline.

> **A road must never be stored in `segments`.** Segments extrude into walls. A road in `segments`
> would become a 6-metre-wide wall down the middle of the site and destroy every camera view on
> the plan. Its own array, always.

**Tree.** A point with a canopy radius, a total height and a **clear stem**. The clear stem is the
field that earns the type: a camera mounted at 2.5 m may see *under* a canopy whose stem is clear
to 3 m, and that is precisely the argument installers have on site. `species` is
broadleaf/conifer/palm and drives the glyph only — no catalogue, no seasons (§1).
2D: a filled canopy circle with a trunk dot. 3D: a trunk cylinder to `stem`, then a canopy form to
`height`.

**Hedge.** A polyline with a width and a height — the low solid occluder that actually blocks a
fence-line camera, and the reason it is a separate type from a wall rather than a wall with a
height override: the outliner needs to group it as planting, and "erase hedge" must not mean
"erase wall".

**Ground.** An area polygon with a surface (grass / water / gravel / hardstanding). Purely
cosmetic, drawn beneath everything else. It is what makes a site plan legible as a site plan rather
than a diagram of fences.

**Toolset changes.** `TOOLSETS[KIND_OUTDOOR]` gains `road · tree · hedge · ground`.
`TOOLSETS[KIND_BUILDING]` gains **none of them** — a road inside a building is a category error,
and the outdoor toolbar is already the longer of the two. (A campus with both is modelled as an
outdoor area with buildings on it, which is what the twin tree already expresses.)

*Touches:* `plan_objects.js` (4 entries), `report_floorgrid.go` (4 renderers + struct fields),
`icons.js` (4 glyphs), i18n ×4, `apps/myseliasan/README.md`, the myseliasan manual.

### P6 — Coverage occlusion *(planned, sequenced last, separate go/no-go)*

The one that makes an authored plan *mean* something: the camera wedge stops being a decorative
cone and becomes a real visibility polygon.

2D: shadow-cast the wedge against walls, hedges and tree canopies — a standard visibility-polygon
sweep over the occluder edge set, computed in `plan_geometry.js` so all four renderers share one
answer. Height-aware: an occluder shorter than the camera's mount height does not block, a window
does not block above its sill, a tree with a clear stem above the mount height does not block.

3D: clip the cone geometry to the same polygon.

Payoff beyond the pretty picture: the PDF survey report starts showing *real* coverage, and the
capacity/coverage story becomes defensible to a customer. This is also the phase that retroactively
justifies drawing trees at all (§3.2).

Sequenced last because it is the largest item and because P1–P5 are independently shippable
without it. **Decide at the end of P5, not now.**

---

## 7. Cross-cutting obligations

- **i18n ×4** (`en` · `ms` · `zh` · `ar`) for every new string — flat `grid.*` keys, per the
  existing convention in `src/views/i18n/en.js`. Arabic is the fourth language.
- **Air-gap.** myseliasan has **no egress**. Every icon is inline SVG from `@shared`, every font is
  self-hosted, CSP stays `'self'`. No CDN, no external tile, no web font — check before adding any
  asset.
- **docs-sync before every commit**; **i18n-sync** as well, since every phase touches the frontend.
- **A `changes/pending/*` entry per phase.** Scope token matters: a `myseliasan` app bump is what
  produces a release. A doc-only or unknown token hard-fails the release run on main.
- **Live bench per phase, not just green tests.** The suite's base rate is roughly 28 benched items
  to 50 real defects; unit-green has never meant working here. Drive the actual screen, open the
  actual PNG. The throwaway-myseliasan sqlite + CDP recipe applies, including the two known traps:
  `password_change_required` on first login, and the setup wizard blocking navigation.
- **`npm run lint:undef` after every refactor pass** — it is the lint that catches the
  ReferenceErrors webpack hides, and P3 is a large refactor.
- **One PR per phase, each rebased onto `main`. Never stack PRs in this repo.**

---

## 8. Sequencing and status

| Phase | Depends on | Status |
|---|---|---|
| P1 Editor becomes a workspace (own tab) | — | **Shipped (#250)** |
| P2 Viewport and navigation | P1 (styles the shell P1 creates) | **Built, in review** |
| P3 Object registry | — (independent; land after P2) | **Planned** |
| P4 Outliner and numeric inspector | P3 | **Planned** |
| P5 Outdoor kit | P3 | **Planned** |
| P6 Coverage occlusion | P5 | **Planned — separate go/no-go after P5** |

**P1 must precede P2.** Every piece of chrome P2 builds — dark viewport, menu row, status bar, dock
zones — is sized for a full window; building it in the modal first means styling and benching it
twice. P3 is independent of both and could be done at any point, but running it after P2 keeps the
large refactor away from the phase that is mostly CSS.

Prerequisite: **PR #247** (`feat/myseliasan-point-asset-areas` — the fleet map twin tree) has
landed on `main`. Every phase here branches fresh from `main`; P1 shipped from
`feat/myseliasan-plan-workspace`.
