# Module: apps/mymatasan/services/sighting_search.go

## Purpose

`SightingSearch` is the node's half of federated cross-node search (flagship hardening
W2-4, finding F-10): it answers one node's share of a fleet-wide "where was this seen"
query, over the control channel, for both object-class sightings and recognized
plates/faces.

## Responsibilities

- `NewSightingSearch(observations *ObservationService, alerts dbsql.IGenericRepo[entities.AlertEvent], cameras dbsql.IGenericRepo[entities.Camera])` — any dependency may be `nil` in a test exercising only one half; a `nil` dependency yields no hits of that kind rather than an error.
- `SearchObjects(ctx, SightingQuery) (SightingPage, error)` — object-class presence intervals (reusing `ObservationService`'s repo, footage-segment resolution, and camera-recording state), newest first, capped at `Limit` (default 200, max 1000; one extra row is fetched to detect the cap without a second count query). Joins camera names in so a federated result identified only by `cameraId` — meaningless across a fleet where "camera 3" differs per node — is readable without the caller issuing a second tunneled request. Unlike the node's own Objects grid (`ObservationService.GetObservations`), it does **not** hide sightings that have no playable footage: a fleet search answers "was it seen", not "can I watch it", and a detect-only camera at a remote gate is often the only witness. Footage state is reported per row instead (`SegmentId`/`SeekSeconds` when playable, `FootagePending: true` when the sighting is newer than the camera's newest saved segment **and** the camera records at all — a detect-only camera's sightings are never marked pending, since no clip is ever coming).
- `SearchIdentities(ctx, SightingQuery) (SightingPage, error)` — recognized plates and faces, which live on `AlertEvent` rows (in `Metadata.plate`/`Metadata.personName`), not the observation index, since the metadata recorder only coalesces object *classes*. Diagnostic alerts are excluded at the query. Because the repository layer has no `LIKE`, the `Text` substring match (against the identity, the alert's rendered `Label`, and — for a plate — its color/vehicleType) is done in memory over a bounded, paged scan (`identityScanPageSize=500` rows/page, `identityScanPages=40` max) with the requested `Limit` cap checked as it walks the pages. Reaching either bound sets `Capped: true` — the same contract as `SearchObjects`, never guessed as "no more matches". An alert with an unrecognized face (no `personName`) is deliberately not an identity hit — matching nothing an operator ever typed and making a fruitless search look like it found someone.
- `Labels(ctx) ([]string, error)` — delegates to `ObservationService.Labels(ctx, 0)` so the node's own label picker and the fleet-wide label picker are populated from exactly the same scan.
- `SightingHit` — one unified row shape (`Kind: "object"|"identity"`) for both search kinds, so a fleet caller merging results from many nodes never has to interleave two differently-shaped lists.
- `SightingPage.Capped`/`Oldest` — every answer declares whether it returned a prefix of the truth (`Capped`) and, if so, how far back that prefix reaches (`Oldest`, the earliest `StartedAt` in `Items`). The control plane's coverage block is built on this contract.

## Notes

- Read through `ObservationService`, not a parallel query path: a sighting found by a fleet search resolves to the same footage segment the node's own Objects page would open for it.
- `identityHit`/`alertIdentity` are the classification core: an alert carries an identity only when its `Metadata` JSON has a non-empty `plate` or `personName`; `IdentityKinds` (`plate`/`face`) and an empty/unrecognized request default to *both*, so a typo in the query parameter never silently narrows an investigative search to nothing.
- `cameraNames(ctx)` reads up to 1000 cameras; a read failure yields an empty map (fall back to the id) rather than failing the whole search over a cosmetic join.
- Wired in `app.go` as `sightingSearch`, built from `observationService`, `repo.AlertEvent`, `repo.Camera`; exposed at `GET /api/observations/search` (`apis/observation.go.md`) and `GET /api/vision/alerts/identities` (`apis/vision.go.md`) — deliberately two different route prefixes so the two halves stay governed by their existing page grants (Objects vs. AI log) rather than a new, separately-grantable path.
- Covered by `sighting_search_test.go`.
