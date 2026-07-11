# Module: apps/myseliasan/apis/notifications.go

## Purpose

Exposes the myseliasan control plane's unified notification feed — node-pushed events (AI alerts, health notices, going-offline) that nodes have published up the control channel.

## Endpoints

Both routes require a myseliasan session + accessrbac middleware.

| Method | Path | Notes |
|---|---|---|
| `GET` | `/api/notifications` | Paginated list. `?nodeId=ID` scopes to one node's events. `?unread=true` filters to unread only. `?limit=` / `?offset=` for pagination (default limit 100). Returns `{items, total}`. |
| `GET` | `/api/notifications/stream` | Server-Sent Events feed for live notification delivery to the dashboard. |
| `POST` | `/api/notifications/{id}/read` | Mark one notification read, clearing it from the unread count/badge. The feed has a single shared unread state (not per-user) — a read here is a read for every operator. Returns the updated notification. |

## Notes

- Notifications are stored in the shared `notification` entity table and published by `ingestNodeEvent` in `app.go` when the control channel server receives a node-pushed event frame.
- All endpoints are protected by `auth.Middleware` + `session.Middleware` (the shared `AccessSessionMidware`).
