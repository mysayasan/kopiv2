# Module: apps/myiotsan/services/profile_transfer.go

## Purpose

Profile import/export: a device type is a small declarative document, so it should be portable.

The point is not backup. Tuning a profile — getting a deadband right for a particular sensor in
a particular building — is real work, and an integrator who does it once should be able to carry
it to the next site rather than rediscovering it. Same reasoning as mymatasan's `.mmskill`
export. The format is deliberately plain JSON with a version field: a config format nobody can
read in a text editor is a config format nobody will trust.

## Key Type: ProfileExport

```go
type ProfileExport struct {
    Version       int
    Slug          string
    Name          string
    Vendor        string
    Description   string
    TopicTemplate string
    PayloadFormat string
    Keys          []SaveTelemetryKey
}
```

The portable form of a `DeviceProfile` + its `TelemetryKey`s. `profileExportVersion` (currently
`1`) guards the format.

## Key Function: (*ProfileService) Export

```go
func (s *ProfileService) Export(ctx context.Context, id int64) (*ProfileExport, error)
```

Renders a profile (via `Detail`) as a `ProfileExport` document. Served by
`GET /api/profiles/{id}/export` (`apis/profiles.go.md`).

## Key Function: (*ProfileService) Import

```go
func (s *ProfileService) Import(ctx context.Context, raw []byte, actor int64) (*ProfileDetail, error)
```

Creates a profile from a document, with three guarantees worth relying on:

- **An imported profile is NEVER builtin, whatever the document claims.** "Builtin" means
  "shipped by us and therefore undeletable"; a file off the internet does not get to assert that
  about itself.
- **A slug collision is REPORTED, not silently overwritten.** Quietly replacing a profile would
  re-point every device using it at different decoding rules — data corruption wearing the
  costume of a successful import. `Import` returns an error naming the existing slug instead.
- **An unknown format version is refused, not guessed at.** `doc.Version != profileExportVersion`
  is a hard error — a half-understood profile would silently mis-decode every reading the
  devices using it will ever send.

Also rejects an empty slug and a document with zero telemetry keys (nothing a device sends could
be decoded). On success, delegates to the ordinary `ProfileService.Create` path — an imported
profile is created exactly like a hand-authored one. Served by `POST /api/profiles/import`
(`apis/profiles.go.md`), body capped at 256KB.

## Notes

- Reuses `SaveTelemetryKey` (the same DTO `apis/profiles.go`'s create/update path uses) for the
  `Keys` field, so export/import round-trips through the identical shape the profile CRUD API
  already validates.
