# Module: infra/config/configfile/patch.go

## Purpose

Package `configfile`: the shared surgical `config.json` writer. LIFTED OUT of
`apps/myseliasan/services/settings_materialize.go` (now deleted) so a second caller —
the pre-boot setup wizard, `infra/apphost/firstboot` (`../apphost/firstboot/
firstboot.go.md`) — could reuse the identical leaf-patch/atomic-write logic instead of
growing its own copy. `apps/myseliasan/services/settings.go` (`apps/myseliasan/services/
settings.go.md`) is the other, pre-existing caller, now calling this package instead of
its own local functions.

The suite's infrastructure blocks (`db`, `cache`, `server`, `tls`, `logging`, `rateLimit`,
...) are read by `infra/apphost` exactly ONCE at boot, before any app is handed a database
handle. There is no seam for a stored value to override them at startup, so the only way
an edit can take effect is to write it into the file the host re-reads on the next launch
(myseliasan's Settings editor: edit, then restart) or continues booting from in the same
process (the setup wizard: edit, then continue).

The write is **surgical**. Only the top-level blocks named by the patches are
re-serialized; every other block — including ones a given caller never exposes — keeps
its original bytes verbatim, so an untouched block never reformats and diffs stay small.
The original top-level key ORDER is preserved for the same reason. Writes are atomic
(temp file + rename), so a crash mid-write can never leave a truncated `config.json` — the
file that, if lost, means the app cannot boot at all.

Known follow-up, deliberately NOT done by this change: `apps/myidsan/services/
settings_materialize.go` still carries its own separate, un-consolidated copy of this
same logic.

## `Patch` / `Flatten`

`Patch{Path []string, Value any}` sets one leaf value at a nested path (e.g.
`{["rateLimit","devOnly","requests"], 300}`); a single-element path replaces a top-level
scalar wholesale (e.g. `{["allowOrigins"], "*"}`).

`Flatten(prefix []string, data map[string]any) []Patch` turns a nested map into leaf
patches — scalars become one patch each, nested maps recurse, keys walked in sorted order
so a given input always produces the same patch list (deterministic output, small diffs).

## `Materialize(configPath string, patches []Patch) error`

Reads `configPath`, applies `patches` via `PatchBytes`, writes the result back atomically
(`AtomicWrite`). An empty patch list is a no-op — no file is read or written, so a caller
that computed no changes never touches the file.

## `PatchBytes(raw []byte, patches []Patch) ([]byte, error)`

Groups patches by top-level key. For each affected key: a single scalar patch
(`len(path)==1`) replaces the whole top-level value; otherwise the existing block is
decoded, `SetPath` writes the nested leaves into it, and the block alone is re-serialized
— sibling keys inside that block not part of this write are preserved (decoded then
re-marshaled), while every OTHER top-level block is left as untouched raw bytes.

## `SetPath(root map[string]any, path []string, value any)`

Walks `path`, creating intermediate `map[string]any` objects as needed, and sets the leaf
value. Exported so callers with their own nested-map plumbing (e.g. myseliasan's
`setLeaf`) can share it without going through a full `Patch`/`Flatten` round trip.

## `encodeOrdered` / `TopLevelKeyOrder`

`encodeOrdered` stitches the (possibly patched) top-level blocks back into a
pretty-printed JSON document, emitting keys in the document's ORIGINAL order
(`TopLevelKeyOrder`, a `json.Decoder` token-walk that reads each key then skips its
value via `Decode` into a `json.RawMessage`) — any brand-new key is appended, sorted. Each
block's bytes are used verbatim where unpatched, so an untouched block keeps its exact
original formatting/whitespace.

## `AtomicWrite(path string, data []byte) error`

Writes to a temp file in the same directory (`os.CreateTemp`), then `os.Rename`s it over
`path` — a crash mid-write never leaves a truncated `config.json`. `os.Rename` replaces
the destination on both POSIX and Windows.

## Notes

- This package has no dependency on any caller's validation — it only ever receives
  patches its callers have already validated (myseliasan's `commit` runs
  `validateSection`/`applyToConfig` first; the wizard's `commit` runs `validate` first), so
  it does no value-level checking of its own, only structural JSON patching.
- See `apps/myseliasan/services/settings.go.md` and `infra/apphost/firstboot/
  firstboot.go.md` for the two current callers.
