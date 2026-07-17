# Module: apps/myiotsan/services/notification_destination.go

## Purpose

The per-DESTINATION outbound-delivery model — ported from mymatasan
(`apps/mymatasan/services/notification_destination.go`) and scaled down to what an IoT hub
actually needs. A destination is one place an alert is delivered (a webhook, a Telegram chat, an
MQTT topic), each with its own severity floor and category filter, and it supersedes the old
single webhook/telegram toggles in `notification_settings.go` — those are now kept only so an
older, pre-destinations config migrates forward once (`migrateLegacyDestinations`).

The vision-only pieces mymatasan's version carries — per-field toggles, snapshot mode, LPR/
template tokens — are deliberately dropped: a sensor alert has no bounding box or plate to
template.

## Key Type: NotificationDestination

```go
type NotificationDestination struct {
    Id          string
    Name        string
    Type        string // webhook | telegram | mqtt
    Enabled     bool
    MinSeverity string
    URL         string                   // webhook target
    BotToken    string                   // telegram
    ChatId      string
    Categories  []string                 // nil/empty = all categories
    Mqtt        NotificationMqttSettings // MQTT publish target
}
```

`NotificationMqttSettings` carries broker URL, topic, client id, QoS, retain, username/password,
and an optional CA/client cert+key trio for a TLS broker — natural for an IoT hub, since an alert
can be republished to a broker topic other systems already watch.

## Responsibilities

- `knownCategories` — the only two categories myiotsan actually publishes:
  `notification.CategoryDeviceAlert` (an IoT rule fired on a reading, including a device going
  offline) and `notification.CategorySystem` (enrollment, actuation, sign-in security, the Test
  button). An empty `Categories` list on a destination means "all of them".
- `(d NotificationDestination) AllowsCategory(category)` — whether this destination should
  receive a notification of the given category; empty list allows everything.
- `migrateLegacyDestinations(s NotificationSettings)` — seeds the destination list from the old
  `Webhook`/`Telegram` singletons the first time (before any destination is ever stored), so an
  operator who configured delivery under the old model does not silently lose it on upgrade. Only
  a channel that is enabled or has something configured is carried across.
- `normalizeDestinations(in)` — trims whitespace, assigns a unique id
  (`newDestinationID`) to any destination missing one or colliding with another, defaults an
  empty name to its type's title (`titleizeType`), normalizes severity, and drops any category
  the hub does not publish via `normalizeCategories` (so a stale category can never silently
  filter out everything).
- `validateDestinations(in)` — refuses an **enabled** destination that cannot deliver: a webhook
  needs a valid `http(s)` URL, telegram needs both a bot token and a chat id, MQTT needs a broker
  URL and topic (and a client cert/key must be provided together, never one alone). A **disabled**
  destination may be half-filled — an operator saving a draft — and is left alone.
- `newDestinationID()` — mints a short `dst-<12 hex>` id from `crypto/rand`; a destination is
  addressable by it for both per-destination save and the delete route.

## Notes

- `NotificationSettings.Destinations` (`notification_settings.go.md`) is the field this type
  populates; `channelConfig` maps every **enabled** destination onto
  `notification.DestinationConfig` (one filtered outbound channel per destination) rather than
  the old fixed webhook/telegram pair.
- `apis/settings.go.md`'s `PUT /api/settings/notification/destination` and
  `DELETE /api/settings/notification/destination/{id}` are the HTTP surface —
  `NotificationSettingsService.SaveDestination`/`DeleteDestination` upsert or remove one
  destination without clobbering the rest.
- Hermetically tested in `settings_test.go` (`settings_test.go.md`):
  `AllowsCategory`, `normalizeDestinations`/`validateDestinations`, and `channelConfig`'s
  destination mapping — no database, no real notification hub.
- Verified end-to-end on a booted app: `PUT` a webhook destination with categories and an MQTT
  destination, `GET` reflected both, the Test button dispatched through the configured
  destinations, `DELETE` removed one and left the other untouched.
