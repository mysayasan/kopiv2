# Module: apps/mymatasan/services/update.go

## Purpose

In-app self-update: checks GitHub Releases for a newer `mymatasan` build (scheduled + on demand) and, on installs that own their files (portable archive or the Windows installer), downloads, SHA-256-verifies, and swaps in the new binary + `static/`/`ai/` assets, then restarts via `apphost.Restarter`. Package (`.deb`/`.rpm`) and Docker installs are detected and steered to their own upgrade path instead of self-updating.

## Responsibilities

- `UpdateService` (`NewUpdateService(currentVersion, homeDir, restarter)`) — `restarter` is the minimal `updateRestarter` interface (`Restart(reason string)`, satisfied by `apphost.Restarter`) so `services` doesn't import `apphost`.
- `RegisterScheduler(start)` — wired from `app.go` via `deps.Scheduler.StartPeriodic`; runs `CheckNow` immediately and then every 6 hours so the UI's "update available" state stays fresh without the client polling GitHub itself.
- `managedKind()` — reads `MYMATASAN_MANAGED` (`"package"` or `"docker"`, set by `deploy/nfpm/mymatasan.service` and `deploy/Dockerfile.release` respectively); empty means portable/installer.
- `canSelfUpdate()` — true only when `managedKind()` is empty AND `homeDir` is writable (probes with a throwaway file). This is the gate the API and UI use to decide whether to offer "Update now" vs. package/Docker guidance.
- `CheckNow(ctx)` / `Status()` — `CheckNow` queries `GET https://api.github.com/repos/mysayasan/kopiv2/releases/latest` (optionally authenticated via `GITHUB_TOKEN` for a higher rate limit) and caches `latest`/`htmlURL`/`publishedAt`/`checkErr`; `Status()` returns the cached `UpdateInfo` snapshot, computing `UpdateAvailable` via `versionGreater(latest, current)` (SemVer compare through `infra/versioning.ParseSemVer`).
- `StartUpdate(ctx)` / background `run()` — errors immediately when `canSelfUpdate()` is false, an update is already applying, or there's no newer version cached; otherwise runs in the background:
  1. Re-fetches the latest release, then `selectAssets` picks the archive matching the `mymatasan_` product prefix **and** `runtime.GOOS`/`runtime.GOARCH` (suffix `_<os>_<arch>`, `.zip` on Windows else `.tar.gz`) plus the release's `checksums.txt`.
  2. Downloads the archive into `<homeDir>/.mmupdate` (staging dir, cleaned up after).
  3. `verifyChecksum` — SHA-256 the downloaded archive and compare against the matching line in `checksums.txt`; mismatch aborts.
  4. Extracts (`.tar.gz` via the shared `extractTarGz` from `python_install.go`, `.zip` via `extractZipAll`).
  5. `swap(extractDir)` — copies the new binary into the home dir then renames it into place (same-filesystem atomic rename); on Windows the running exe is first renamed aside to `<exe>.old` since it can't be overwritten in place. `static/` and `ai/` directories are swapped the same way (old dir renamed to `<dir>.old`, new dir renamed into place; best-effort rollback on failure).
  6. On success, calls `restarter.Restart("self-update")` after a short delay so the HTTP response reaches the client first.
- `CleanupStaleFiles()` — called once at startup (`app.go`) to remove leftovers from a previous update (`<exe>.old`/`.new`, `.mmupdate`, `static.old`, `ai.old`).

## Notes

- Exposed by `apis/system.go`: `GET /api/system/update` (status), `POST /api/system/update/check` (force a check), `POST /api/system/update/apply` (admin-write, starts the background apply).
- `UpdateInfo` (the JSON the UI polls) includes `current`, `latest`, `updateAvailable`, `canSelfUpdate`, `managed` (`""`/`"package"`/`"docker"`), `htmlUrl`, `publishedAt`, `checkedAt`, `error`, plus the apply job's `applying`/`applyStatus`/`applyLog`.
- The release archive must contain a `checksums.txt` asset (produced by GoReleaser) and a binary named after the running executable or literally `mymatasan`/`mymatasan.exe` — `selectAssets`/`swap` fail closed otherwise.
- **Security: product-prefix guard.** `selectAssets` requires the asset name to start with `mymatasan_`, not just contain the `_<os>_<arch>` suffix. myseliasan now also releases from this repository and its archives carry the identical suffix and extension (e.g. `myseliasan_1.9.0_windows_amd64.zip`) sharing the same `checksums.txt`, so a suffix-only match would let this updater download and install a myseliasan release over mymatasan (the checksum would even verify). This is the second, independent guard — the first is that myseliasan publishes its own releases with `--latest=false` under its own tag namespace, so it should never be what `GET .../releases/latest` returns in the first place (see `deploy/README.md` and `deploy/README-myseliasan.md`).
- `apphost.Restarter.Restart` is the same primitive used by the factory reset and (on Windows) the SCM-aware `runWithPlatform`; a supervised deployment (`KOPIV2_SUPERVISED=1`) exits cleanly for its supervisor to relaunch, and a bare-metal run relaunches itself.
