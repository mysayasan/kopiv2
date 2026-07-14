# Module: apps/myiotsan/services/ingest.go

## Purpose

The hot path: a payload arrives, and `Ingest` decides what it means, whether it is worth
storing, and whether it should wake somebody up.

    broker -> decode (profile bindings) -> deadband -> batched write
                                                \
                                                 -> rule evaluation -> alert

Everything here is built to keep the database OFF this path: profile bindings are cached,
liveness writes are throttled (`DeviceService.TouchSeen`), readings are batched
(`ReadingWriter`), and the deadband drops most samples before they ever reach the queue. The one
thing allowed to be slow is an alert, because an alert is rare and matters.

## Key Type: Ingest

```go
func NewIngest(devices *DeviceService, profile *ProfileService, gate *DeadbandGate, writer *ReadingWriter, rules *RuleService, logf func(string, ...any)) *Ingest
func (i *Ingest) Handle(ctx context.Context, deviceId int64, clientId, topic string, payload []byte)
func (i *Ingest) InvalidateProfile(profileId int64)
func (i *Ingest) Stats() IngestStats
```

`Handle` satisfies `mqtt.MessageHandler` and is also the entry point future HTTP ingest would
call, so a device that cannot speak MQTT gets the same pipeline behind it. Per message:

1. **`TouchSeen` first, unconditionally.** A device faithfully reporting an unchanged value is
   alive; if this sat behind the deadband, a perfectly healthy stable sensor would look dead to
   the offline rule.
2. Resolves the device and its profile; a device with `ProfileId <= 0` can connect and is
   provably alive, but nothing it says can be decoded — a configuration gap, not an error
   worth spamming per message.
3. Resolves the profile's cached `profileBindings` (`bindingsFor`) — decoded key list, gate
   rules, key metadata, and whether the profile is `"raw"` format.
4. Decodes via `codec.DecodeRaw` (raw, single-binding profiles) or `codec.DecodeJSON`
   (everything else); a malformed JSON payload is logged (bounded — one line per bad payload)
   and dropped.
5. For each decoded `Sample`: a numeric key whose payload was not actually a number (`IsNum`
   false — Zigbee2MQTT's `"unavailable"`) is skipped entirely — see `infra/iot/codec`.
6. `gate.Admit(...)` decides storage. **Critically, a suppressed sample is still evaluated by
   the rules below** — the deadband is a STORAGE decision, not a detection one. A value sitting
   3 degrees over a limit without moving must still fire, and gating rules behind the deadband
   would mean a perfectly steady overheat is never alerted on — deliberately called out as "the
   worst possible bug this app could contain" in the source comment.
7. `rules.OnReading(ctx, dev, s.Key, s.Num, nowSec)` runs regardless of the admit decision.

## Key Type: profileBindings

The per-profile decode plan (`[]codec.Binding`, `map[string]GateRule`,
`map[string]*entities.TelemetryKey`, `raw bool`), cached in `Ingest.bindings` keyed by
`profileId`. Reading `TelemetryKey`s from the database on every message would put a query on
the hot path — the whole thing this pipeline exists to avoid. `InvalidateProfile` drops one
entry so an edited deadband takes effect on the next message, not the next restart.

## Key Function: isSuspect

Flags a reading outside its key's declared `Min`/`Max` range. Stored anyway (see
`device_reading.go.md`) — a sensor reporting -3000 degrees is broken, and dropping the evidence
would hide the failure.

## Key Type: IngestStats

```go
type IngestStats struct {
    Received, Decoded, Stored, Suppressed, Written, Dropped int64
    Queued, Series int
}
```

`Suppressed` is the deadband earning its keep — the ratio of suppressed to stored IS the
storage design; if it ever falls near zero the deadbands are mistuned and the database is about
to be in trouble. `Written`/`Dropped`/`Queued` come from `ReadingWriter.Stats()`; `Series` from
`DeadbandGate.Size()`. Exposed via `GET /api/devices/stats` (`apis/devices.go.md`).

## Notes

- Wired in `apps/myiotsan/app/app.go`'s ingest-spine assembly (see `app/app.go.md`).
- The "measured 98.2% suppressed, zero dropped" result cited in `docs/MYIOTSAN_PLAN.md` §9 is
  this stats struct's own output from a live load test.
