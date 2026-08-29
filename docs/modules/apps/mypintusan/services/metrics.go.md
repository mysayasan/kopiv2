# Module: apps/mypintusan/services/metrics.go

## Purpose

The Prometheus series this appliance emits beyond the shared API middleware's, and the help text
that makes a scrape readable by somebody who did not write the code.

## Why it exists

`domain/shared/audit`'s `Record` **swallows its own write errors on purpose** — auditing must never
fail the action being audited, and on this appliance that action may be a person standing at a door.
The consequence is that a trail which has stopped recording produces **no symptom at all**:
everything else stays green while the record of who changed the rules quietly develops a hole.

That is the whole reason this file exists. The principle the suite already recorded once —
*instrument what fails silently* — has no better example.

## Series

| name | meaning |
|---|---|
| `mypintusan_audit_write_failures_total` | administrative-trail entries that could not be persisted. **The only symptom a broken trail produces.** |
| `mypintusan_audit_retention_purged_total` | trail rows removed by age-based retention |

Naming follows the suite: `kopiv2_*` for app-neutral shared infra, `mypintusan_*` for this app's own.
The names are passed into the shared service rather than fixed there, because the suite's existing
series are app-prefixed and renaming a shipped metric silently breaks whatever dashboard or alert is
watching it.

## Usage

`DescribeMetrics(deps.Metrics)` is called once from `app/app.go` at startup;
`WithAuditMetrics` in `services/audit.go` binds the names to the shared recorder.

## Related

`services/audit.go.md`, `app/audit_retention.go.md`, `infra/telemetry`.
