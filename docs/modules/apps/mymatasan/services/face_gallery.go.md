# Module: apps/mymatasan/services/face_gallery.go

## Purpose

`FaceGalleryService` owns the **global** face gallery: the enrolled people and their faceprints,
plus the file the live face worker reads to recognize them. It is the face analog of the anomaly
bank, but **labelled** (one gallery entry per person) and **global** rather than per-camera — enroll
once, recognized on every camera that has face recognition switched on.

Matching does **not** happen here or in SQL: this service persists encrypted embeddings and exports a
plaintext gallery file (`faces_gallery.json`) that the Python worker loads into memory and compares
with cosine similarity. The database's only job is durable, portable storage — see
`entities/face_embedding.go.md` for the encryption/portability rationale.

## Responsibilities

- **People CRUD**: `ListPeople`, `CreatePerson(name, notes, actor)`, `UpdatePerson(id, name, notes,
  enabled, actor)`, `DeletePerson(id)`.
- **Enrollment**: `Enroll(ctx, personId, imageJPEG, source, actor)` calls the `FaceEmbedder` seam,
  applies `pickEnrollFace` to enforce exactly one clear, large-enough face, encrypts+stores the
  vector, sets the person's roster `Thumbnail` from the first enrolled face if unset, and rebuilds
  the gallery file. Never echoes the stored ciphertext back to the caller (`row.Vector = ""` before
  return).
- **Embeddings**: `ListEmbeddings(personId)` (metadata only — `Vector` stripped before return),
  `DeleteEmbedding(id)` (rebuilds the gallery after delete).
- **`FaceEmbedder` interface** — the one model-dependent seam: `Embed(ctx, imageJPEG)
  ([]DetectedFace, error)` + `Model() string`. Production runs `pythonFaceEmbedder`
  (`face_embedder.go.md`); tests substitute a fake.
- **`pickEnrollFace(faces []DetectedFace) (DetectedFace, error)`** — pure, unit-tested — enforces
  "exactly one clear, large face" for enrollment: `ErrNoFace` (zero faces or quality below
  `faceMinEnrollQuality` = 0.6), `ErrMultipleFaces` (more than one face), `ErrFaceTooSmall` (box width
  fraction below `faceMinBoxFraction` = 0.02). A bad enrollment silently poisons every future match,
  so it errors loudly rather than guessing.
- **`rebuildGallery(ctx)`** — writes the plaintext gallery file the live worker loads
  (`{model, persons:[{id, name, embeddings:[[float]]}]}`), including only **enabled** people with at
  least one embedding, then calls `reload()` to restart the detector so it re-reads the file. Written
  atomically (temp file + rename) so the worker never reads a half-written file.
- **`encodeVector`/`decodeVector`** — float32 little-endian bytes → `atrest.Cipher`
  encrypt/decrypt (nil-safe, e.g. tests) → base64 (portable TEXT), and the inverse.
- `DeletePerson` **crypto-shreds** the person: their embeddings are deleted first (the ciphertext
  becomes unrecoverable once the rows are gone), then the person row, then the gallery is rebuilt so
  the live worker immediately forgets them.

## Constructor

`NewFaceGalleryService(persons, embeddings, cipher *atrest.Cipher, embedder FaceEmbedder,
galleryPath string, reload func(), logf func(string, ...any)) *FaceGalleryService` — `reload`/`logf`
default to no-ops when nil. `galleryPath` is the worker-readable export file; when empty,
`rebuildGallery` is a no-op (feature effectively dormant).

## Notes

- Face templates are **biometric data** (GDPR Art. 9 / BIPA weight). The feature is off by default,
  every `/api/faces` route is admin-only, and deleting a person crypto-shreds their embeddings.
- `TestFaceVectorRoundTrip` (`face_gallery_test.go`) proves an embedding survives
  encode→store→decode unchanged with no cipher. `TestPickEnrollFace` proves the zero/multiple/
  low-quality/tiny-face rejections above are exhaustive and correct, using table-driven fakes — no
  I/O, no real model.
- This service has no dependency on `infra/vision`; the rule-side policy (which recognized/unknown
  faces fire a rule) lives in `infra/vision/face.go`, which only reads the `personId`/`personName`/
  `confidence` metadata the worker attaches to a `"face"` candidate — see
  `docs/modules/infra/vision/face.go.md`.
