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
| `GET`    | `/api/faces`                | List enrolled people, each with a `photos` faceprint count (`services.PersonSummary`). |
| `POST`   | `/api/faces`                | Create a person (`{name, notes, enabled}`). |
| `PUT`    | `/api/faces/{id}`           | Update a person's name/notes/enabled. |
| `DELETE` | `/api/faces/{id}`           | Delete a person — crypto-shreds their embeddings (see `services/face_gallery.go.md`). |
| `POST`   | `/api/faces/{id}/enroll`    | Enroll a faceprint from a base64 image (`{image, source}`). |
| `GET`    | `/api/faces/{id}/embeddings`| List a person's embeddings (metadata only — no vector bytes). |
| `DELETE` | `/api/faces/embeddings/{id}`| Delete one embedding. |
| `GET`    | `/api/faces/models`         | What face recognition still needs on this host (models, worker script, opencv). |
| `POST`   | `/api/faces/models/install` | Start the background download/install of the missing pieces. |
| `GET`    | `/api/faces/models/install/status` | Poll the install job (running/done/failed + live log). |
| `GET`    | `/api/faces/sightings`      | The most recent recognition per person, from the alert log. |

The `models` routes are registered **before** `/{id}` so `models` is never parsed as a person id.
They are the in-app answer to "face models are not installed" — see
`services/face_models_install.go.md`. `StartInstall` failures ("already running", "the model
directory is not writable") come back as `400` with the reason, because both are the caller's
business.

## Why `GET /api/faces` carries a count

A person with zero embeddings is skipped by the gallery export, so they are recognized by nothing.
The roster screen states that per person, and it can only do so if the list endpoint says how many
faceprints each person has — hence `ListPeopleWithCounts` rather than `ListPeople` here.

## Why the roster needs `/sightings`

Enrolling a person and switching a camera on produces alerts, event clips and notifications — all of
which land on OTHER screens (Notifications, the alert log, the timeline). From the People page the
feature was indistinguishable from one that does nothing, which is how somebody ends up asking what
it is for. This route answers "has any of this ever worked, and when", per person, so each card can
say *seen 4 minutes ago on Lobby Entrance*.

It reads the alert log rather than keeping a tally: the alert IS the record, and a second counter
would be a second thing to keep true. It scans a bounded window of recent `face` alerts newest-first
(`?scan=`, default 500, max 2000) and keeps the first row per `personId` — so a person seen once a
year is absent, which is the right trade for a line that means "recently seen". `unknownAt` /
`unknownCount` report UNRECOGNIZED faces separately: they belong to nobody so they cannot sit on a
card, but "strangers are being seen and none of them are enrolled" is exactly what a screen showing
only known people would otherwise hide.

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
