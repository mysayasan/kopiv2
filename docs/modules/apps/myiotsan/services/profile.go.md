# Module: apps/myiotsan/services/profile.go

## Purpose

Owns device types and the datapoints they report — the abstraction mymatasan does not have, and
the difference between a product and a demo: without it, onboarding a hundred identical door
sensors means configuring a hundred devices by hand; with it, the hundredth device is a name
and a profile.

## Key Type: ProfileService

```go
func NewProfileService(db dbsql.IDbCrud) *ProfileService
```

- `List`, `Detail(id) (*ProfileDetail, error)` (profile + its keys), `KeysFor(profileId)` — the
  latter is on the ingest path (every payload needs its bindings), so it is cached by
  `services.Ingest` rather than read per message.
- `Create(ctx, SaveProfileRequest, actor)` / `Update(...)` — a profile and its keys are saved in
  one call, and keys are **replaced wholesale**, not diffed: a profile is a small declarative
  document, and an edit that half-applies is worse than one that replaces (`replaceKeys` deletes
  then re-inserts).
- `Delete(ctx, id)` — refuses with `ErrProfileBuiltin` if the profile is shipped; builtins can be
  used and copied but not removed.
- `EnsureBuiltins(ctx)` — seeds the shipped catalog (`profile_catalog.go.md`) on every boot.
  Existing profiles are left ALONE (matched by `Slug`) — a site that has tuned a builtin's
  deadbands must not have that overwritten on the next boot, the same rule the RBAC seeder
  follows.

## Key Types: SaveProfileRequest / SaveTelemetryKey / ProfileDetail

Request/response DTOs for the profile CRUD API (`apis/profiles.go`).

## Notes

- `replaceKeys`'s delete-then-insert against a **fresh, empty** table is exactly what
  `EnsureBuiltins` does on first boot — this was the trigger for the "total affected: 0"
  panic fixed by `isNoResultErr` (see `device.go.md`).
- An edited profile must invalidate `services.Ingest`'s cached bindings for it
  (`apis.profilesApi.update`/`remove` call `ingest.InvalidateProfile(id)`) so a changed deadband
  takes effect on the next message, not the next restart.
