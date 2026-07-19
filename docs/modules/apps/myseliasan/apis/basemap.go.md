# Module: apps/myseliasan/apis/basemap.go

## Purpose

Serves the offline vector basemap for the fleet map's geographic view: a self-hosted
[Protomaps](https://protomaps.com/) PMTiles archive, so an air-gapped/intranet install never
reaches for a tile CDN (OpenStreetMap tiles, Google Maps, etc).

## Why a single file instead of a tile server

PMTiles is a flat, range-addressed archive: the browser (via `ol-pmtiles`, vendored into
`apps/myseliasan/views/react-webpack`) fetches byte ranges of **one file** and does tile lookup
client-side. That means no tile-server process, no SQLite (as MBTiles would need), and no new Go
dependency — `net/http`/`http.ServeContent` already answers Range requests. The archive is
produced out-of-band with `pmtiles extract` and dropped in the data dir; nothing here talks to
the internet.

## Endpoints

Both routes require a myseliasan session (`auth.Middleware` + `session.Middleware`).

| Method | Path | Notes |
|---|---|---|
| GET | `/api/basemap/info` | Cheap, always `200`. Returns `{available, attribution, sizeBytes}`. `available: false` (no archive provisioned) is a **supported state**, not an error — the frontend adds no basemap layer and the map still renders (node pins over blank space). `attribution` is a static string (`"© OpenStreetMap contributors"`, the ODbL-required credit), never a network call. |
| GET | `/api/basemap/tiles.pmtiles` | The archive itself, Range-served via `http.ServeContent` (206 + `Accept-Ranges` + `If-Range`). Returns 400 (`ErrBadRequest`) when no basemap is provisioned. |

## Notes

- **No compression middleware on this route, ever.** The archive's tiles are already
  gzip-compressed internally (PMTiles header "tile compression: gzip"), and the client addresses
  them by absolute byte offset. A compressing middleware rewriting the response body would corrupt
  those offsets. If a compression middleware is ever added to this codebase, it MUST skip
  `/api/basemap/tiles.pmtiles`.
- `Cache-Control: public, max-age=86400` on the archive response — it changes only when an
  operator re-provisions it.
- `ResolveBasemapPath(dataDir, configured)` — an explicit configured path wins; otherwise
  `<dataDir>/basemap/basemap.pmtiles` is used when present. Returns `""` (not provisioned) rather
  than erroring when the file is absent, since requiring a ~200MB asset to boot would be hostile.
  `app.go` calls this with an empty `configured` (`apis.ResolveBasemapPath(deps.DataDir, "")`).
- Covered by `apis/basemap_test.go`: Range-request correctness (exact byte slice + headers),
  no `Content-Encoding` on the response, and `/info` degrading gracefully with no archive.
