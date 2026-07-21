# Module: apps/myseliasan/apis/basemap.go

## Purpose

Serves the offline vector basemap for the fleet map's geographic view: one or more self-hosted
[Protomaps](https://protomaps.com/) PMTiles **region** archives, so an air-gapped/intranet install
never reaches for a tile CDN (OpenStreetMap tiles, Google Maps, etc) for normal operation. Also
exposes an **optional, opt-in** on-demand region download when an operator has configured a remote
pmtiles source — the one action in this app that deliberately reaches the internet.

## Why a directory of files instead of a tile server

PMTiles is a flat, range-addressed archive: the browser (via `ol-pmtiles`, vendored into
`apps/myseliasan/views/react-webpack`) fetches byte ranges of **one file per region** and does tile
lookup client-side. That means no tile-server process, no SQLite (as MBTiles would need), and no
new Go dependency — `net/http`/`http.ServeContent` already answers Range requests. A fleet spanning
several disjoint areas (e.g. two campuses in different cities) is served as **several region
files** rather than one planet-sized archive, each independently provisioned (`pmtiles extract`
out-of-band, or downloaded on demand — see below) and dropped in the basemap directory.

## Endpoints

All routes require a myseliasan session (`auth.Middleware` + `session.Middleware`).

| Method | Path | Notes |
|---|---|---|
| GET | `/api/basemap/info` | Cheap, always `200`. Returns `{available, attribution, canDownload, regions[]}`. `regions` is one entry per `*.pmtiles` file in the basemap dir: `{name, bounds, sizeBytes}` — `bounds` (`[minLon,minLat,maxLon,maxLat]`) is parsed straight out of the PMTiles v3 header (`readPMTilesBounds`, no external tool needed) so the frontend can add a layer per region and constrain the view to it; `nil` when the header can't be read. `available: false` (no region files) is a **supported state**, not an error — the frontend adds no basemap layer and the map still renders (node/building pins over blank space). `canDownload` reports whether a remote source is configured (env var or runtime `PUT /config`). `attribution` is a static string (`"© OpenStreetMap contributors"`, the ODbL-required credit), never a network call. |
| GET | `/api/basemap/config` | Reports download setup state for the UI: `{source, canDownload, hasTool, envManaged}`. `hasTool` is whether the `pmtiles` binary is resolvable on this host (`exec.LookPath`, or a direct `os.Stat` when configured as a path). `envManaged: true` means `MYSELIASAN_BASEMAP_SOURCE` is set and the runtime value below is read-only. |
| PUT | `/api/basemap/config` | Sets (or clears, with an empty value) the runtime remote source URL: `{source}`. Persisted to `<basemapDir>/source.txt` so it survives a restart without an env var. Refused (400) when the env var manages it, or when `source` is non-empty and not `http://`/`https://` (the server will fetch from it — a deliberate URL, not a path). |
| POST | `/api/basemap/download` | Extracts a new region archive from the configured remote source via the `pmtiles extract` CLI tool. Body: `{minLon, minLat, maxLon, maxLat, maxZoom}`. Refused (400) when no source is configured, the bbox is invalid/inverted/out-of-range, or the requested area exceeds 25°×25° (a sanity cap so one request can't try to pull the whole planet at street level); `maxZoom` is clamped to `(0,14]`, defaulting to 14. Only one extract runs at a time (`sync.Mutex.TryLock`, refused with 400 rather than queued) since it is CPU/IO-heavy and writes into the shared dir; bounded by a 12-minute context timeout. Saves the result as `region_<minLon*100>_<minLat*100>_<maxLon*100>_<maxLat*100>_z<zoom>.pmtiles`, written to a `.part` temp file first and renamed on success so a failed/killed extract never leaves a half-written region for `/info` to pick up. Returns the new `regionInfo` on success. |
| GET | `/api/basemap/tiles/{name}` | One region archive, Range-served via `http.ServeContent` (206 + `Accept-Ranges` + `If-Range`). `{name}` must match `nameRe` (`^[A-Za-z0-9_.-]+\.pmtiles$`) — this is the path-traversal guard, since the name is resolved directly inside the basemap dir with no other sanitization. Returns 400 (`ErrBadRequest`) on an invalid name or a missing/unreadable region. |

## Notes

- **No compression middleware on the tiles route, ever.** A region archive's tiles are already
  gzip-compressed internally (PMTiles header "tile compression: gzip"), and the client addresses
  them by absolute byte offset. A compressing middleware rewriting the response body would corrupt
  those offsets. There is deliberately no compression middleware in this codebase; if one is ever
  added, it MUST skip `/api/basemap/tiles/{name}`.
- `Cache-Control: public, max-age=86400` on a tiles response — it changes only when an operator
  re-provisions/re-downloads that region.
- **The one online action in an otherwise air-gapped app**: `/download` is disabled by default
  (`canDownload: false`) and only becomes available when `MYSELIASAN_BASEMAP_SOURCE` (env, always
  wins and locks the field in the UI) or a runtime `PUT /config` source is set, **and** the
  `pmtiles` binary is resolvable (`MYSELIASAN_PMTILES_BIN`, default `"pmtiles"` on `PATH`). An
  intranet install that never sets either stays fully offline exactly as before this feature.
- `ResolveBasemapDir(dataDir, configured)` — an explicit configured path wins; otherwise
  `<dataDir>/basemap` is used. Unlike the old single-file resolver, this always returns a path (a
  directory, not a specific archive) and never checks existence — an absent/empty directory is a
  supported state handled by `regionFiles()` returning nothing. `app.go` calls this with an empty
  `configured` (`apis.ResolveBasemapDir(deps.DataDir, "")`), and passes `MYSELIASAN_BASEMAP_SOURCE`
  / `MYSELIASAN_PMTILES_BIN` env vars through as the `source`/`bin` constructor args.
- Covered by `apis/basemap_test.go`: Range-request correctness (exact byte slice + headers), no
  `Content-Encoding` on the response, `{name}` path-traversal rejection, `/info` degrading
  gracefully with no region files and listing regions when present, and `ResolveBasemapDir`'s
  default-directory convention.
