# Module: apps/mypintusan/apis/notifications.go

## Purpose

Registers the unified notification feed under `/api/notifications`: door alarms (duress, tamper,
forced/held-open, reader offline), badge **decisions** (grant/deny/operator unlock, published by
`services/alarm.go.md`'s `NotificationAlarmer.Decision`), and the app's own security events (a
sign-in lockout) — one place an operator looks, and the endpoint the fleet control plane replays
from after a disconnect.

## Responsibilities

- `NewNotificationsApi(router, svc)` mounts:
  - `GET /notifications` — paged, filterable by `unread`, `category`, `source`. When the query
    carries `since=<unix seconds>`, this instead becomes the **replay pull**: it calls
    `notification.Service.ListSince(ctx, since, limit)` and returns that notification's
    oldest-first feed from `since`, ignoring `unread`/`category`/`source` — this is the endpoint
    `myseliasan`'s fleet control plane calls over the control tunnel on reconnect to catch up on
    notifications this node published while its control channel was down (see
    `docs/modules/apps/myseliasan/app/app.go.md`'s "Replay on reconnect"). Without it, anything
    raised while the control channel was down would silently never reach the parent.
  - `POST /notifications/{id}/read` — mark read; the actor is the signed-in local user
    (`notificationActorId`), for the read-marker trail.
  - `GET /notifications/stream` — server-sent events; the feed updates live rather than being
    polled, via `notification.Service.StreamHandler()`.

## Notes

- Thin binding over `domain/notification.Service`, the same package every appliance uses.
- `readPaging`/`readNotificationID`/`notificationActorId` are small local helpers, not shared —
  each appliance's `apis` package defines its own copies against its own `apis` types
  (`sharedapis.LocalUserFromContext`), matching the pattern in
  `docs/modules/apps/myiotsan/apis/notifications.go.md`.
- Wired in `apps/mypintusan/app/app.go.md`'s `RegisterAppRoutes` (`apis.NewNotificationsApi(protected, notifications)`), gated in the RBAC catalog by `services/rbac.go.md` (`/api/notifications` viewer-readable, `/api/notifications/*/read` operator-writable).
- The events that actually populate this feed for this app are the alarms raised by
  `services/alarm.go.md`'s `NotificationAlarmer.Raise` (door-state incidents) and its new
  `Decision` method (badge grants/denials/operator unlocks, published at `Info` severity). Both
  flow up the fleet control channel once the app is adopted — see
  `apps/mypintusan/app/wire_fleet.go.md`.
