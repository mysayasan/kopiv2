# Module: apps/mymatasan/services/vector_codec.go

## Purpose

The storage codec for fixed-length float32 embedding vectors, shared by the face gallery
(`face_gallery.go`) and appearance search (`appearance_search.go`) so the two features use one
implementation rather than two that drift apart.

## Responsibilities

- **`encodeVectorAtRest(cipher *atrest.Cipher, vec []float32) (string, error)`** — float32
  little-endian bytes → `atrest.Cipher` encrypt (nil-safe, e.g. tests) → base64 (standard,
  `base64.StdEncoding`).
- **`decodeVectorAtRest(cipher *atrest.Cipher, enc string) ([]float32, error)`** — the inverse.
  Errors (`corrupt embedding length %d`) rather than panicking on a byte count that is not a
  whole number of float32s.
- **`cosineSimilarity(a, b []float32) float64`** — normalises rather than assuming unit length
  (the producers do normalise, but a vector that has been through storage, a model swap, or a
  hand-written test is not guaranteed to be). Length mismatch or a zero vector returns `0`
  rather than panicking or comparing the overlap — two different feature spaces have no
  meaningful similarity, and inventing one is how a model swap turns into confident nonsense
  instead of an empty result.

## Why one codec, shared

Both features store fixed-length float32 vectors and both need the same three properties:

- **Portable.** base64 `TEXT` persists identically on sqlite, MariaDB and Postgres. Matching
  never happens in SQL — every consumer loads vectors and compares them in memory — so the
  database's only job is durable, engine-agnostic storage.
- **Encrypted at rest.** The float bytes pass through `infra/atrest` before encoding, so a
  stolen database file yields neither biometric templates nor a searchable index of who was
  where. Deleting the owning row crypto-shreds the vector with it.
- **Little-endian, explicitly.** Not Go's native order, which would make a database written on
  one architecture decode to noise on another.

`face_gallery.go`'s `encodeVector`/`decodeVector` now delegate to `encodeVectorAtRest`/
`decodeVectorAtRest` rather than carrying their own copy of the byte-layout logic — see
`services/face_gallery.go.md`.

## Notes

- Distinct from `services/appearance_search.go`'s wire-form codec
  (`EncodeAppearanceVectorParam`/`DecodeAppearanceVectorParam`), which is deliberately
  **unencrypted** base64url for a vector in flight over the already-authenticated,
  already-encrypted control-plane tunnel — see `services/appearance_search.go.md`. This file's
  codec is for the vector at rest in the database.
- `round4`-style helpers and the appearance-specific scoring maths live in
  `appearance_search.go`, not here — this file is purely the byte codec plus similarity.
- No dedicated test file; covered indirectly by `face_gallery_test.go`'s
  `TestFaceVectorRoundTrip` and `appearance_search_test.go`.
