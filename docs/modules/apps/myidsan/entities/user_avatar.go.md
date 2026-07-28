# Module: apps/myidsan/entities/user_avatar.go

## Purpose

Entity for a user's uploaded profile picture, one row per account, backing the
self-service **Profile** page's photo and the Users management page's per-row
avatar. The client resizes/crops the source image to a 256px square JPEG
(`canvas`, in `App.js`'s `resizeImageToDataUrl`) before it is ever uploaded, so
the server never touches an image codec and the stored payload is small (tens of
KB) — that is why it is kept inline as base64 in a plain `TEXT` column rather than
routed through file storage.

It is deliberately **not** carried in the JWT/session claims (which would blow
the cookie size on every request); the SPA fetches it separately from
`GET /api/profile/avatar` (own) or `GET /api/profile/avatar/{userId}` (admin), and
a missing row is a normal `404` that the SPA renders as an initials fallback, not
an error.

## Fields

| Field | Notes |
|---|---|
| `Id` | Primary key. |
| `UserLoginId` | FK to the owning account; `ukey1` — exactly one avatar row per user, upserted in place on re-upload. |
| `ContentType` | Stored image's MIME type (`image/jpeg`, `image/png`, `image/webp`, `image/gif` — the upload allow-list in `apis/profile.go`). |
| `DataB64` | Base64-encoded image bytes. `json:"-"` — never serialized into a JSON API response; the image is only ever streamed as raw bytes by `serveAvatar`, not returned inline in a JSON payload. |
| `UpdatedAt` | Unix timestamp of the last upload; also returned as `version` from a successful upload so the client can cache-bust its own `<img>` tag. |

## Notes

- Registered in `apps/myidsan/app/app.go`'s `Entities()` for bootstrap schema
  generation, alongside `UserMfaFactor`/`UserMfaRecoveryCode`/`PasswordResetRequest`.
- Owned exclusively by `apps/myidsan/apis/profile.go` (`profileApi`) via a plain
  `dbsql.IGenericRepo[UserAvatar]` — there is no separate service layer for
  avatars, unlike MFA/password-reset, since the logic is a small amount of
  upload-validate-store/serve/delete keyed by user id with no cross-entity
  business rules.
- `maxAvatarBytes` (512 KB, in `apis/profile.go`) caps the **decoded** size as a
  generous safety bound, not a target — the client-side resize keeps a real
  upload far smaller.
