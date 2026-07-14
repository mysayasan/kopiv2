# Module: apps/myiotsan/apis/notifications.go

## Purpose

Registers the unified notification feed under `/api/notifications`: rule alerts, device
health, and the app's own security events (a sign-in lockout) — one place an operator looks.

## Responsibilities

- `NewNotificationsApi(router, svc)` mounts:
  - `GET /notifications` — paged, filterable by `unread`, `category`, `source`.
  - `POST /notifications/{id}/read` — mark read.
  - `GET /notifications/stream` — server-sent events; the feed updates live rather than being
    polled, via `notification.Service.StreamHandler()`.

## Notes

- Thin binding over `domain/notification.Service`, the same one mymatasan uses; myiotsan's rule
  engine publishes into it with `Category: notification.CategoryDeviceAlert`
  (`infra/notification/types.go.md`).
