# Module: infra/recording/shred.go

## Purpose

Securely deletes recorded footage so it cannot be trivially recovered from disk, replacing a plain unlink with a multi-pass overwrite-then-unlink. Pure `os`/`io`, so it behaves identically on Windows, Linux, and macOS.

## Responsibilities

- `ShredFile(path, passes)` — overwrite the file's contents with random data `passes` times (syncing after each pass), truncate it, rename it to a random name (to defeat filename recovery), then remove it.
- `SecureRemove(path, passes)` — shred when `passes > 0`, otherwise a plain `os.Remove`; a missing file (or empty path) is treated as success, so it is a safe drop-in for `os.Remove` on best-effort cleanup paths.
- `DefaultShredPasses` (3) — the pass count used when shredding is enabled but no explicit count is configured.

## Notes

- Best-effort only: on SSDs (wear-levelling remaps blocks), copy-on-write filesystems (ZFS/Btrfs/APFS), and snapshotted/journalled volumes, the original blocks may survive an overwrite. Full-disk encryption is the only real guarantee — this is documented next to the `recording.shred` setting.
- A single 1 MiB random buffer is reused for the length of each pass (fast, still random per pass).
- Wired into retention purge (`rtsp.go purgeOldFiles`) and the recording service's `PurgeOldSegments` / `DeleteSegment` via the `RecorderConfig.ShredPasses` / service `shredPasses` fields. Intermediate `.ts` files removed during remux are NOT shredded (the data persists in the kept `.mp4`).
- Also used by the factory reset (`services.SystemResetService`) to shred all media before the database drop.
