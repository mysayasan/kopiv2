# Module: apps/myiotsan/apis/settings.go

## Purpose

Registers everything the Settings page's tabs need under `/api/settings`: user and role
management (closing a real gap — myiotsan's policy catalog, `services/rbac.go.md`, has named
`/api/settings/users` and `/api/settings/roles` since P0, and the `viewer`/`operator`/`admin`
roles have existed since then, but **nothing served these routes**, so viewer and operator were
unassignable and the appliance was effectively single-admin — found and closed in P4), and, new
with the tabbed Settings page, the outbound **notification** delivery config and the
**telemetry**/broker storage knobs. A catalog that names routes the app does not serve is a lie
an operator would rely on — exactly the thing the catalog exists to prevent.

Runs on the **shared** appliance user service (`domain/shared/services`) — the same code
`mymatasan` uses — so bcrypt handling, sessions, and the last-admin guard are one implementation,
not two. The notification/telemetry surface is thin HTTP over the two new sibling services,
`services.NotificationSettingsService` (`services/notification_settings.go.md`) and
`services.TelemetrySettingsService` (`services/telemetry_settings.go.md`).

## Responsibilities

- `NewSettingsApi(router, users sharedservices.ILocalUserService, roles sharedservices.IAccessRoleService, notif *services.NotificationSettingsService, telem *services.TelemetrySettingsService)`
  mounts, all under `/settings`:
  - `GET /roles` — the roles an admin may assign (`viewer`/`operator`/`administrator`).
  - `GET /users`, `POST /users` — list / create a local user
    (`sharedservices.CreateLocalUserRequest`).
  - `PUT /users/{id}` — edit (`sharedservices.UpdateLocalUserRequest`); the shared service
    refuses an edit that would remove the last administrator — an appliance nobody can administer
    is a bricked appliance.
  - `DELETE /users/{id}` — remove.
  - `POST /users/{id}/password` — reset a user's password.
  - `GET`/`PUT /notification` — read/save the outbound webhook/telegram delivery config
    (`services.NotificationSettings`). `PUT` both persists and immediately applies the config to
    the live notification hub via `notif.Save` (see the service doc for why this is the
    load-bearing call).
  - `POST /notification/test` — publishes a test notification at an optional `?severity=` query
    param, so an operator can confirm a channel actually delivers rather than trusting "saved".
  - `GET`/`PUT /telemetry` — read/save the storage retention and broker knobs
    (`services.TelemetrySettings`). Unlike `/notification`, **saving here does not apply
    anything live** — every field is read once at boot (see `app.go.md`), so an edit takes
    effect only after a restart (`apis/system.go.md`'s `POST /system/restart`).
- `requireAdmin` is a **self-gate on top of the matrix**: `services.Policy()` already denies these
  routes to viewer and operator, but this is defence in depth on the one surface in the app that
  can mint an account, rewire where alerts leave the box, or resize the storage/broker knobs.

## Notes

- Mounted in `app.go`'s `RegisterAppRoutes` right after `sharedapis.NewLocalAuthApi`, on the same
  `protected` subrouter (auth + RBAC matrix already applied).
- `services.Policy()` already listed `/api/settings/users`/`/api/settings/roles` as admin-only,
  no-grants rows since P0 — this file is what finally makes that catalog entry true rather than
  aspirational. The rbac catalog also now lists `/api/settings/notification` and
  `/api/settings/telemetry` explicitly (`services/rbac.go.md`).
- Before this change, myiotsan's outbound delivery (`notification.Service.Configure`) was never
  called from anywhere in the app — every alert landed only in the in-app feed. `saveNotification`
  is the first code path that ever wires a webhook or telegram destination for myiotsan.
