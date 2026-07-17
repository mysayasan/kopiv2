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
func (i *Ingest) SetEnrollment(e *Enrollment)
func (i *Ingest) SetTwin(c *CommandService)
func (i *Ingest) SetFlows(f *FlowRuntime)
func (i *Ingest) Handle(ctx context.Context, p iotmqtt.Principal, clientId, topic string, payload []byte)
func (i *Ingest) HandlePolled(ctx context.Context, dev *entities.IotDevice, samples []codec.Sample)
func (i *Ingest) InvalidateProfile(profileId int64)
func (i *Ingest) Stats() IngestStats
```

`SetEnrollment` wires the enrollment window in (`app.go`, after construction) so a quarantined
client's payloads become candidates instead of telemetry.

`SetTwin` (P4) wires actuation's device twin in. Every reading updates the twin's REPORTED half
— this is what CONFIRMS a command: "we published a message" is not "the relay closed"; only the
device saying so is. See `services/commands.go.md`.

`SetFlows` (Flow Engine) wires the flow runtime in, the same way `SetTwin` does — after
construction (the runtime is built later in `app.go`) and before the broker starts, so no reading
is missed. A nil runtime means the flow engine is simply not consulted. See
`services/flow_runtime.go.md`.

`Handle` satisfies `mqtt.MessageHandler` and is also the entry point future HTTP ingest would
call, so a device that cannot speak MQTT gets the same pipeline behind it.

**THE QUARANTINE is the very first thing `Handle` checks**, before anything else: if
`p.Enrolling`, the payload is handed to `enroll.Observe` (recorded as a `DiscoveredDevice`
candidate) and `Handle` returns — no decode, no deadband, no rule evaluation, no write. This
early return IS the security boundary the whole enrollment design leans on: everything below it
in `Handle` treats the payload as trusted sensor data, and a stranger admitted through an open
window must never reach any of it. See `services/enrollment.go.md`.

For an adopted device (`p.DeviceId`), per message:

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
8. (Flow Engine) `flows.OnReading(ctx, dev, s.Key, s.Num, nowSec)` runs alongside the rules — the
   SAME reading, a parallel consumer. It only ENQUEUES onto the runtime's event channel here
   (execution happens on the runtime's own worker goroutine), so a flow can never slow ingest. See
   `services/flow_runtime.go.md`.
9. (P4) `twin.OnReported(ctx, deviceId, s.Key, s.Num, nowSec)` runs too, regardless of the admit
   decision, unconditionally for every decoded sample — a reading is a fact about the world
   whether or not any command is outstanding on that key. This is the twin's REPORTED half, and
   it is the only thing that can confirm a `sent` `DeviceCommand`. See `services/commands.go.md`.

## Key Function: handleSamples

```go
func (i *Ingest) handleSamples(ctx context.Context, dev *entities.IotDevice, binds *profileBindings, samples []codec.Sample, nowMs, nowSec int64)
```

**(P5)** The BACK HALF of `Handle` (deadband -> storage -> rules -> twin), extracted so a POLLED
device rides it too. It is deliberately protocol- and codec-blind: an MQTT payload and a Modbus
poll both arrive here as a `[]codec.Sample` and are treated identically — steps 5-8 of `Handle`'s
per-message list above, unchanged.

## Key Function: HandlePolled

```go
func (i *Ingest) HandlePolled(ctx context.Context, dev *entities.IotDevice, samples []codec.Sample)
```

**(P5)** The entry point a POLL driver (`services.ModbusPoller`, `modbus_poller.go.md`; later
OPC-UA) calls with a batch it has already decoded — there is no payload to parse, so `Handle`'s
JSON/raw decode step does not apply. **No quarantine either**: a polled device is one the operator
configured and the app dialled OUT to, not a stranger that dialled in, so it skips straight past
where `Handle`'s enrollment check would sit. It still does `TouchSeen` first and unconditionally
(the same liveness rule as the MQTT path — a device polling an unchanged value must not look dead)
and still drops a sample whose key the profile does not declare, exactly as an unbound MQTT field
would be. Everything past that is `handleSamples`, identical to the MQTT path.

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

- **(P5) Two entry points, one back half.** `Handle` (MQTT publish, decode-then-`handleSamples`)
  and `HandlePolled` (Modbus/OPC-UA poll, already-decoded-then-`handleSamples`) converge on the
  identical deadband/storage/rules/twin machinery — a polled inverter gets the same suppression,
  history, alerting and command-confirmation a published door sensor does, for free.
- Wired in `apps/myiotsan/app/app.go`'s ingest-spine assembly (see `app/app.go.md`).
- The "measured 98.2% suppressed, zero dropped" result cited in `docs/MYIOTSAN_PLAN.md` §9 is
  this stats struct's own output from a live load test.
