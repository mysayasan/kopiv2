# Module: apps/mymatasan/apis/observation.go

## Purpose

HTTP surface for the object metadata recorder's search: "what objects did this camera see", queryable by camera, label, and time window, each result linked to the footage segment covering it.

## Responsibilities

- `NewObservationApi(router, serv *services.ObservationService, search *services.SightingSearch)` — registers under `/observations`:
  - `GET /observations` — paginated presence-interval search. `?cameraId=` scopes to one camera (omit/`0` for all); `filters`/`sorters` query params (shared `DataTable` server-mode format, parsed via `sharedapis.ParseListQueryOptions[entities.ObjectObservation]`) drive true server-side column filtering/sorting (time daterange, `Label`, `MaxCount`, etc.) so paging runs over the real filtered set. Returns `{items: []ObservationResult, total}`.
  - `GET /observations/labels` — distinct object labels observed (optionally scoped to `?cameraId=`), for the search UI's label filter list.
  - `GET /observations/search` — **federated cross-node search (W2-4, F-10)**: this node's answer to one leg of a fleet-wide "what objects did the fleet see" query. Query params (`from`/`to` unix seconds, repeatable/CSV `labels`, `minConfidence`, `limit`) are parsed by `sightingQueryFromRequest` — shared with `apis/vision.go`'s identity search so the two halves of one fleet search can never disagree about the window or confidence floor they were asked for. Delegates to `services.SightingSearch.SearchObjects`; returns a `SightingPage` (`items`, `capped`, `oldest`). Deliberately kept under `/observations` rather than a namespace of its own so the existing Objects page grant governs it — a role that may read this node's object metadata over its own UI may read it through the control-plane tunnel too, and no role gains reach because a fleet feature shipped. Called by `myseliasan`'s `apps/myseliasan/services/fleet_search.go` over the control channel, never by a browser directly.

## Notes

- Thin handler: all search/footage-linkage/retention logic lives in `services.ObservationService`; the federated-search logic lives in `services.SightingSearch` (`services/sighting_search.go.md`).
- Mirrors the Alert Log's `DataTable` server-mode contract (same filter/sorter query-param shape), so the frontend can reuse the same grid component for observation search.
- `/search` returns `500` (`"search is unavailable"`) if `search` is `nil` rather than panicking — the same defensive shape `apis/vision.go`'s identity endpoint uses.
