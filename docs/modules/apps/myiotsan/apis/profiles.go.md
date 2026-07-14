# Module: apps/myiotsan/apis/profiles.go

## Purpose

Registers the device-type catalog under `/api/profiles`.

## Responsibilities

- `NewProfilesApi(router, profiles, ingest)` mounts `GET/POST /profiles`,
  `GET/PUT/DELETE /profiles/{id}`.
- `update`/`remove` both call `ingest.InvalidateProfile(id)` after the database change — an
  edited deadband must take effect on the NEXT message, not the next restart, since
  `services.Ingest` caches each profile's decode bindings.
- `create`/`update` bodies are `services.SaveProfileRequest`; `list` returns the profile
  summaries, `detail` returns a `services.ProfileDetail` (profile + its telemetry keys).

## Notes

- Deleting a builtin profile surfaces `services.ErrProfileBuiltin` as a `400`.
