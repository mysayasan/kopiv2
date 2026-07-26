# Module: apps/myseliasan/services/settings_materialize.go

## Purpose

Materializes edited settings back into the on-disk `config.json`.

Why write the file at all, rather than only a DB row the way mymatasan's own runtime settings
work: myseliasan's editable settings (TLS, CSP, rate limit, cache, logging, telemetry, SSO,
pairing) are INFRASTRUCTURE blocks the shared apphost reads exactly ONCE at boot — before the
app is ever handed a database handle. There is no seam for a DB value to override them at
startup, so the only way an edit can take effect is to write it into the file the host re-reads
on the next launch, then restart (`apis/system.go`). This mirrors the host's own
`persistJWTSecret` write-back. The DB copy (`ControlSetting` row, see `settings.go.md`) remains
the authoritative store for reset-to-defaults and audit, not for the live value.

The write is **surgical**: only the edited top-level blocks are re-serialized; every other
block — including ones this feature never exposes (`db`, `server`, `bootstrap`) — keeps its
original bytes verbatim, so an untouched block never reformats. The original top-level key
ORDER is preserved, keeping the file readable and diffs small.

## `configPatch`

Sets one leaf value at a nested path, e.g. `{["rateLimit","devOnly","requests"], 300}`. A
single-element path sets a top-level scalar (e.g. `{["allowOrigins"], "*"}`).

## `materializeConfig(configPath string, patches []configPatch) error`

Reads `configPath`, applies `patches` via `patchConfigBytes`, and writes the result back
atomically (`atomicWrite`). An empty patch list is a no-op — no file is read or written.

## `patchConfigBytes(raw []byte, patches []configPatch) ([]byte, error)`

Groups patches by their top-level key. For each affected key: a single scalar patch
(`len(path)==1`) replaces the whole top-level value; otherwise the existing block is decoded,
`setPath` writes the nested leaves into it, and the block is re-serialized alone — sibling keys
inside the same block that were not part of this save are preserved (decoded then
re-marshaled), but every OTHER top-level block is left as untouched raw bytes.

## `encodeOrdered(raw []byte, top map[string]json.RawMessage) ([]byte, error)`

Stitches the (possibly patched) top-level blocks back into a pretty-printed JSON document,
emitting keys in the **original document order** (`topLevelKeyOrder`, a `json.Decoder`
token-walk that reads each key then skips its value) — any brand-new key is appended, sorted.
Each block's bytes are used verbatim where unpatched, so an untouched block keeps its exact
original formatting/whitespace.

## `atomicWrite(path string, data []byte) error`

Writes to a temp file in the same directory (`os.CreateTemp`), then `os.Rename`s it over
`path` — a crash mid-write never leaves a truncated `config.json`. `os.Rename` replaces the
destination on both POSIX and Windows.

## Notes

- This file has no dependency on `ISettingsService`'s validation — it only ever receives
  patches that `settings.go`'s `commit` has already run through `validateSection`/
  `applyToConfig`, so it does no value-level checking of its own, only structural JSON
  patching.
- `flattenPatches` (in `settings.go`) is what turns a section's nested map into the
  `[]configPatch` this file consumes — every leaf becomes one patch, sorted by key for
  deterministic output.
