# Module: apps/myiotsan/services/notification_settings.go

## Purpose

The runtime-editable outbound-delivery config for myiotsan's alerts — webhook and telegram — and
the **first code path that ever wires it up**. Before this file, myiotsan wrote every alert to
its in-app notification feed only; the shared `notification.Service`'s outbound channels started
empty and nothing in the app ever called `Configure`. This service IS that missing call: an
admin edits the config from the Settings > Notifications tab, `Save`/`Sync` push it into the live
hub, and only from that point on does an alert leave the box. The delivery engine itself
(webhook POST, Telegram bot API) is unmodified shared infra (`domain/notification`,
`infra/notification`) — this file is the myiotsan-side plumbing and persistence, not a new
delivery mechanism.

Persisted as one JSON blob in the shared `RuntimeSetting` KV (key `"notification"`) — the same
storage pattern the site location uses (`services/schedules.go.md`'s `GetLocation`/`SetLocation`)
— so no new table.

## Key Type: NotificationSettingsService

```go
type NotificationSettingsService struct {
    repo  dbsql.IGenericRepo[sharedentities.RuntimeSetting]
    notif notifier
}
```

`notifier` is the narrow slice of `*notification.Service` this store needs — `Publish` and
`Configure` — so the store is unit-testable without a real hub (see `settings_test.go.md`).

## Responsibilities

- `NewNotificationSettingsService(db, notif)` — constructor; `notif` is
  the app's already-constructed `notification.Service`.
- `Get(ctx)` — returns the stored `NotificationSettings`, or normalized (disabled) defaults if
  nothing was ever saved.
- `Save(ctx, settings)` — validates (`validateNotifSettings`), persists (create-or-update on the
  KV row), **and applies live**: calls `s.notif.Configure(channelConfig(settings))`. This one
  line is the load-bearing behavior of the whole file — turning "in-app feed only" into "also
  delivers to webhook/telegram".
- `Sync(ctx)` — applies the currently-stored settings to the hub without editing them; called
  once at app startup (`app.go`'s `RegisterAppRoutes`) so a config saved in a previous run, but
  not yet re-applied to this process's fresh hub instance, takes effect on boot rather than
  silently going dark until the next save.
- `Test(ctx, severity)` — publishes a real `notification.Notification` (category `System`) so an
  operator can confirm a channel actually delivers — "saved" is not "reaches my phone". `severity`
  defaults to `warning` via `parseSeverity` if empty/unrecognised.
- `channelConfig(s)` — maps the persisted shape onto `notification.ChannelConfig`, using the
  Webhook/Telegram singleton fields (the shared service converts them to destinations internally
  when `Destinations` is empty) — myiotsan does not need mymatasan's full per-destination model.
- `normalizeNotifSettings` / `validateNotifSettings` — trims whitespace, defaults an unset
  severity floor to `"warning"` (an unconfigured floor should not flood a channel with info-level
  chatter), and refuses to enable a channel missing its required fields (a webhook needs a valid
  `http(s)` URL; telegram needs both a bot token and a chat id).

## Notes

- Wired in `app.go`'s `RegisterAppRoutes`: constructed early (`services.NewNotificationSettingsService(deps.Db,
  notificationService)`), `Sync`'d immediately after construction (a sync failure only logs a
  warning — it must not abort boot), then passed into `apis.NewSettingsApi` alongside
  `TelemetrySettingsService`. See `apis/settings.go.md` for the HTTP surface
  (`GET`/`PUT /api/settings/notification`, `POST /api/settings/notification/test`) and
  `services/rbac.go.md` for the admin-only catalog row.
- Hermetically tested in `settings_test.go` (`settings_test.go.md`): validation rules,
  `channelConfig`'s severity/field mapping, and the default-severity fallback — no database, no
  real notification hub.
- Verified end-to-end on a booted app: saved a webhook config and used the test button to confirm
  `Configure` actually reached the live hub.
