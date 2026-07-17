# Module: apps/mymatasan/entities/face_embedding.go

## Purpose

Defines `FaceEmbedding`, one faceprint for an enrolled person — a fixed-length numeric vector
computed from one enrollment photo (or captured camera frame) by the face-recognition model. A
person typically has several, spanning angles/lighting: matching uses the **exemplar** strategy (a
live face is matched by its best similarity to *any* of a person's embeddings), which is far more
robust than averaging them into a single prototype.

## Fields

- `Id`, `PersonId` (indexed, required) — the owning `FacePerson`.
- `Vector` — `json:"-"` (never serialized to the API); base64 of the model's raw float32 bytes
  **after** passing through `infra/atrest` encryption. See *Encryption at rest* below.
- `Dim` — vector length (128 for the SFace embedder); recorded so a future model swap can be detected
  rather than silently mismatched against embeddings from a different model.
- `Model` — the embedder identity string that produced this vector (`"opencv-sface-128"`).
- `Source` — `"upload"` or `"camera"`, i.e. how the enrollment image was captured.
- `Quality` — the detector's confidence score for the face this vector was extracted from.
- `CreatedAt`.

## Portability

The vector is stored as a base64 **TEXT** string, not a binary blob, so it persists identically on
every supported SQL engine (sqlite / MariaDB / Postgres) — a Go `string` maps to a `TEXT` column on
all three. Matching never happens in SQL; the face worker loads all embeddings into memory and
compares with cosine similarity (see `apps/mymatasan/ai/yolo_worker.py`'s `_faces_detect`), so the
database's only job is durable, portable storage.

## Encryption at rest

`Vector` holds the base64 of the model's raw float32 bytes **after** passing through
`infra/atrest` (the same AES-256-GCM envelope the recordings/snapshots use), so a stolen database
file does not leak biometric templates. `FaceGalleryService.DeletePerson` deletes these rows first,
which crypto-shreds the person's faceprints (the ciphertext key material for that row is gone once
the row is gone — the export gallery is also rewritten immediately after).

## Notes

- Bootstrap creates this table from the registered entity (`app.go`'s `Entities()`).
- `FaceGalleryService` never echoes `Vector` back to an API caller — see
  `services/face_gallery.go.md` and `apis/faces.go.md`.
