# Module: apps/myiotsan/services/telemetry_settings.go

## Purpose

The runtime-editable storage/broker configuration behind the Settings > Telemetry tab: raw and
rollup retention days, the write-batcher's batch size/flush interval/queue size, and the embedded
MQTT broker's listen address. Persisted as one JSON blob in the shared `RuntimeSetting` KV (key
`"telemetry"`), seeded from `config.json`'s `mqtt`/`telemetry_store` blocks the first time.

**The one thing to understand about this file: it has no live-apply side effect, unlike its
sibling `NotificationSettingsService`.** Every field here is consumed once, at process
construction — the write-batcher is sized when built, the retention loop closes over its config
at start, the MQTT listener binds once — so `Save` only persists; an edit takes effect on the
next **restart** (`apis/system.go.md`'s `POST /api/system/restart`). The UI says so plainly and
links straight to the restart button.

## Key Type: TelemetrySettings

```go
type TelemetrySettings struct {
    RawRetentionDays    int
    RollupRetentionDays int
    BatchSize int
    FlushMs   int
    QueueSize int
    MqttAddr  string
}
```

## Key Type: TelemetrySettingsService

```go
type TelemetrySettingsService struct {
    repo     dbsql.IGenericRepo[sharedentities.RuntimeSetting]
    defaults TelemetrySettings
}
```

## Responsibilities

- `NewTelemetrySettingsService(db, defaults)` — `defaults` comes from the app's already-decoded
  `config.json` (`appCfg.Telemetry.*`, `appCfg.MQTT.Addr`), so a fresh install's stored blob (once
  one exists) always starts from the shipped config, not a second hardcoded set of numbers.
- `Get(ctx)` — returns the stored settings if present, else `defaults`; **this is what `app.go`
  reads once at boot** to feed `services.NewReadingWriter`, `telemetry.RunRollup`, and
  `iotmqtt.New` — the "effective" config for this run.
- `Save(ctx, settings)` — fills any zero/blank field from `defaults` (`withTelemetryDefaults`, so
  an older or partial blob never silently becomes a 0 batch size or a 0-day retention), validates
  (`validateTelemetrySettings`: retention days ≥ 1, batch/queue/flush > 0, broker address must
  contain `:`), and persists. **Does not call anything live** — the caller (the Settings UI)
  restarts to apply.

## Notes

- Wired in `app.go`'s `RegisterAppRoutes`: constructed with `appCfg.Telemetry`/`appCfg.MQTT.Addr`
  as defaults, then `Get(ctx)` is called immediately (`effTelemetry`) and its result — not
  `appCfg.Telemetry` directly — feeds `NewReadingWriter`'s batch/flush/queue options,
  `telemetry.RunRollup`'s retention config, and `iotmqtt.New`'s listen address. This is the
  store-over-config pattern: `config.json` is now only the seed for the *first* boot; every boot
  after a save reads the KV-stored value instead.
- Passed into `apis.NewSettingsApi` alongside `NotificationSettingsService`; see
  `apis/settings.go.md` for the HTTP surface (`GET`/`PUT /api/settings/telemetry`) and
  `services/rbac.go.md` for the admin-only catalog row.
- Hermetically tested in `settings_test.go` (`settings_test.go.md`):
  `withTelemetryDefaults` fills zeros from defaults without clobbering an explicitly-set field,
  and `validateTelemetrySettings` rejects zero retention, a non-`host:port` broker address, and a
  zero batch size.
