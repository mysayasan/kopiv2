# Module: apps/myseliasan/services/filesystem_browse.go

## Purpose

The server-side file/folder picker behind the Settings editor's path fields (TLS cert/key, SSO CA
cert, file-storage folder, log file — `services/settings.go.md`) — an operator browses to a path
in a modal instead of typing one out by hand. Ported from mymatasan's own picker. Read-only (names
and directory-ness only, never file contents) and confined to a whitelist of allowed roots; the
API layer (`apis/settings.go.md`) additionally gates the whole surface to superadmins.

## Types

- `FsEntry{Name, Path, Dir}` — one item in a directory listing.
- `FsBrowseResult{Path, Parent, Separator, Entries}` — one directory level: its absolute path, the
  parent to navigate up to (empty at an allowed root, so the picker falls back to the roots
  listing rather than escaping the whitelist), the OS path separator (so the frontend can join
  paths correctly cross-platform), and its entries.

## `BrowseDirectory(dir string, extraRoots []string) (FsBrowseResult, error)`

Lists the immediate entries of `dir`, confined to the allowed roots (`allowedBrowseRoots`).
`extraRoots` are additional site-specific roots the caller wants reachable (`apis/settings.go`
passes `deps.DataDir` and `deps.HomeDir`, wired in `app.go`'s `RegisterAppRoutes`), on top of the
built-in whitelist.

- An empty `dir` returns the roots themselves (`rootListing`) rather than the whole filesystem —
  this is the picker's opening state.
- A non-existent or unresolvable `dir` (e.g. a default `"./certs/cert.pem"` that hasn't been
  created yet) is not an error — it also falls back to the roots listing, so a field's current
  (possibly relative, possibly absent) value never breaks the picker.
- A `dir` that resolves to a file rather than a directory browses that file's containing folder
  instead.
- A `dir` outside every allowed root also falls back to the roots listing — the whitelist is
  enforced here, not left to the caller.
- Otherwise reads the directory (`os.ReadDir`), hides dotfiles (still typeable by hand), and sorts
  directories first, then case-insensitively by name.

## `allowedBrowseRoots(extra []string) []string`

Builds the whitelist: the app's current working directory, the user's home directory, a short list
of OS-specific common install/config locations (`ProgramFiles`/`ProgramData`/`LOCALAPPDATA` on
Windows; `/etc`, `/opt/homebrew/etc`, `/Applications` on darwin; `/etc`, `/opt`, `/var/lib` on
other Unix), and `extra` (the app's writable data dir, so `./certs`, `./uploads`, `./logs` under it
are reachable). Only directories that actually exist are kept, de-duplicated
(`normForCompare`/`pathEqual` — case-insensitive on Windows).

## `withinAllowed` / `allowedParent` / `rootListing`

- `withinAllowed(dir, roots)` — is `dir` equal to, or nested under, one of `roots`.
- `allowedParent(dir, roots)` — the directory to navigate up to; `""` at an allowed root or when
  the parent would escape the whitelist (so "up" from a root never leaks the true filesystem root).
- `rootListing(roots, sep)` — presents the whitelisted roots as the top-level picker entries (used
  both for the initial empty-path call and every whitelist-escape fallback above).

## Notes

- No write, delete, or content-read operation exists in this file — it only ever returns names and
  a directory flag. The only side channel is confirming a path *exists* and *what's alongside it*,
  which is why it stays server-side and superadmin-gated (`apis/settings.go`'s `requireSuper`)
  rather than exposed to a lesser role.
- Tested in `filesystem_browse_test.go`: listing within an allowed root (including the
  directories-first sort and dotfile hiding) and the whitelist guard itself
  (`withinAllowed`/`allowedParent` reject anything outside the sandbox, including the allowed
  root's own parent).
- See `apis/settings.go.md` for the `GET /api/settings/fs/browse` HTTP surface and how
  `browseRoots` is threaded in from `app.go`.
