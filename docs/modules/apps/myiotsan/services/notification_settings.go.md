# Module: apps/myiotsan/services/notification_settings.go

## Purpose

The runtime-editable outbound-delivery config for myiotsan's alerts, now **per-destination**
(webhook/telegram/mqtt — `notification_destination.go.md`) rather than a fixed webhook+telegram
pair, and the **first code path that ever wires delivery up at all**. Before this file, myiotsan
wrote every alert to its in-app notification feed only; the shared `notification.Service`'s
outbound channels started empty and nothing in the app ever called `Configure`. This service IS
that missing call: an admin adds one or more destinations from the Settings > Notifications tab,
`Save`/`Sync` push them into the live hub, and only from that point on does an alert leave the
box. The delivery engine itself (webhook POST, Telegram bot API, MQTT publish) is unmodified
shared infra (`domain/notification`, `infra/notification`) — this file is the myiotsan-side
plumbing and persistence, not a new delivery mechanism.

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

## Key Type: NotificationSettings

```go
type NotificationSettings struct {
    Webhook      WebhookSettings           // legacy singleton, migration only
    Telegram     TelegramSettings          // legacy singleton, migration only
    Destinations []NotificationDestination // authoritative — see notification_destination.go.md
}
```

The `Webhook`/`Telegram` singletons are kept only so an older, pre-destinations config migrates
forward once (`migrateLegacyDestinations`); live delivery reads `Destinations`.

## Responsibilities

- `NewNotificationSettingsService(db, notif)` — constructor; `notif` is
  the app's already-constructed `notification.Service`.
- `Get(ctx)` — returns the stored `NotificationSettings`, or normalized (disabled) defaults if
  nothing was ever saved.
- `SaveDestination(ctx, dest)` — upserts **one** destination against the currently-persisted
  settings (not a client-supplied full blob), so saving one destination never clobbers another's
  stored config: an empty `dest.Id` appends a new one (minting an id via `newDestinationID`);
  a non-empty id replaces the matching entry, or appends if it no longer exists. Delegates to
  `Save` for validation/persistence/apply, then returns the destination as actually stored (with
  its final id) plus the full settings.
- `DeleteDestination(ctx, id)` — removes one destination by id, leaves the rest untouched,
  delegates to `Save`.
- `Save(ctx, settings)` — validates (`validateNotifSettings`), persists (create-or-update on the
  KV row), **and applies live**: calls `s.notif.Configure(channelConfig(settings))`. This one
  line is the load-bearing behavior of the whole file — turning "in-app feed only" into "also
  delivers to every enabled destination".
- `Sync(ctx)` — applies the currently-stored settings to the hub without editing them; called
  once at app startup (`app.go`'s `RegisterAppRoutes`) so a config saved in a previous run, but
  not yet re-applied to this process's fresh hub instance, takes effect on boot rather than
  silently going dark until the next save.
- `Test(ctx, severity)` — publishes a real `notification.Notification` (category `System`) so an
  operator can confirm a channel actually delivers — "saved" is not "reaches my phone". `severity`
  defaults to `warning` via `parseSeverity` if empty/unrecognised.
- `channelConfig(s)` — maps every **enabled** destination in `s.Destinations` onto a
  `notification.DestinationConfig` (its own severity floor, category subscription, and
  type-specific fields — URL for webhook, bot token/chat id for telegram, the full
  `NotificationMqttSettings` for mqtt) and appends it to `notification.ChannelConfig.Destinations`.
  A disabled destination is skipped entirely.
- `normalizeNotifSettings` / `validateNotifSettings` — trims whitespace, defaults an unset
  severity floor to `"warning"` on the legacy singletons (an unconfigured floor should not flood a
  channel with info-level chatter), seeds `Destinations` from the legacy singletons the first
  time via `migrateLegacyDestinations` (`notification_destination.go.md`) if the list is still
  empty, then normalizes/validates the destination list itself
  (`normalizeDestinations`/`validateDestinations`) — that is what actually enforces a webhook's
  `http(s)` URL, telegram's bot token + chat id, or MQTT's broker URL + topic.

## Notes

- Wired in `app.go`'s `RegisterAppRoutes`: constructed early (`services.NewNotificationSettingsService(deps.Db,
  notificationService)`), `Sync`'d immediately after construction (a sync failure only logs a
  warning — it must not abort boot), then passed into `apis.NewSettingsApi` alongside
  `TelemetrySettingsService`. See `apis/settings.go.md` for the HTTP surface
  (`GET`/`PUT /api/settings/notification`, `PUT /api/settings/notification/destination`,
  `DELETE /api/settings/notification/destination/{id}`, `POST /api/settings/notification/test`)
  and `services/rbac.go.md` for the admin-only catalog row.
- See `notification_destination.go.md` for the destination model itself (`NotificationDestination`,
  `NotificationMqttSettings`, category filtering, normalize/validate, id minting).
- Hermetically tested in `settings_test.go` (`settings_test.go.md`): validation rules,
  `channelConfig`'s destination mapping, and the default-severity fallback — no database, no
  real notification hub.
- Verified end-to-end on a booted app: `PUT` a webhook destination with categories and an MQTT
  destination, `GET` reflected both, the test button dispatched through the configured
  destinations (Configure applied), `DELETE` removed one destination and left the other intact.
