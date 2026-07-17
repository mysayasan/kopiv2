# Module: apps/mymatasan/entities/face_person.go

## Purpose

Defines `FacePerson`, one enrolled identity in the **global** face gallery — a named person the
system should recognize when their face appears on any camera that has face recognition enabled.
Unlike a `TeachSkill` (per-camera), a person is enrolled once and recognized everywhere.

## Fields

- Identity: `Id`, `Name` (required), `Notes`.
- `Enabled` — lets an admin pause a person (excluded from the live gallery export) without deleting
  their enrollment/embeddings.
- `Thumbnail` — a small base64 JPEG of a representative face, for the roster UI only; never used for
  matching.
- Audit fields: `CreatedBy`, `CreatedAt`, `UpdatedBy`, `UpdatedAt`.

## Notes

- A person carries **no biometric data itself**; faceprints live in `FaceEmbedding` rows keyed by
  `PersonId` (see `face_embedding.go.md`).
- **Face templates are biometric data** (GDPR Art. 9 / BIPA weight). The feature is off by default
  and every `/api/faces` route is admin-only; enrollment is a deliberate act gated behind a consent
  acknowledgment in the UI, and deleting a person crypto-shreds their embeddings — see
  `services/face_gallery.go.md`.
- Bootstrap creates this table from the registered entity (`app.go`'s `Entities()`) when SQLite or
  another supported DB engine starts.
