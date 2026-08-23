# Module: apps/mymatasan/apis/observation.go

## Purpose

HTTP surface for the object metadata recorder's search: "what objects did this camera see", queryable by camera, label, and time window, each result linked to the footage segment covering it.

## Responsibilities

- `NewObservationApi(router, serv *services.ObservationService, search *services.SightingSearch, appearance *services.AppearanceService)` — registers under `/observations`:
  - `GET /observations` — paginated presence-interval search. `?cameraId=` scopes to one camera (omit/`0` for all); `filters`/`sorters` query params (shared `DataTable` server-mode format, parsed via `sharedapis.ParseListQueryOptions[entities.ObjectObservation]`) drive true server-side column filtering/sorting (time daterange, `Label`, `MaxCount`, etc.) so paging runs over the real filtered set. Returns `{items: []ObservationResult, total}`.
  - `GET /observations/labels` — distinct object labels observed (optionally scoped to `?cameraId=`), for the search UI's label filter list.
  - `GET /observations/search` — **federated cross-node search (W2-4, F-10)**: this node's answer to one leg of a fleet-wide "what objects did the fleet see" query. Query params (`from`/`to` unix seconds, repeatable/CSV `labels`, `minConfidence`, `limit`) are parsed by `sightingQueryFromRequest` — shared with `apis/vision.go`'s identity search so the two halves of one fleet search can never disagree about the window or confidence floor they were asked for. Delegates to `services.SightingSearch.SearchObjects`; returns a `SightingPage` (`items`, `capped`, `oldest`). Deliberately kept under `/observations` rather than a namespace of its own so the existing Objects page grant governs it — a role that may read this node's object metadata over its own UI may read it through the control-plane tunnel too, and no role gains reach because a fleet feature shipped. Called by `myseliasan`'s `apps/myseliasan/services/fleet_search.go` over the control channel, never by a browser directly.
  - `GET /observations/appearance` — appearance search (W3-2), "find more like this". See below.
  - `GET /observations/appearance/vector` — one sighting's appearance descriptor, in wire form, for federation. See below.

## Appearance search (W3-2)

Two more routes under `/observations`, for the same reason `/search` is: they rank/read the
very rows the Objects page grant already governs, so no administrator has to notice a new
path to keep an operator's existing reach. `/appearance/vector` is also the second federation
entry point — the control plane calls it before it calls `/appearance` (see
`myseliasan`'s `services/fleet_appearance.go.md`).

- **`GET /observations/appearance/vector?observationId=`** (`appearanceVector`) — returns one
  sighting's descriptor in the URL-safe wire form: `{observationId, vector, model, label, dim}`.
  `500` (`"appearance search is unavailable"`) if `appearance` is `nil`; `400` if
  `observationId` is missing; `400` (not `404`) if the sighting has no descriptor — deliberately
  distinguished from "not found", since the sighting exists but was recorded before appearance
  search was turned on for that camera (or on a camera where it never was).

- **`GET /observations/appearance?observationId=&from=&to=&cameraId=&limit=&minStandout=`**
  (`appearanceSearch`) — ranks this node's recorded sightings against one sighting's appearance.
  The query is named by an **observation**, not an uploaded photograph — deliberate: ranking the
  whole estate against a supplied picture is a different feature with a different risk profile
  (a review tool becoming a watchlist), and the workflow here starts on the screen, from a
  sighting the operator is already looking at.
  - Resolves the query vector from `observationId` (the normal, browser-driven case — `400`,
    not "no matches", if that sighting has no descriptor, so an operator doesn't conclude the
    person appeared nowhere else when the truth is the camera never described them) **or** from
    a raw `vector`/`model` query pair (the control-plane fan-out case: the sighting lives on
    whichever node the operator was watching and means nothing to the others, so the control
    plane resolves the descriptor via `/appearance/vector` first and passes the vector instead —
    see `EncodeAppearanceVectorParam`/`DecodeAppearanceVectorParam` in
    `services/appearance_search.go.md`). An optional `excludeObservationId` accompanies the
    vector form so the node that owns the sighting can drop its own pick (which would otherwise
    return as its own best match at `1.00`).
  - `cameraId` accepts repeated and/or comma-separated values.
  - Delegates to `services.AppearanceService.Search`, then — when `serv` (`ObservationService`)
    is set and there are hits — resolves each hit's footage via `ObservationService.ResolveFootageFor`
    so a ranked shortlist an operator cannot open is never returned; a shortlist an operator
    *can* open opens the same file the Objects grid would.
  - `services.ErrAppearanceRangeTooWide` maps to `400` (the window is answerable, just not this
    wide) rather than `500`.
  - `label`/`model` are ultimately required (from the resolved sighting or supplied directly);
    missing either is `400`.

## Notes

- Thin handler: all search/footage-linkage/retention logic lives in `services.ObservationService`; the federated-search logic lives in `services.SightingSearch` (`services/sighting_search.go.md`); the appearance ranking logic lives in `services.AppearanceService` (`services/appearance_search.go.md`).
- Mirrors the Alert Log's `DataTable` server-mode contract (same filter/sorter query-param shape), so the frontend can reuse the same grid component for observation search.
- `/search` and `/appearance`/`/appearance/vector` return `500` (`"search is unavailable"` /
  `"appearance search is unavailable"`) if the corresponding service is `nil` rather than
  panicking — the same defensive shape `apis/vision.go`'s identity endpoint uses.
- `parseFloatQuery` (used by `minStandout`) is defined in `apis/recording.go` and shared across
  the `apis` package — see `apis/recording.go.md`.
- Federated by `myseliasan` at `GET /api/nodes/search/appearance` — see `myseliasan`'s
  `services/fleet_appearance.go.md` / `apis/fleet_search_api.go.md`.
