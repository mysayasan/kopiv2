# Module: apps/myseliasan/services/fleet_search.go

## Purpose

`FleetSearchService` answers a fleet-wide "where was this seen" search by scatter-gathering
one tunneled request per adopted node the caller's role can reach — federated cross-node
search, flagship hardening W2-4 / finding F-10.

## Why federated, not replicated

The alternative was to copy each node's observation index into the control plane and search
it locally. That index is the highest-volume derived data in the product — one row per
label-presence span per camera — so replicating it would make the control-plane database
grow with the whole estate's detection volume, need a write queue on every appliance, its
own retention policy, and a backfill for anything written while a link was down. The node
already holds both the index and the footage the answer must link to, and the control plane
already has an authenticated, authorized, audited transport to it (the control channel). So
the query travels instead of the data.

That choice has one real cost: an offline node contributes nothing, and reporting that
honestly — never silently — is the point of the feature. An investigator shown an empty
result must be able to tell "the fleet never saw it" from "the depot recorder has been
unreachable for a week"; replication would have papered over exactly that distinction with
stale data, which is worse, since it answers confidently and wrongly. Every result carries a
`Coverage` block saying which nodes answered, which did not, and why.

## Responsibilities

- `NewFleetSearchService(nodes searchNodeLister, sites searchSiteNamer, sender ControlSender, access INodeAccessService)` — `sites` may be `nil` (site names are then omitted from results, degrading a label, not the answer). `nodes`/`access` are narrowed interfaces (`List`, `Resolve`) for the same reason the policy reconciler narrows its own: a read-only feature holding the adoption/revocation surface is one refactor away from being able to release a node.
- `Search(ctx, roleId, FleetSearchQuery) (FleetSearchResult, error)` — resolves the caller's reachable, sighting-capable nodes (`targets`), then fans out over both sources (`objects`/`identities`, selectable via `Sources`, both by default) with bounded parallelism (`fleetSearchConcurrency = 8`), a per-node deadline (`fleetSearchNodeTimeout = 15s`) and a whole-search budget (`fleetSearchBudget = 45s`, held on the service so tests can shrink it). The budget matters precisely when the caller has no deadline of its own, which is the normal case — a browser waiting on the endpoint imposes none — and without it the worst case is the per-node timeout multiplied by the number of batches: a fifty-node estate of wedged appliances would hold the request open for minutes and then answer. Nodes the budget ran out before reaching are reported as `timeout`, the same contract as every other failure here. Merges every node's hits newest-first (ties broken by node id then row id, so re-running the same search never shuffles rows), clamps the merged set to `Limit` (default 200, max 2000) and marks `Truncated` if it had to cut. Every node is asked for the **full** merged limit, never `limit/N` — the fleet's newest N sightings can all come from one busy node, and dividing the budget evenly would drop real hits to make room for rows that do not exist elsewhere.
- `Labels(ctx, roleId, FleetSearchQuery) (FleetLabelsResult, error)` — the fleet-wide union of recorded object labels for the search UI's picker, with the same `Coverage` account: a label list built from four of nine nodes would silently exclude what the other five saw.
- `targets(ctx, roleId, q)` — resolves which nodes a search visits: filtered by `q.NodeId`/`q.SiteId` if set, then by the caller's live per-node access (a node the role cannot reach is not even counted as "skipped" — that count would still disclose it exists), then by `nodeKindHoldsSightings` (only `""`/`"camera"` kinds are asked; an IoT hub or door controller is counted in `SkippedKind` so "5 of 9 nodes" never reads as four failures).
- `askNode`/`askNodeLabels` — tunnel one GET to one node (`GET /api/observations/search`, `GET /api/vision/alerts/identities`, or `GET /api/observations/labels`) via `ControlSender.SendRequest` under the per-node timeout, decode the envelope (`decodeNodeResult` — accepts the plain `{result}` shape and the legacy double-wrapped `{data:{result}}` shape, the same tolerance the agent chat already needs over this tunnel), and classify any failure into the coverage vocabulary (`classifySearchFailure`).
- Coverage vocabulary (`SearchNodeOk`/`Offline`/`Timeout`/`Denied`/`Unsupported`/`Error`) — kept apart because an operator acts on each completely differently: reconnect the node, grant a role on it, upgrade it, or read the error. `rollUpSourceStatus` reduces a node's per-source outcomes to one status, worst-wins — a node with one denied source is reported `denied`, never `ok`, because calling it `ok` is how "this plate was never seen here" gets said about a node whose identity search was actually refused.
- `summarizeCoverage` — counts nodes that `answered` (at least one source came back) and decides `Complete`/`CompleteThrough`. `CompleteThrough` is the **newest** of the capped sources' horizons, not the oldest: each capped source is complete only back to where its own prefix stops, so the fleet's guaranteed-complete horizon is the latest of those stops, not the earliest.
- No offset by design (`FleetSearchQuery` has none): a global offset cannot be honoured across independent per-node result sets — each node pages its own — and the usual over-fetch-then-slice workaround silently drops rows the moment any node caps. Narrow the time window instead; the coverage block says when the window is still too wide.

## Notes

- `FleetSightingHit`'s node-local fields are decoded verbatim from the node's `SightingHit` rather than re-modelled on this side: the node owns the meaning of a sighting, and a second definition here is a second thing to keep in step.
- `nodeKindHoldsSightings("")` is `true` — every node adopted before `ManagedNode.Kind` existed is a mymatasan recorder, matching the field's documented default elsewhere in the suite.
- Covered by `fleet_search_test.go`; live-verified against a real two-node fleet by `tools/fleetbench/bench_w24_search.py` (36/36).
- Wired in `app.go` as `fleetSearchService`, registered via `apis.NewFleetSearchApi` immediately BEFORE `apis.NewNodeProxyApi` — see `apis/fleet_search_api.go.md` and `app.go.md`.
