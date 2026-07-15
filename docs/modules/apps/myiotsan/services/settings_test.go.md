# Module: apps/myiotsan/services/settings_test.go

## Purpose

Hermetic tests (no database, no real notification hub) for the validation and mapping logic in
`notification_settings.go` and `telemetry_settings.go` — the parts of both new settings stores
that are pure functions and worth pinning directly, rather than only exercising through a live
boot.

## Responsibilities

- `TestNotifSettings_ValidationRequiresChannelFields` — a disabled channel needs nothing; an
  enabled webhook requires a valid `http`/`https` URL (rejects empty, rejects `ftp://`); an
  enabled telegram requires both a bot token and a chat id.
- `TestNotifSettings_ChannelConfigMapsSeverityAndFields` — `channelConfig` correctly carries
  URL/token/chat-id and maps a severity string (`"critical"`, `"info"`) onto the
  `notification.Severity` enum; an empty/unrecognised severity floors at `notification.Warning`,
  not `Info` — an unconfigured floor should not flood a channel.
- `TestTelemetrySettings_DefaultsFillZeros` — `withTelemetryDefaults` keeps an explicitly-set
  field (`RawRetentionDays: 7`) while filling every zero/blank field (`BatchSize`, `QueueSize`,
  `MqttAddr`, `RollupRetentionDays`) from the supplied defaults — a partial saved blob (or one
  saved before a field existed) never silently becomes 0.
- `TestTelemetrySettings_Validation` — a sane config validates; zero retention days, a broker
  address without `host:port`, and a zero batch size are each individually rejected.

## Notes

- Calls the unexported `validateNotifSettings`/`normalizeNotifSettings`/`channelConfig`/
  `parseSeverity`/`withTelemetryDefaults`/`validateTelemetrySettings` functions directly — no
  `NewNotificationSettingsService`/`NewTelemetrySettingsService`, no `RuntimeSetting` repo, no
  `notification.Service`.
- The database-dependent half of both stores (`Get`/`Save`/`Sync`/`Test` against a real KV row,
  and `Save`'s live `Configure` call reaching an actual hub) is exercised live instead — see
  `app/app.go.md`'s Settings-page verification note (created a user+role, set location, saved and
  tested a webhook, saved telemetry retention).
