# Module: apps/mymatasan/entities/object_appearance.go

## Purpose

Declares `ObjectAppearance`: one appearance descriptor for one recorded sighting (W3-2,
"find more like this"). A fixed-length numeric vector describing what a person or vehicle
looked like at the clearest (peak-confidence) moment of an `ObjectObservation` interval — one
vector per presence interval, not per frame.

## Fields

| Field           | Type    | Notes |
|-----------------|---------|-------|
| `Id`            | int64   | Auto-increment primary key. |
| `ObservationId` | int64   | The `ObjectObservation` this descriptor belongs to (`idx:"observation"`), so the observation's own delete/purge path can find and shred it. |
| `CameraId`      | int64   | Denormalised from the observation (`idx:"cam_time"` with `SeenAt`) — a ranked search scans every candidate in a window and scores it in memory, so avoiding a join back to the observation index per row turns one indexed range scan into a scan plus N lookups. |
| `SeenAt`        | int64   | Unix seconds of the peak frame (`idx:"cam_time"`). |
| `Label`         | string  | Lowercased object class (`person`, `car`, ...). Ranking is always scoped to one label — a person and a car are both points in the same feature space and will return a similarity score for each other, a confident answer to a question nobody asked. |
| `Vector`        | string  | `json:"-"` (never serialized to the API); `base64(atrest-encrypted(float32-le bytes))` — see *Portability and encryption at rest* below. |
| `Dim`           | int     | Vector length. Every stored candidate must match the query's length to be compared; a mismatch is skipped rather than compared. |
| `Model`         | string  | The embedder identity string that produced this vector (`"resnet18-imagenet-512"`). Vectors from two different models are **never** compared — cosine similarity across unrelated feature spaces returns plausible-looking numbers with no meaning behind them, which is worse than returning nothing because it looks like a result. Every search filters on `Model`, so swapping the embedder degrades to "no older matches" rather than to silent nonsense. |
| `Confidence`    | float64 | The detector's confidence in the sighting the crop came from, carried so a ranking can prefer a clear view over a marginal one at equal similarity. |
| `CreatedAt`     | int64   | Unix seconds; row write time. |

## Why a separate table rather than a column on `ObjectObservation`

The observation index is the highest-write table the metadata recorder owns and is read
constantly by the search grid, which selects whole rows; hanging a two-kilobyte vector off
every row would make every unrelated query drag it across the wire. Keeping it beside means
appearance can also be purged, or never written at all, without touching the index the rest
of the product reads.

## Portability and encryption at rest

Follows `FaceEmbedding` exactly, for the same reasons (see
`entities/face_embedding.go.md`): `Vector` is base64 **TEXT** — identical on sqlite / MariaDB
/ Postgres, since matching never happens in SQL — holding the model's raw float32 bytes
**after** passing through `infra/atrest`, so a stolen database file does not hand over a
searchable index of who was where. The codec is shared with `FaceEmbedding`'s, factored into
`services/vector_codec.go.md`.

A vector is not biometric identification the way a faceprint is — it describes clothing,
build and colour, not a face — but it is treated with the same at-rest care rather than a
lesser one.

## Notes

- Bootstrap creates this table from the registered entity (`app.go`'s `Entities()`).
- Written by `services.MetadataRecorder` (via the `AppearanceStore` seam) after the
  observation row it describes; deleted by `services.ObservationService` (via the
  `AppearanceReaper` seam) *before* the observation row it describes, on both the retention
  sweep and the per-camera purge — see `services/appearance_search.go.md`.
