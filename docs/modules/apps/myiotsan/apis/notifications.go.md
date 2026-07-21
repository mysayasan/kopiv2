# Module: apps/myiotsan/apis/notifications.go

## Purpose

Registers the unified notification feed under `/api/notifications`: rule alerts, device
health, and the app's own security events (a sign-in lockout) — one place an operator looks.

## Responsibilities

- `NewNotificationsApi(router, svc)` mounts:
  - `GET /notifications` — paged, filterable by `unread`, `category`, `source`. When the query
    carries `since=<unix seconds>`, this instead becomes the **replay pull**: it calls
    `notification.Service.ListSince(ctx, since, limit)` and returns that notification's
    oldest-first feed from `since` (capped 500), ignoring `unread`/`category`/`source` — this is
    the endpoint `myseliasan`'s fleet control plane calls over the control tunnel on reconnect to
    catch up on notifications this node published while its control channel was down (see
    `docs/modules/apps/myseliasan/app/app.go.md`'s "Replay on reconnect").
  - `POST /notifications/{id}/read` — mark read.
  - `GET /notifications/stream` — server-sent events; the feed updates live rather than being
    polled, via `notification.Service.StreamHandler()`.

## Notes

- Thin binding over `domain/notification.Service`, the same one mymatasan uses; myiotsan's rule
  engine publishes into it with `Category: notification.CategoryDeviceAlert`
  (`infra/notification/types.go.md`).
