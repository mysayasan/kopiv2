# Module: apps/myidsan/services/audit_retention.go

## Purpose

Implements `IAuditService.PurgeOlderThan` (see `services/audit.go.md`) — the single,
config-file-only, age-based, archive-first exception to the audit trail's otherwise total
lack of a delete path.

## Responsibilities

- `ActionAuditPurge = "audit.retention_purge"` — the action name the purge records itself
  under, in the same trail it just trimmed, so a reader who finds history starting abruptly
  can tell "this was trimmed on purpose, and here is where the rest lives" apart from
  "nothing happened before this date".
- `PurgeResult` — `Archived`, `Deleted` (a mismatch between the two is worth logging: it
  means something else wrote to the table mid-purge), `ArchivePath` (empty when nothing was
  old enough to purge), `Cutoff`.
- `(*auditService) PurgeOlderThan(ctx, maxRetentionDays, archiveDir) (PurgeResult, error)`:
  1. Refuses `maxRetentionDays <= 0` outright.
  2. Computes `cutoff = now - maxRetentionDays` and pages rows older than it oldest-first,
     `auditArchiveBatch` (500) rows at a time, so a multi-million-row first purge does not
     have to fit in memory. Paging is by **offset**, not by id, which is safe only because
     rows are never removed until the very end — the window cannot shift underneath the
     paging mid-run.
  3. Writes each row as one JSON line to a `.audit-archive.partial` temp file (`0o600`)
     under `archiveDir` (created `0o700` if missing).
  4. `fsync`s the temp file, closes it, then renames it to its final name,
     `audit-through-id<highestId>-<cutoff YYYYMMDD>.jsonl` — named for the highest archived
     row id and the cutoff date so it is both unique without a clock read and self-describing
     before it's opened.
  5. **Only after** the rename succeeds does it call `repo.Delete` with the same age filter.
     Any failure before the rename (paging error, disk full, permissions) returns early
     having deleted nothing — a failed run costs a retention cycle, never history. A failure
     in `Delete` itself is also surfaced (the error names the archive path) rather than
     swallowed, since the archive is already durable and the caller may want to retry. On a
     successful delete, adds the deleted count to `MetricAuditRetentionPurgedTotal` (when a
     metrics recorder is attached, and only when it deleted at least one row) — see
     `services/metrics.go.md`.
  6. On a successful delete, calls `s.Record` with `ActionAuditPurge`, `Outcome: success`,
     and `Metadata: {cutoff, maxRetentionDays, archived, deleted, archiveFile, highestId}`.
     This entry is newer than the cutoff it just computed, so it always survives the run
     that wrote it (and every later one).
  7. Returns an empty, no-error `PurgeResult` when there is nothing old enough to archive —
     a no-op run is not logged as a purge.

## Notes

- Constructed nowhere directly — it is a method on the existing `auditService`
  (`services/audit.go.md`), so `NewAuditService`'s one construction site in `app/app.go.md`
  is the only place that needs to change if the signature ever does.
- Called only from `apps/myidsan/app/audit_retention.go.md`'s `startAuditRetention`, on a
  periodic scheduler task, never inline with a request.
- Covered by `apps/myidsan/services/audit_retention_test.go` against a hand-written fake
  repo (archive-then-delete ordering, nothing deleted when the archive directory cannot be
  written, the archive kept and named in the error when `Delete` itself fails, multi-batch
  paging past `auditArchiveBatch`, a no-op when nothing is expired, refusing a non-positive
  `maxRetentionDays`) and `apps/myidsan/services/audit_retention_sqlite_test.go` (the same
  behaviour against a real bootstrapped SQLite database: a `CreatedAt` cutoff one day inside
  the 30-day window is kept — guarding the off-by-one that would otherwise eat a recent row
  — expired rows round-trip through the archive with their content intact, and a second
  back-to-back run with nothing newly expired is a clean no-op rather than re-archiving or
  deleting the first run's own `audit.retention_purge` record).
