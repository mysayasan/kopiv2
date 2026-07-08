# Module: apps/mymatasan/apis/observation.go

## Purpose

HTTP surface for the object metadata recorder's search: "what objects did this camera see", queryable by camera, label, and time window, each result linked to the footage segment covering it.

## Responsibilities

- `NewObservationApi(router, serv *services.ObservationService)` — registers under `/observations`:
  - `GET /observations` — paginated presence-interval search. `?cameraId=` scopes to one camera (omit/`0` for all); `filters`/`sorters` query params (shared `DataTable` server-mode format, parsed via `sharedapis.ParseListQueryOptions[entities.ObjectObservation]`) drive true server-side column filtering/sorting (time daterange, `Label`, `MaxCount`, etc.) so paging runs over the real filtered set. Returns `{items: []ObservationResult, total}`.
  - `GET /observations/labels` — distinct object labels observed (optionally scoped to `?cameraId=`), for the search UI's label filter list.

## Notes

- Thin handler: all search/footage-linkage/retention logic lives in `services.ObservationService`.
- Mirrors the Alert Log's `DataTable` server-mode contract (same filter/sorter query-param shape), so the frontend can reuse the same grid component for observation search.
