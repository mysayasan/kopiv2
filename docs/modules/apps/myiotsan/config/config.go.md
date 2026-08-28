# Module: apps/myiotsan/config/config.go

## Purpose

Owns myiotsan's own configuration blocks (`mqtt`, `telemetry_store`), decoded via the phase-C
per-app config seam (`apphost.AppConfigDecoder`): the shared `AppConfigModel` carries what
every app needs (server, db, security, auth), and each app decodes its OWN blocks from the same
raw document. myiotsan's blocks stay TOP-LEVEL in `config.json` — not nested under an `"app"`
key — so a deployed config file does not have to be rewritten to gain them.

## Key Type: Config

```go
func Load(raw []byte) (*Config, error)
```

`Config{MQTT MQTTConfig, Telemetry TelemetryConfig}`. `Load` unmarshals (a nil/empty `raw` is
valid and yields a working appliance under defaults — the shipped behaviour, not a degraded
one) then calls `normalize()`.

## Key Type: MQTTConfig

- `Enabled` — turns the embedded broker on. A site that already runs Mosquitto/EMQX can turn it
  off and point its devices at that instead (a future connect mode) — the ingest pipeline does
  not care where a payload came from.
- `Addr` — the plaintext listener; defaults to `"0.0.0.0:1883"`, the standard MQTT port.

## Key Type: TelemetryConfig

Tunes the storage path — write batching and retention, the knobs that decide whether the
appliance keeps up:

- `BatchSize` (default 200), `FlushMs` (default 250) — the write-behind batcher's transaction
  size and max buffering delay.
- `QueueSize` (default 8192) — the buffer depth before load is shed (readings dropped, not
  blocked, past this).
- `RawRetentionDays` (default 30) — how long individual readings survive.
- `RollupRetentionDays` (default 400, over a year) — how long the downsampled buckets survive;
  longer than the raw rows so last summer stays comparable to this one.
- `RollupIntervalMs` (default 1 hour, unchanged shipped cadence) — how often the rollup-and-
  retention worker (`services.TelemetryService.RunRollup`) runs. Deliberately here rather than in
  the runtime-editable Telemetry settings screen (`services/telemetry_settings.go.md`): it is a
  maintenance cadence, not an operator tuning knob, and a background job with no way to make it
  run is a job nobody has ever watched do its work — on every bench of this app before this field
  existed, the rollup worker had never run once. Not present in the shipped `config.json`/
  `config.dev.json` (unlike the other `telemetry_store` fields above), so both fall through to
  the 1-hour `normalize()` default; a bench harness sets it explicitly (5s) to exercise the
  worker on a short run.

## Notes

- Feeds `services.NewReadingWriter` (`ReadingWriterOptions`) and `services.TelemetryService`'s
  `RetentionConfig` in `apps/myiotsan/app/app.go`'s `RegisterAppRoutes`.
- `apps/myiotsan/config.json`/`config.dev.json` both ship the same defaults explicitly
  (`mqtt.enabled: true`, `mqtt.addr: "0.0.0.0:1883"`, and the `telemetry_store` block) — the
  explicit values in the shipped files and this file's `normalize()` defaults intentionally
  agree, with one deliberate exception: `rollupIntervalMs` is left out of both shipped files, so
  it always resolves through `normalize()`'s 1-hour default rather than a value an installer
  could accidentally shorten.
