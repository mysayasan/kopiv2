# Module: domain/shared/services/system_reset.go

## Purpose

Factory reset for the control-plane / appliance style apps — `myseliasan`, `myidsan`,
`myiotsan`. Each previously had no factory reset at all (`mymatasan` has its own, older,
richer orchestrator — see `apps/mymatasan/services/system_reset.go.md`). This is a shared
seam so the three apps get one implementation instead of three near-identical copies.

## Responsibilities

- `ResetBootstrapOptions(appName, cfg, entities, migrations, seeders)` — builds a
  `bootstrap.Options` from the running app config, so each app's `app.go` wires a reset in
  a few lines instead of a field-by-field copy. Its doc comment carries the load-bearing
  warning: migrations must still be passed even though the rebuilt database is brand new —
  a fresh database **baselines** them (records every migration as applied without running
  it, since the schema was just created at the current shape). Omit them and the rebuild
  still writes a manifest (so the next boot reads as "not fresh"), and every migration is
  then replayed against an already-current schema and fails — a factory reset would leave
  the app unable to start. See `docs/DB_BOOTSTRAP_SPEC.md`'s "Baselining" section.
- `SystemResetService` — the orchestrator. Deliberately **simpler** than mymatasan's: no
  shred / TRIM / free-space-scrub stages, because none of these three apps holds a large
  media library — their state is the database, a handful of uploaded files, and the
  at-rest key, so the honest implementation is short: stop background work, destroy the
  key, unlink the data directories, drop and rebuild the database, restart. The JSON
  `ResetProgress` contract deliberately matches mymatasan's so the shared SPA overlay
  (`frontend/shared/src/FactoryReset.js`) behaves identically across the suite.
  - `ResetProgress{Running, Stage, Percent, Message, Warning, Error, StartedAt, UpdatedAt}`
    — kept in memory only, never persisted: the database is dropped mid-flight, so the UI
    polls this out of process memory until the server restarts under it. `Stage` is one of
    `idle` / `erasing` / `wiping_db` / `restarting` / `failed`.
  - `Allowed()` — reflects `SystemResetConfig.BootstrapOpts.Bootstrap.AllowReset`
    (`bootstrap.allowReset`), so the button/dialog stay hidden client-side on an install
    that has not opted in.
  - `ConfirmPhrase()` — the app's own name (each app's `app.go` sets `ConfirmPhrase:
    m.Name()`), exposed so the UI's typed-confirmation instruction and the server-side
    check can never drift apart.
  - `Start(confirm string)` — verifies the typed phrase **server-side**
    (`ErrConfirmMismatch` on a mismatch), not only in the browser: the client dialog
    guards against a mis-click, this guards against anything else that can reach an
    authenticated endpoint — a stray `curl`, a replayed request, a script pointed at the
    wrong host. Refuses if reset is disabled (`Allowed() == false`) or already running,
    otherwise launches `run()` in the background and returns immediately.
  - `run()` — best-effort pipeline that **always** drives to a restart (a half-wiped
    install that never restarts is the worst outcome), ordered so irreversible work lands
    before anything slow or fallible:
    1. `StopServices()` (optional) — stop pollers/control channels/schedulers before the
       wipe.
    2. `KeyStore.Destroy()` (optional, `CryptoEraser` interface) — crypto-erase first, so
       every sealed column/file becomes unrecoverable immediately regardless of what the
       later stages manage to do. A failure is a non-fatal `warn`, not an abort.
    3. `eraseData(collectRoots(ctx))` — empties each resolved root's **contents** via
       `os.RemoveAll` per entry, leaving the (now empty) directory in place.
    4. `CloseDatabase()` (optional) — close this process's own DB connection pool
       immediately before the drop. **Required for sqlite on Windows**, where the file
       cannot be deleted while this process holds it open — without it the wipe silently
       "succeeds" while the old data survives.
    5. `bootstrap.Reset(ctx, BootstrapOpts)` — drop + rebuild + reseed. A reported error is
       a `warn`, not an abort, since restarting re-runs bootstrap and can finish a rebuild
       a transient error interrupted.
    6. `Restarter.Restart("factory reset")`, after a 1.5s delay so the client can read the
       final `100%`/`restarting` state before the server goes down under it.
  - `InProgress()` — true while `Running`, and stays true through the `restarting` stage:
    `run()` clears `Running` about a second before the process actually relaunches, but
    the DB pool is already closed by then, so `apis.NewResetGate` (below) must keep
    shedding load until this process dies.
  - `collectRoots(ctx)` — resolves `CollectDataPaths(ctx)` to absolute paths and refuses
    the **working directory** and any **filesystem root** (`abs == filepath.Dir(abs)`): a
    reset must not be able to remove the app itself. Deduplicates and drops empty/`"."`/
    `".."` entries.
- `SystemResetConfig` — wires the orchestrator per app: `ConfirmPhrase`, `CollectDataPaths`,
  `BootstrapOpts`, `Restarter` (`ProcessRestarter`, satisfied by `apphost.Restarter`),
  `KeyStore` (optional, `CryptoEraser`, satisfied by `atrest.KeyStore`), `StopServices`
  (optional), `CloseDatabase` (optional but strongly recommended), `Logf` (optional — the
  only durable trace, since the audit trail lives in the database being dropped).

## Notes

- Interfaces (`ProcessRestarter`, `CryptoEraser`) are kept local so this package doesn't
  depend on host/crypto wiring, mirroring mymatasan's own `services/system_reset.go`
  pattern.
- Per-app wiring lives in each app's `app.go` (`apps/myseliasan/app/app.go.md`,
  `apps/myidsan/app/app.go.md`, `apps/myiotsan/app/app.go.md`) and differs in exactly what
  `CollectDataPaths` returns, whether a `KeyStore` is available, and what `bootstrap.allowReset`
  ships as: **false** for myseliasan and myidsan, **true** for myiotsan.
- HTTP surface lives in `domain/shared/apis/system_reset.go` (see that doc) — this package
  has no HTTP awareness of its own.
- Frontend: `frontend/shared/src/FactoryReset.js` (`FactoryResetSection`,
  `FactoryResetDialog`, `FactoryResetOverlay`), mounted per app in the Settings > System
  tab.
