# Module: apps/myseliasan/apis/fleet_search_api.go

## Purpose

HTTP surface for federated cross-node search (W2-4, F-10): "where was this seen" across
every adopted node the caller can reach, in one request.

## Endpoints

| Method | Path | Notes |
|---|---|---|
| GET | `/api/nodes/search` | Runs a fleet-wide sighting search. See query parameters below. Returns `services.FleetSearchResult`: `{items: []FleetSightingHit, total, coverage, truncated}`. |
| GET | `/api/nodes/search/labels` | The fleet-wide union of recorded object labels, for the search UI's label picker. Returns `services.FleetLabelsResult`: `{labels: []string, coverage}`. |

### Query parameters (both routes)

| Param | Type | Notes |
|---|---|---|
| `from` / `to` | int64 | Unix-seconds window. |
| `sources` | CSV/repeatable | `objects` and/or `identities`; empty/unrecognized means both. |
| `labels` | CSV/repeatable | Restricts object hits to these detector labels. |
| `text` | string | Identity substring — a plate, part of one, or a person's name. |
| `identityKinds` | CSV/repeatable | `plate` and/or `face`; empty/unrecognized means both. |
| `minConfidence` | float | 0..1 floor. |
| `siteId` | int64 | Restrict to nodes at one site. |
| `nodeId` | string | Restrict to one node. |
| `limit` | int | Caps the merged result set (default 200, max 2000). |

## Responsibilities

- `NewFleetSearchApi(router, auth, session *middlewares.AccessSessionMidware, search *services.FleetSearchService, audit services.IAuditService)` — mounts under `/api/nodes` (via `PathPrefix("/nodes")`), behind `auth.Middleware` only (no separate RBAC middleware on this subrouter).
- **Deliberately lives under `/api/nodes`, not a namespace of its own.** The permission matrix matches by path prefix, so a role already granted `GET` on `/api/nodes` — which every role that can use the fleet screens already has, and which the browser-side fan-out this replaces already relied on — can search without an administrator needing to notice a new path and grant it. A new top-level prefix would have silently taken the feature away from every non-superadmin role the day it shipped.
- The real authorization is not the prefix: `roleId(r)` resolves the caller's **live** role (via `session.CurrentPrincipal`, falling back to `operatorIdentity(r)`) and `FleetSearchService.Search`/`Labels` re-resolve per-node access from the same grants the node proxy uses — enforced again by the node itself against its own permission matrix. A just-demoted operator loses reach without a re-login, the same rule the node proxy applies.
- `runSearch`/`labels` — thin handlers: parse the query (`fleetSearchQueryFromRequest`), call the service, `SendResult`. `runSearch` also calls `recordSearch` after a successful call.
- `recordSearch` — audits every fleet-wide search as `fleet.search` (`TargetType: "fleet"`, `TargetId: nodeId` if scoped). Tunneled READS are deliberately not audited elsewhere in this app (it would drown the trail), but this one is: an estate-wide sweep for a named person or a specific vehicle, across every site at once, is exactly the question a data-protection review asks — "who searched for this plate, and when" — so the query terms (`text`, `labels`) are recorded, not just that a search happened. The recorded `Outcome` reflects **coverage**, not row count: `"partial"` whenever `!result.Coverage.Complete`, so a trail entry can never be misread as proof the whole fleet was actually asked.
- Registered BEFORE `apis.NewNodeProxyApi` in `app.go` so `/api/nodes/search` is matched here rather than falling into the proxy's `/api/nodes/{id}/proxy/...` catch-all — load-bearing regardless of how gorilla orders its own route fallbacks, since the specific literal route is registered first.

## Notes

- `search == nil` yields `500` (`"fleet search is unavailable"`) rather than a panic.
- See `services/fleet_search.go.md` for the scatter-gather, the coverage vocabulary, and why the query is federated rather than the index replicated.
- Live-verified against a real two-node fleet by `tools/fleetbench/bench_w24_search.py` (36/36) — see `tools/fleetbench/README.md`.
