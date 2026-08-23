# Module: apps/myseliasan/services/fleet_appearance.go

## Purpose

`FleetSearchService.AppearanceSearch` answers a fleet-wide "where else in the estate did this
go?" (W3-2): an operator picks one sighting on one node, and the control plane asks every
other node they can reach whether it saw anything that looks like it, merging the ranked
answers.

## Two hops, not one

The operator names a sighting on one node (`SourceNodeId`/`SourceObservationId`). That id
means nothing anywhere else, so `AppearanceSearch` first fetches the **descriptor** from the
node that holds it (`fetchQueryVector`, hop one), then fans the descriptor out to every
target node (hop two). A federated search that skipped the first hop and passed the
observation id around would return results only from the node that happened to record the
sighting, and would look like it worked while quietly doing nothing on every other node.

Hop one is resolved against the **same** access rules as the search itself
(`s.targets(ctx, roleId, FleetSearchQuery{NodeId: q.SourceNodeId})`): resolving the query
through a node the caller cannot reach would let them search by a sighting they are not
allowed to see, which is a read they don't have dressed up as a query. If the source node
cannot be reached or answers that the sighting has no descriptor, the whole search is a hard
stop (an error, not an empty result) — there is no question to ask the rest of the fleet, and
answering "no matches" would say the estate never saw anything like it.

## The query travels, not the index

The same call `services/fleet_search.go.md` (W2-4) already makes, and here it matters even
more: replicating appearance descriptors upward would move a two-kilobyte vector per
appearance-eligible sighting into the control plane — the estate's entire detection volume
again, in a form that only grows. Each node already holds both the vectors and the footage a
hit must link to, and the control plane already has an authenticated, authorized, audited
transport to it.

## Responsibilities

- `AppearanceSearch(ctx, roleId int64, FleetAppearanceQuery) (FleetAppearanceResult, error)` —
  hop one (`fetchQueryVector`), then resolves targets exactly like `Search` (`targets`, same
  `SiteId`/`NodeId` scoping, same per-node live-access re-check), then fans out with the same
  bounded parallelism (`fleetSearchConcurrency`) and whole-search budget
  (`s.searchBudget()`) as object search. The node that owns the sighting is asked to
  **exclude** it (`&excludeObservationId=`) so the operator's own pick never comes back as its
  own best match at `1.00`.
- `fetchQueryVector(ctx, target, observationId) ([]float32URL, model, label string, error)` —
  GETs `/api/observations/appearance/vector?observationId=` on the source node
  (`fleetAppearanceNodePathVector`) via the control tunnel. A non-`200` or an empty
  vector/model is reported as `"that sighting has no appearance description on <node>"` — hop
  one has no partial-success shape, unlike hop two.
- `askNodeAppearance(ctx, target, path)` — GETs `/api/observations/appearance` on one target
  node (`fleetAppearanceNodePathSearch`), classifying failures into the same coverage
  vocabulary object search uses (`classifySearchFailure`) so `ok`/`offline`/`timeout`/
  `denied`/`unsupported`/`error` mean the same thing across both searches.
- `appearanceNodeQuery(vector, model, label, q)` — builds the per-node query string
  (`vector`, `model`, `label`, `from`, `to`, `minStandout`, `limit`). `vector` is carried as
  `float32URL` — the already-wire-encoded string from `fetchQueryVector` — passed through
  rather than decoded and re-encoded on this side: the control plane has no reason to look
  inside a descriptor, and a decode/re-encode round trip is a place for the bytes to change
  without anything noticing.
- Merging: ranked by **`Standout`**, not raw `Similarity` — each node calibrates against its
  own candidate set, so "0.97 at the depot" and "0.97 at the gate" are not comparable
  quantities, but how far each stands out from its own crowd is. Ties break on confidence,
  then newest, then node id — deterministic ordering across repeated runs of the same search.
  Clamped to `Limit` (falls back to `fleetSearchMaxLimit` when unset or over it), `Truncated`
  set if it had to cut.
- `out.Scanned` sums every node's `Scanned` — the same "40,000 compared vs. 3 compared" honesty
  `AppearanceService.Search` reports node-locally (`services/appearance_search.go.md`),
  carried up so an empty fleet-wide result can be told apart from a fleet nobody had much to
  compare against.

## Types

- `FleetAppearanceQuery` — `SourceNodeId`/`SourceObservationId` (required), `From`/`To`,
  `SiteId`/`NodeId` (narrows which nodes are *asked*, independent of which node the sighting
  came from), `MinStandout`, `Limit`.
- `FleetAppearanceHit` — a node-local `AppearanceHit` (`services/appearance_search.go.md`)
  qualified with `NodeId`/`NodeName`/`SiteId`/`SiteName`.
- `FleetAppearanceResult` — `Items`, `Total`, `Coverage` (`SearchCoverage`, same struct/vocabulary
  as `fleet_search.go`), `Scanned`, `Truncated`, `Model`/`Label` (echoed from hop one, so a node
  running a different embedder is seen to have contributed nothing rather than merely appearing
  to have found no matches), `MinStandout`.

## Notes

- Wired in `app.go` on the same `fleetSearchService` object `FleetSearchService.Search` lives
  on — no separate constructor; `AppearanceSearch` is a second method on the existing service so
  it reuses `targets`, `tunnel`, `searchBudget`, and `classifySearchFailure` verbatim rather than
  duplicating the scatter-gather machinery.
- Exposed at `GET /api/nodes/search/appearance` — see `apis/fleet_search_api.go.md`.
- Node-side counterpart: `apps/mymatasan/apis/observation.go.md`'s `/observations/appearance`
  and `/observations/appearance/vector`.
