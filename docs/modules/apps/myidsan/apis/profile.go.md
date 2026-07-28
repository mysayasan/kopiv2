# Module: apps/myidsan/apis/profile.go

## Purpose

The profile-picture surface behind the self-service **Profile** page and the
Users management page's per-row avatar. `profileApi` handles both a caller's
**own** avatar (self-service, auth-only, no RBAC matrix — the same posture as
`/api/mfa` and `/api/login/default/change-password`) and, on a separate,
superadmin-gated subrouter, **any** account's avatar by id, which is what lets a
superadmin set another user's picture from the Users page (`RowAvatar`'s inline
camera button in `App.js`). Both surfaces funnel into the same shared
store/serve/delete logic, keyed only by which user id is being acted on.

## Routes

`NewProfileApi` mounts two subrouters under `/profile`:

Self-service, `auth.Middleware`-protected only, no RBAC matrix — acts on the
caller's own account from the JWT claims (`claims.Id`):

- `GET /api/profile/avatar` (`getOwnAvatar` → `serveAvatar`) — streams the
  caller's own avatar image bytes, or a plain `404` when none is set (the SPA
  renders an initials fallback, not an error).
- `POST /api/profile/avatar` (`putOwnAvatar` → `storeAvatar`) — body
  `{dataUrl}`, a `data:image/<type>;base64,<data>` URL produced client-side by
  `resizeImageToDataUrl` (256px square JPEG). Returns `{ok, version}` — `version`
  is the new `UpdatedAt` unix timestamp, used by the SPA to cache-bust its
  `<img>` tag.
- `DELETE /api/profile/avatar` (`deleteOwnAvatar` → `removeAvatar`) — removes the
  caller's own avatar row, if any (a no-op, not an error, when none exists).

Admin, `auth.Middleware` + `access.Middleware` + `access.RequireSuperadmin` — the
same privilege tier as the rest of user-account management:

- `GET /api/profile/avatar/{userId}` (`adminGetAvatar`)
- `POST /api/profile/avatar/{userId}` (`adminPutAvatar`)
- `DELETE /api/profile/avatar/{userId}` (`adminDeleteAvatar`)

All three admin routes validate `{userId}` (`avatarUserId`, `> 0`) and then
delegate to the identical `serveAvatar`/`storeAvatar`/`removeAvatar` helpers the
self-service routes use, just with an admin-supplied `userId` instead of the
caller's own.

## Validation

- `parseImageDataURL` requires a `data:` URL with a `;base64,` marker and a MIME
  type on the server's allow-list (`allowedAvatarTypes`: `image/jpeg`,
  `image/png`, `image/webp`, `image/gif`); anything else is
  `ErrBadRequest`/`errUnsupportedImage`.
- The decoded payload must be non-empty and at most `maxAvatarBytes` (512 KB) —
  generous headroom over what the client's own 256px-JPEG resize actually
  produces, not a target size.
- The JSON body itself is capped at 1 MB (`http.MaxBytesReader`) to account for
  base64's ~4/3 inflation of the byte cap.
- `serveAvatar` re-checks `ContentType` against the same allow-list before
  setting the response `Content-Type`, falling back to
  `application/octet-stream` for a row written before the allow-list existed
  (defense in depth against a stored value that predates or bypassed
  validation).

## Notes

- No server-side image codec is involved anywhere in this file — resizing is
  entirely client-side (`resizeImageToDataUrl` in `App.js`), which keeps the
  build dependency-light and air-gap safe (no CDN, no native image library).
- Responses set `Cache-Control: private, no-cache` (never a shared cache) so a
  fresh upload is picked up immediately; the client's own `?v=<version>` query
  param is the actual cache-buster for the browser's local cache.
- Constructed in `apps/myidsan/app/app.go` as
  `apis.NewProfileApi(api, *deps.Auth, deps.Access, dbsql.NewGenericRepo[myidsanentities.UserAvatar](deps.Db))`
  — a bare generic repo, no dedicated service layer (see
  `entities/user_avatar.go.md`).
