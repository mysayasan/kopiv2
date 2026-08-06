# Module: apps/myseliasan/apis/notifications.go

## Purpose

Exposes the myseliasan control plane's unified notification feed — node-pushed events (AI alerts, health notices, going-offline) that nodes have published up the control channel.

## Endpoints

All routes require a myseliasan session + accessrbac middleware.

| Method | Path | Notes |
|---|---|---|
| `GET` | `/api/notifications` | Paginated list. `?nodeId=ID` scopes to one node's events. `?cameraId=N` additionally scopes to one camera **server-side** (0 = no camera filter) — added so a caller never has to over-fetch a node's whole feed and filter client-side, which silently dropped a camera's events once they fell past the page limit. `?unread=true` filters to unread only. `?limit=` / `?offset=` for pagination (default limit 100). Returns `{items, total}`. |
| `GET` | `/api/notifications/stats` | Aggregated dashboard payload (bucketed counts/breakdowns) over myseliasan's own notifications table, for the control-plane landing **Dashboard** tab. `?from=`/`?to=` are unix seconds (default: last 7 days ending now); `?bucket=hour\|day` selects the timeseries granularity (default `day`); `?tzOffset=` is the viewer's timezone offset in minutes (clamped to ±840) so buckets align to local time. Calls `INotificationService.Stats`; the per-node breakdown is `Stats.BySource` keyed `"node:<id>"`. |
| `GET` | `/api/notifications/baseline` | Expected-activity band for each bucket of the events-over-time chart, matching its granularity (`?bucket=hour\|day`). Rollup-backed (`domain/notification.RollupMaintainer` — see `services/rollup_cursor.go.md`); empty until the first sweep lands. `?from=`/`?to=`/`?tzOffset=` same contract as `stats`. `?nodeId=` (new) narrows the band to one node's own learned baseline (translated to `source: "node:<id>"` — per-*source*, not per-camera: origin camera ids collide across nodes, so the per-camera narrowing mymatasan offers would still silently merge unrelated cameras here, and is still refused). Omitting `?nodeId=` returns the fleet-wide band, summed across every source including rows folded before the per-source dimension existed. A per-node band stays "learning" (never flags, never bounds) until enough source-split rollup history accumulates for that node — see `domain/entities/notification_rollup.go.md`'s upgrade note. Backs the Dashboard/Insight chart's expected band and the AI digest's `baseline_spike`/`baseline_quiet`/`node_baseline_spike`/`node_baseline_quiet` findings (`services/agent_findings.go.md`). Same endpoint mymatasan already exposes (fleet-wide only there — mymatasan has no `?nodeId=` concept). |
| `GET` | `/api/notifications/tally` | Compact per-`(source, cameraId)` notification counts + worst severity, aggregated server-side (`domain/notification.Service.Tally`) so the fleet map can badge node/building/camera markers without paging every raw notification row to the browser. `?unread=true` restricts to the unread feed (the map's use). Returns `[]TallyRow{source, cameraId, count, severity}`. |
| `GET` | `/api/notifications/stream` | Server-Sent Events feed for live notification delivery to the dashboard. |
| `POST` | `/api/notifications/{id}/read` | Mark one notification read, clearing it from the unread count/badge. The feed has a single shared unread state (not per-user) — a read here is a read for every operator. Returns the updated notification. |

## Notes

- Notifications are stored in the shared `notification` entity table and published by `ingestNodeEvent` in `app.go` when the control channel server receives a node-pushed event frame.
- All endpoints are protected by `auth.Middleware` + `session.Middleware` (the shared `AccessSessionMidware`).
- `app.go` now also folds the feed into hourly rollups (`domain.NotificationRollup`,
  `notification.RollupMaintainer`) and runs a config-driven retention purge
  (`notification.retentionDays`/`notification.purgeIntervalHours`, 0 keeps everything) —
  see `app/app.go.md`'s "Notification rollups and retention purge" and
  `services/metrics.go.md`'s `MetricNotificationsPurgedTotal`. This is the same analytics
  substrate mymatasan already ran; only the `baseline` HTTP surface above and the purge/rollup
  wiring were new to myseliasan.
