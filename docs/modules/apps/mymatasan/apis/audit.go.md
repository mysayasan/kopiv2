# Module: apps/mymatasan/apis/audit.go

## Purpose

mymatasan's audit surface: the read/export routes for the trail, plus `Auditor` — the value every audited handler in this app records through.

mymatasan shipped without an audit log, which was the worst place in the suite for the gap. myidsan and myseliasan both recorded who did what; mymatasan is the app that holds the actual video. `DELETE /api/recording/segments/{id}` destroyed footage and recorded no actor, no reason and no timestamp — and neither did viewing or downloading it. RBAC already stops an operator from reaching the delete route (an operator who was present at an incident must not be able to destroy the evidence of it), so the threat model was understood; there was simply no record when an administrator did it.

## Endpoints

Mounted on the protected router, governed by the role permission matrix (`services/rbac.go` lists `/api/audit` as admin-only, and `services/pages.go` gives it its own `PageAudit` entry):

- `GET /api/audit` — newest-first listing, filterable by `action`, `outcome`, `actor`, `targetType`, `targetId`, `from`, `to`.
- `GET /api/audit.csv` — the same listing as a CSV download, for an auditor who wants it outside the product. Capped at `auditCSVMaxRows` (10000); when the result is truncated the response carries `X-Audit-Truncated` with the true total, because a silently shortened export is worse than a bounded one.

**There is deliberately no delete route and no update route, not even for a superadmin.** The value of the trail is that the person whose actions it records cannot edit it, and an endpoint that trimmed it would remove exactly that property.

## Auditor

`NewAuditor(serv, trustedProxyCIDRs)` builds the recording helper; `Record` / `Success` / `Failure` are what handlers call.

- **The trusted-proxy list comes from the same config block the rate limiter uses**, so "which hops may set `X-Forwarded-For`" has one answer in this app. Without it `middlewares.ClientIP` ignores the header entirely — an untrusted caller must not be able to forge the address recorded against their own deletion.
- **A nil `Auditor` records nothing and does not panic.** Auditing must never be able to fail the action being audited, and that has to hold for a mis-wired composition root too.
- `auditActor` reads the principal the auth middleware left in the request context (mymatasan authenticates with its own local session, not a JWT). An unauthenticated request cannot reach an audited route, so the zero return is defensive — and the action is still recorded, with the actor blank, because an unattributed entry beats a missing one.

## What is recorded

Weighted deliberately toward **evidence handling** rather than configuration, because that is what an investigation asks about and what a tender asks to see:

| Action | Where | Note |
|---|---|---|
| `recording.view` / `recording.download` | `apis/recording.go` `downloadSegment` | Recorded once per playback. A ranged request is a scrubbing `<video>` element, so only the opening range is recorded — otherwise seeking through one clip writes dozens of rows and buries the trail |
| `recording.delete` | `deleteSegment` | The row is read BEFORE the delete, so the camera and time range are in the entry; afterwards "recording 412 was deleted" is not an answer to "what footage did we lose" |
| `recording.purge` | `purgeExpired`, `purgeCameraNow` | |
| `recording.config_change` | `saveConfig` | Carries retention before/after — shortening retention is a slower way of deleting footage |
| `camera.credential_change` | `apis/camera.go` `saveCredentials` | The username is recorded, the password never is |
| `user.create` / `user.update` / `user.delete` | `apis/settings.go` | |

`user.update` records the RESULTING role rather than a before/after pair: the shared `ILocalUserService` has no by-id read, and widening an interface three apps implement just to decorate an audit entry is the wrong trade. "This user now holds role N, set by this actor at this time" is the security-relevant fact.

## Notes

- Backed by `domain/shared/audit` — see `docs/modules/domain/shared/audit/service.go.md` for the append-only contract, the truncation caps and retention.
- The action vocabulary and target-type constants live in `apps/mymatasan/services/audit.go`.
- Metric names are `mymatasan_audit_write_failures_total` / `mymatasan_audit_retention_purged_total`, attached in the composition root via `services.WithAuditMetrics`. The write-failure counter matters more here than most metrics: `Record` swallows its own errors by design, so it is the only symptom a trail that stopped recording produces.
- Covered by `audit_recording_test.go`: deleting footage is attributed (actor, role, IP, user agent) and captures the camera + window before deletion; a forged `X-Forwarded-For` is ignored; viewing is recorded; mid-clip seeks are not each recorded; a nil `Auditor` does not panic; and a compile-time guard that the trail interface exposes no update or targeted-delete path.
- **Not yet live-benched.** The UI for reading the trail is not built — the routes and the page grant exist, but no screen renders them yet.
