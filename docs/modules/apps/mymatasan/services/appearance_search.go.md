# Module: apps/mymatasan/services/appearance_search.go

## Purpose

`AppearanceService` stores and ranks appearance descriptors (W3-2, "find more like this"): an
operator watching a person walk out of shot picks the sighting, and this ranks every other
recorded sighting by how much it *looks like* that one.

## What this is and is not

The descriptor is a general-purpose ImageNet embedding of the crop, produced by the resnet18
the anomaly feature already installs (`_crop_backbone` in `apps/mymatasan/ai/yolo_worker.py.md`
— shared deliberately so appearance search needs no additional model download, which matters
on a product deployed into networks with no egress). It separates coarse appearance well (a
red jacket from a black one, a white van from a blue hatchback) and is markedly weaker than a
purpose-trained re-identification network at matching the SAME person across large changes in
pose, lighting or camera. So this returns a **ranked shortlist for a human to confirm** and
never asserts that two sightings are the same individual — the API and the UI say so too.
Every row records the model that produced it so a better one can replace this without old
vectors being silently compared against new ones.

## How this is scored, and why it is not a percentage

Cosine similarity over these embeddings does not span a usable range. Measured on the real
model: two crops of the SAME subject score `0.9825`, and two crops of OBVIOUSLY DIFFERENT
subjects score `0.9498`. ImageNet features are dominated by structure (a person-shaped thing
against a dark background) and discard most of what an operator means by "the man in the red
jacket". So the raw number is fine for **ordering** and worthless as a **verdict** — an
absolute floor filters nothing, and a screen reporting "95% match" between two unrelated
people is not a weak feature, it is a wrong answer delivered confidently.

What is meaningful is how far a candidate **stands out from the others that were compared**.
`Standout` is a robust z-score — deviations above the **median**, scaled by the median
absolute deviation (`madToSigma = 1.4826`) — computed by `similarityCentreAndSpread`/
`medianOf`. Median/MAD rather than mean/standard-deviation because the thing being looked for
is an outlier: if a person walked past six cameras, six near-duplicates of the query are in
the set, and a mean would climb towards them (flattening the very matches the search exists to
surface) while a median does not. Below `appearanceMinCalibrationSample` (12) comparisons,
`Calibrated` is `false` and hits are ranked by raw similarity instead, with the API/UI told so
rather than dressed up as a statistical finding.

## Responsibilities

- `NewAppearanceService(repo dbsql.IGenericRepo[entities.ObjectAppearance], cipher *atrest.Cipher) *AppearanceService`.
- `Store(ctx, AppearanceRecord) error` — persists one descriptor for a written observation. A
  missing vector or model is not an error (most observations legitimately have nothing to
  store — the appearance stage skips crops that are too small or too uncertain); a `nil`
  service/repo also no-ops, so an install without appearance search pays nothing to call it.
- `VectorFor(ctx, observationId) ([]float32, model, label string, error)` — the stored
  descriptor for one observation, so "find more like this sighting" can be asked by id rather
  than by uploading an image. Backs both the local search's `observationId=` query form and the
  federation vector endpoint.
- `Search(ctx, AppearanceQuery) (AppearanceResult, error)` — ranks stored descriptors against
  `q.Vector`, filtered to `q.Label` and `q.Model` (both required — a person and a car are both
  points in the same feature space, and ranking without a label filter is a confident answer to
  a question nobody asked) and optionally `q.From`/`q.To`/`q.CameraIds`. Every candidate is
  scored **before** anything is dropped (the threshold is relative to the distribution, so the
  distribution must be known first — filtering as it goes would calibrate against the
  survivors, circularly). Hits below `q.MinStandout` (default `appearanceDefaultMinStandout` =
  2.0, deliberately permissive — hiding a real match to keep the list tidy is the more
  expensive mistake) are dropped once calibrated; sorted by similarity, then confidence, then
  newest, for a deterministic order across repeated runs. Capped to `q.Limit` (default 50, max
  200).
- `ErrAppearanceRangeTooWide` — returned (never silently truncated) when a window holds more
  than `appearanceMaxScan` (50000) candidate descriptors. Rows are read newest-first, so a
  truncated scan would drop the *oldest* candidates — and the match an investigator is looking
  for is usually the older one — producing a confident shortlist with the answer missing from
  it and nothing on screen able to say so. The API maps this to a `400` (the window is
  answerable, just not this wide) rather than a `500`.
- `DeleteForObservations(ctx, observationIds []int64) (int, error)` / `DeleteForCamera(ctx,
  cameraId) (int, error)` — the `AppearanceReaper` seam `ObservationService` calls on the
  retention sweep and the per-camera purge (`services/observation.go.md`). A descriptor that
  outlives the sighting it describes is both a storage leak and a retention breach.
- `EncodeAppearanceVectorParam([]float32) string` / `DecodeAppearanceVectorParam(string)
  ([]float32, error)` — the **wire** form of a query vector (unpadded base64url of the raw
  float32 little-endian bytes, unencrypted — this travels over the already-authenticated,
  already-encrypted control-plane tunnel, not at rest), used when the control plane fans one
  node's sighting out to every other node (`myseliasan`'s `services/fleet_appearance.go.md`).
  Unpadded base64url specifically because a query parameter carrying `+`, `/` or `=` is one
  mis-escaped hop from decoding to a different, still-valid-looking vector.

## Types

- `AppearanceHit` — one ranked sighting: `ObservationId`, `CameraId`, `SeenAt`, `Label`,
  `Similarity` (raw cosine — reported, never presented as a percentage), `Standout` (the number
  the screen bands on), `Confidence`, and `SegmentId`/`Seek` — left **zero** by this service
  (no footage dependency in ranking) and filled in by the API layer via
  `ObservationService.ResolveFootageFor`.
- `AppearanceQuery` — `Vector`, `Model`, `Label` (required), `From`/`To`, `CameraIds`,
  `MinStandout`, `Limit`, `ExcludeObservationId` (the sighting the query came from, so it never
  ranks against itself at `1.00`).
- `AppearanceResult` — `Hits`, `Scanned` (how many stored descriptors were actually compared —
  "no matches out of 40,000" and "no matches out of 3" are different answers a ranked list
  alone cannot distinguish), `Model`, `MinStandout`, `Median`/`Spread` (the distribution
  `Standout` is measured against, reported so a result can be audited), `Calibrated`, `From`,
  `To`, `Label`.
- `AppearanceRecord` — one descriptor to persist, as `MetadataRecorder` produces it:
  `ObservationId`, `CameraId`, `SeenAt`, `Label`, `Confidence`, `Vector`, `Model`.

## Notes

- Vectors are stored/compared through `encodeVectorAtRest`/`decodeVectorAtRest`/
  `cosineSimilarity` in `vector_codec.go` (`services/vector_codec.go.md`), shared with the face
  gallery. A stored vector whose `Dim` disagrees with the query length is skipped rather than
  compared, and one row failing to decrypt (e.g. a rotated key) is skipped and logged into a
  smaller `Scanned` rather than failing the whole search.
- Wired in `app.go` as `appearanceService`, built from `repo.ObjectAppearance` and
  `atrestCipher`; then wired into `metadataRecorder.SetAppearanceStore` (write side) and
  `observationService.SetAppearanceReaper` (purge side) — after construction, because both need
  the at-rest cipher which is built later in the wiring than either service is.
- Exposed at `GET /api/observations/appearance` and `GET /api/observations/appearance/vector`
  (`apis/observation.go.md`), and federated by `myseliasan` at `GET /api/nodes/search/appearance`
  (`myseliasan`'s `services/fleet_appearance.go.md` / `apis/fleet_search_api.go.md`).
- Covered by `appearance_search_test.go`: ranking order, cross-model/cross-dimension refusal,
  label scoping, self-exclusion, the standout scoring (including the "not dragged up by
  near-duplicates" and "caller's row ordering doesn't leak into the median" cases), the
  too-few-to-calibrate path, window/camera filtering, the two `ErrAppearanceRangeTooWide` paths
  (truncated page and reported total), store/search round-trip, both purge paths, and the wire
  codec (round-trip and malformed-payload rejection).
