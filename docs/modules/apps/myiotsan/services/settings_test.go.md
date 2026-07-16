# Module: apps/myiotsan/services/settings_test.go

## Purpose

Hermetic tests (no database, no real notification hub) for the validation and mapping logic in
`notification_settings.go`, `notification_destination.go`, and `telemetry_settings.go` — the
parts of both new settings stores that are pure functions and worth pinning directly, rather than
only exercising through a live boot.

## Responsibilities

- `TestNotifSettings_ValidationRequiresChannelFields` — a disabled channel needs nothing; an
  enabled webhook requires a valid `http`/`https` URL (rejects empty, rejects `ftp://`); an
  enabled telegram requires both a bot token and a chat id. (Exercises the legacy singleton path,
  still validated in case an older client `PUT`s the whole blob.)
- `TestNotifSettings_ChannelConfigMapsDestinations` — `channelConfig` maps only **enabled**
  destinations through (a disabled webhook destination in the input is absent from the output),
  carries URL/token/chat-id and a destination's `Categories`, and maps a severity string
  (`"critical"`, `"info"`) onto the `notification.Severity` enum; an empty/unrecognised severity
  floors at `notification.Warning`, not `Info` — an unconfigured floor should not flood a channel.
- `TestNotifDestination_AllowsCategory` — a destination with an empty `Categories` list allows
  every category; one with a list allows only its members.
- `TestNotifDestination_NormalizeAndValidate` — `normalizeDestinations` assigns every destination
  a unique id, defaults a typeless/nameless destination to `webhook`/`"Webhook"`, and drops an
  unknown category (`"bogus.cat"`) while keeping known ones; `validateDestinations` refuses an
  enabled webhook with no URL and an enabled MQTT destination with no topic, but allows a
  **disabled** half-filled destination (a draft).
- `TestTelemetrySettings_DefaultsFillZeros` — `withTelemetryDefaults` keeps an explicitly-set
  field (`RawRetentionDays: 7`) while filling every zero/blank field (`BatchSize`, `QueueSize`,
  `MqttAddr`, `RollupRetentionDays`) from the supplied defaults — a partial saved blob (or one
  saved before a field existed) never silently becomes 0.
- `TestTelemetrySettings_Validation` — a sane config validates; zero retention days, a broker
  address without `host:port`, and a zero batch size are each individually rejected.

## Notes

- Calls the unexported `validateNotifSettings`/`normalizeNotifSettings`/`channelConfig`/
  `parseSeverity`/`normalizeDestinations`/`validateDestinations`/`withTelemetryDefaults`/
  `validateTelemetrySettings` functions directly — no `NewNotificationSettingsService`/
  `NewTelemetrySettingsService`, no `RuntimeSetting` repo, no `notification.Service`.
- The database-dependent half of both stores (`Get`/`Save`/`SaveDestination`/`DeleteDestination`/
  `Sync`/`Test` against a real KV row, and `Save`'s live `Configure` call reaching an actual hub)
  is exercised live instead — see `app/app.go.md`'s Settings-page verification note (created a
  user+role, set location, saved and tested a webhook, saved telemetry retention) and
  `notification_destination.go.md`'s live-verification note (`PUT` a webhook + an MQTT
  destination, `GET` reflected both, `DELETE` removed one).
