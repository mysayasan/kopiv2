# Module: apps/mypintusan/app/audit_retention.go

## Purpose

Schedules the age-based trim of the administrative trail.

## Default: OFF, and that is the right way round here

A door controller is a small box with a small disk, so unbounded growth is a real cost. But the
trail is the only record of who changed who may enter the building, and a controller that silently
forgot last year's grant edits would be worse than one that filled its disk — **the disk announces
itself and the missing history does not**. So it does nothing unless an operator switches it on in
`config.json`.

```json
"audit": { "enabled": true, "maxRetentionDays": 365, "frequencyHours": 24, "archiveDir": "audit-archive" }
```

`maxRetentionDays` is clamped up to the suite's floor (`config.MinAuditRetentionDays()`), with a
warning: a security trail shorter than that answers very little.

## Why turning it on cannot become a way to edit the trail

`PurgeOlderThan` is shaped so that the append-only property survives it:

- it takes an **age**, not a selection of rows;
- it **archives to a file and flushes it** before it deletes anything, and deletes nothing if the
  archive could not be written;
- it **records its own run inside the trail it trimmed** (`audit.retention_purge`), so the boundary
  of the record is itself part of the record.

It is reachable from configuration and from nowhere else. There is no API for it, and adding one
would hand the person the trail is about a button that edits it.

## Scheduling

- The first tick fires after one **interval**, not at boot, so a restart loop cannot become a purge
  loop.
- A failed run is logged and retried on the next tick rather than escalated: a retention failure
  must never take a door controller down.
- The **leader check is asked per tick**, so a change of leadership takes effect without a restart.
  This appliance is single-instance by design (the OSDP bus owns its serial port — see
  `apis/deployment.go.md`), so it always holds the lease. The guard is there because the purge both
  deletes rows and writes the archive of what it deleted to a **local** disk: if this app ever runs
  beside another, two copies racing would leave the trail split across hosts with a gap in each,
  discovered only by somebody who needed it for an investigation.

## Related

`services/audit.go.md`, `apis/audit.go.md`, `domain/shared/audit/retention.go`,
`infra/config/audit_retention.go`.
