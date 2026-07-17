# Module: apps/myiotsan/services/flow_transfer.go

## Purpose

Flow import/export: a flow is a small declarative document (its graph is already JSON), so it
should be portable — the same reasoning as a device profile (`profile_transfer.go.md`) or a
mymatasan `.mmskill`. Designing a flow is real work; an integrator who builds a good "Solar system"
flow once should be able to carry it to the next site, not redraw it.

The format is plain JSON with a version field. An importer that meets a version it does not know
refuses rather than guessing.

## Key Type: FlowExport

```go
type FlowExport struct {
    Version     int    // flowExportVersion = 1
    Slug        string
    Name        string
    Description string
    Category    string
    Graph       string // the node/wire document verbatim
}
func (s *FlowService) Export(ctx context.Context, id int64) (*FlowExport, error)
```

`Graph`'s device references are natural keys, so the import lands unbound on a new site until its
devices exist under those keys — data, not dangling foreign keys.

## Key Function: Import

```go
func (s *FlowService) Import(ctx context.Context, raw []byte, actor int64) (*flowImportResult, error)
```

Creates a flow from a document. Refuses:

- A version other than `flowExportVersion` ("unsupported flow format version …").
- An empty name.
- A graph that fails `parseGraph`.
- A slug that already exists at this site (reported, never silently overwritten).

An imported flow is NEVER builtin, whatever the document claims, and is created DISABLED — a flow
carried from another site references devices by key that may not exist here yet, and it should not
start acting until an admin has looked at it and switched it on.

## Notes

- Backs `POST /flows/import` and `GET /flows/{id}/export` (`apis/flows.go.md`).
