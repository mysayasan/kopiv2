# Module: apps/myiotsan/entities/device_reading.go

## Purpose

Defines one persisted telemetry sample. This is the hot table — everything else in the schema
is small and this one grows forever — so it is deliberately narrow.

Rows exist only for samples that **passed the deadband** (`TelemetryKey.Deadband`). This is
therefore not a record of every packet the broker saw; it is a record of every time something
changed. That distinction is the storage design (see `telemetry_key.go.md`,
`services/deadband.go.md`).

## Fields

- `Id`, `DeviceId` (required).
- `Key` — the telemetry key name, **denormalized** from `TelemetryKey` on purpose: a reading
  must stay readable after its profile is edited or its key deleted. History does not get to
  become unreadable because somebody renamed a field.
- `Ts` — the reading's time in unix **milliseconds**. Sub-second resolution matters: two sensor
  transitions inside one second are two different events.
- `Num`/`Str` — a numeric or boolean reading (bool as 0/1), or a string reading. Which is
  meaningful is decided by the key's `DataType`.
- `Suspect` — marks a reading outside the key's declared `Min`/`Max`. **Stored, not dropped**: a
  sensor reporting nonsense is itself the finding, and discarding it silently would hide a
  failing device.

## Indexes

`idx:"dev_key_time"` spans `DeviceId`, `Key`, `Ts` — what every read wants: "this device's
temperature over the last day", the rollup worker's next batch. Without it, charting a month of
data is a full scan of the largest table in the database.

## Notes

- Written exclusively through `services.ReadingWriter`'s batched insert
  (`apps/myiotsan/services/reading_writer.go.md`), never one row at a time on the ingest path.
- Read back by `services.TelemetryService` (`apps/myiotsan/services/telemetry.go.md`) for
  charts, latest values, and the rollup/retention workers.
- Bootstrap creates this table from the registered entity when SQLite or another supported DB
  engine starts.
