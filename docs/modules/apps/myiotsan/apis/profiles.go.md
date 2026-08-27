# Module: apps/myiotsan/apis/profiles.go

## Purpose

Registers the device-type catalog under `/api/profiles`.

## Responsibilities

- `NewProfilesApi(router, profiles, ingest)` mounts `GET/POST /profiles`,
  `POST /profiles/import` (registered before `/{id}` so the literal path wins),
  `GET /profiles/{id}`, `GET /profiles/{id}/export`, `PUT/DELETE /profiles/{id}`.
- `update`/`remove` both call `ingest.InvalidateProfile(id)` after the database change — an
  edited deadband must take effect on the NEXT message, not the next restart, since
  `services.Ingest` caches each profile's decode bindings.
- `create`/`update` bodies are `services.SaveProfileRequest`; `list` returns the profile
  summaries, `detail` returns a `services.ProfileDetail` (profile + its telemetry keys).
- `export`/`importProfile` (P3) are thin wrappers over `services.ProfileService.Export`/`Import`
  (`services/profile_transfer.go.md`) — a device type is a small declarative document, portable
  between sites the same way mymatasan's `.mmskill` is. `importProfile` body-limits the request
  to 256KB before decoding.

## Notes

- Deleting a builtin profile surfaces `services.ErrProfileBuiltin` as a `400`.
- Importing a profile whose slug already exists is REPORTED as a `400`, never silently
  overwritten — see `services/profile_transfer.go.md` for why a silent overwrite would be data
  corruption dressed as a successful import.

## Why this handler knows about `CommandService`

A profile is where a command's **topic** is declared, so editing one changes which topics are
reserved to the guarded actuation path (`services/commands.go.md`). Create, update, import and
delete therefore call `reservedTopicsChanged()` -> `CommandService.InvalidateTopics()`, alongside the
existing `ingest.InvalidateProfile(id)`. The guard also carries a 30s TTL; this is what makes a
freshly declared command topic protected immediately rather than eventually.
