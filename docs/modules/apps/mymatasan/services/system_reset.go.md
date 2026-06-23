# Module: apps/mymatasan/services/system_reset.go

## Purpose

Orchestrates the **Secure Wipe & Reset** (factory reset): crypto-erase the at-rest encryption key, fast-erase all media, drop and rebuild + reseed the database, securely scrub the freed disk space, then restart the process into a clean first-run state. Runtime settings reset to defaults naturally because they live in the (now-dropped) database.

## Responsibilities

- `SystemResetService` with an in-memory `ResetProgress` (`Running`, `Stage`, `Percent`, `Message`, `Warning`, `Error`, timestamps) — progress is deliberately NOT persisted because the database is dropped mid-flight; the UI polls it from process memory.
- `Allowed()` — reflects `bootstrap.allowReset`.
- `Start()` — refuses when reset is disabled or one is already running; otherwise launches `run()` in the background and returns the initial progress.
- `run()` — best-effort pipeline that ALWAYS drives to a restart, ordered so the irreversible work survives a force-stop:
  1. `StopServices()` — stop recorders/monitors/detector first so the recording ffmpeg releases its open `.ts` segment (otherwise it stays locked, is left behind, and keeps writing during the wipe) and stops competing for the disk.
  2. `KeyStore.Destroy()` ("Destroying encryption key…") — **crypto-erase**: destroying the at-rest key makes every encrypted recording, snapshot, and file instantly unrecoverable on any device/OS regardless of size. This is the real secure-wipe guarantee; the steps below reclaim space and scrub plaintext residue.
  3. `eraseMedia(roots)` — fast `os.RemoveAll` of all media roots (unlink is near-instant, so footage is out of the filesystem before any slow step and survives an interrupt).
  4. `bootstrap.Reset` — drop + restore the database and factory defaults, guaranteed before the slow scrub.
  5. `scrubFreeSpace(roots)` — best-effort, time-budgeted device-appropriate secure overwrite of the *freed* space: per distinct volume `recording.TrimVolume` (TRIM/discard — the correct flash erase) then a random free-space fill (`recording.ScrubFreeSpace`) for HDDs. Safe to interrupt; the files are already gone.
  6. `Restarter.Restart("factory reset")` after a 1.5s grace so the client can read the final 100%/Restarting state.
- `collectRoots`/`eraseMedia`/`scrubFreeSpace` resolve media roots (snapshot dir, training dir, fileStorage uploads, per-camera recording paths), skipping `"."`/empty so the reset can never wipe the working directory.

## Notes

- This is a security wipe: stage problems never abort. A failed key destroy, an un-erasable file, or a `bootstrap.Reset` error is recorded as a non-fatal `Warning` and the sequence still restarts — and the restart re-runs bootstrap, which can complete a rebuild a transient error interrupted.
- Interfaces kept local so the package doesn't depend on host/crypto wiring: `ProcessRestarter` (`Restart(reason)`, satisfied by `apphost.Restarter`) and `CryptoEraser` (`Destroy()`, satisfied by `atrest.KeyStore` — see `infra/atrest`).
- `shredTimeBudget` caps the secure-overwrite phase so a large library on a near-full disk can't stall the reset.
- **Honest caveat:** on SSD/NVMe (wear-levelling/TRIM) no overwrite method guarantees erasure of original cells — the universal guarantees are the crypto-erase (key destroy) and the instant unlink; TRIM + free-space scrub are defense-in-depth for plaintext residue and HDDs.
- Exposed by `apis/system.go` (`POST /api/system/reset`, `GET /api/system/reset/state`, `GET /api/system/reset/progress`); the POST is admin-write gated and additionally guarded by `allowReset`.
