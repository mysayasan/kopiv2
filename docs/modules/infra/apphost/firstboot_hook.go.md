# Module: infra/apphost/firstboot_hook.go

## Purpose

Wires the `infra/apphost/firstboot` package (`firstboot/firstboot.go.md`) into `run.go`'s
`runApp`, and supplies the capabilities the wizard needs from the host — a real database
dial, a real cache dial, a browser launcher — without the `firstboot` package itself
depending on `infra/db/sql`, `infra/cache`, or any app.

## `runFirstBootSetup(app App, homeDir, dataDir string) error`

Called from `runApp` right after path resolution and before `loadConfig` (`run.go.md`).
Seeds the data dir's config from the home dir's shipped default first
(`seedConfigIfMissing`) — the wizard rewrites the DATA dir's copy, so a packaged install
must have one to edit. Then asks `firstboot.Needed` whether to run at all; the
overwhelmingly common case (every boot after the first) costs one small file read and
returns immediately.

When needed, installs its own `signal.NotifyContext` — boot has not reached the point
where the app's own shutdown handling exists yet, so an operator who abandons setup with
Ctrl+C, or a service stopped while the page is open, must exit cleanly rather than being
killed mid-write — and calls `firstboot.Run`. On success, startup continues normally in
the same process and `loadConfig` reads the file the wizard just wrote.

## `firstBootOptions(appName, configPath, dataDir string) firstboot.Options`

Assembles the host-supplied capabilities. Kept as its own function so the wiring can be
asserted by a test: every capability is optional to `firstboot` and silently degrades to
"not verified"/"no browser opens" when nil, which means "we forgot to pass one" would
otherwise be invisible at runtime.

## `probeDB(dataDir) func(ctx, firstboot.DBSettings) error`

Opens a REAL connection with the operator's answers through the same `newDbCrud` the boot
itself uses (`run.go`'s DB adapter selection) — a probe that dialled the port some other
way could pass on settings the app then fails to start with. A SQLite path is resolved
data-relative exactly as it is at boot (`ResolveWritablePath`). Bounded: `newDbCrud` pings
during construction and offers no context of its own, so the dial runs in a goroutine and
the probe returns "connection timed out" on `ctx.Done()` rather than hanging the setup
page for the driver's own connect timeout; the dial is left to finish and close itself in
the background if it does eventually complete.

## `probeCache(ctx, firstboot.CacheSettings) error`

Pings Redis with the operator's answers via `cache.NewRedisStore` — mirrors the in-app
Settings editor's own cache test (`apps/myseliasan/services/settings.go.md`'s
`TestCache`) for the same reason: an unreachable cache is a boot failure, and finding that
out here costs a click instead of a failed start.

## `closeQuietly(db dbsql.IDbCrud)`

Releases a probe's connection pool if the adapter implements `io.Closer`; every engine's
CRUD implementation currently does, and the type assertion keeps this honest if one ever
stops.

## Notes

- This file is the ONLY place `infra/apphost` builds a real DB/cache connection for the
  wizard — `firstboot` itself never imports `infra/db/sql` or `infra/cache`.
- `firstboot.Options.Browser` is set to `launchBrowser` (`browser.go.md`) — the exact same
  suppression rules (service/container/no-display/`KOPIV2_*` env) apply to the wizard's
  own browser launch as to the app's normal ready banner.
