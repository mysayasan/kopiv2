# Module: apps/mymatasan/apis/faces.go

## Purpose

`facesApi` is the enrollment/roster surface for face recognition: manage the people the system
should recognize and their faceprints. Recognition itself is configured as a per-camera detection
rule (`detectionType: "face"`) through the normal `/api/vision/rules` API — this file is only the
global gallery (people + embeddings).

**Face templates are biometric data.** Every route here is admin-only, enforced the same way as the
rest of the admin surface: `NewRequireRolePermission` denies by default and no role but the built-in
superadmin bypass is granted `/api/faces` in the RBAC policy (`services/rbac.go`), so a `viewer`/
`operator` account gets `403` on every route below.

## Routes

| Method   | Path                        | Description |
|----------|-----------------------------|-------------|
| `GET`    | `/api/faces`                | List enrolled people. |
| `POST`   | `/api/faces`                | Create a person (`{name, notes, enabled}`). |
| `PUT`    | `/api/faces/{id}`           | Update a person's name/notes/enabled. |
| `DELETE` | `/api/faces/{id}`           | Delete a person — crypto-shreds their embeddings (see `services/face_gallery.go.md`). |
| `POST`   | `/api/faces/{id}/enroll`    | Enroll a faceprint from a base64 image (`{image, source}`). |
| `GET`    | `/api/faces/{id}/embeddings`| List a person's embeddings (metadata only — no vector bytes). |
| `DELETE` | `/api/faces/embeddings/{id}`| Delete one embedding. |

## Enrollment body

`POST /api/faces/{id}/enroll` accepts `{"image": "<base64 JPEG, optionally a data: URL>", "source":
"upload"|"camera"}` — the **same** endpoint serves an uploaded photo and a snapshot the browser
grabbed from a live camera preview; a `data:image/jpeg;base64,` prefix is stripped if present. A
rejection (no face / multiple faces / face too small) comes back as `400` with the specific
user-actionable reason from `FaceGalleryService.Enroll` — the frontend surfaces it inline so an
admin can retake the photo rather than silently enrolling a bad faceprint.

## Notes

- JSON bodies are decoded with `encoding/json`'s default decoder (no `DisallowUnknownFields`), unlike
  some other app APIs — extra fields are ignored rather than rejected.
- `create`/`update`/`enroll` currently pass actor `0` (the authenticated user id is not yet threaded
  through from request context into this handler) — `FacePerson.CreatedBy`/`UpdatedBy` are `0` for
  every enrollment today; a follow-up should thread the real actor the way other admin APIs do.
- Registered from `wire_routes.go`: `apis.NewFacesApi(protected, w.faceGallery)`, mounted on the same
  `protected` subrouter (auth + RBAC middleware) as every other admin route.
