# Module: apps/myiotsan/services/modbus_poller.go

## Purpose

**(P5)** The POLLED half of ingest, wiring `infra/iot/modbus` (the driver foundation) into the
running app. Everything shipped before P5 PUSHES: a device connects to the embedded broker and
publishes, and `Ingest.Handle` reacts. A Modbus device does the opposite — it sits on a TCP port
and answers only when read — so something has to do the reading, on a schedule. That is this
service: one goroutine per Modbus device, each dialing OUT, decoding (SunSpec auto-discovery for
compliant hardware, or a vendor register map for the rest), and handing the samples to
`Ingest.HandlePolled` — the identical deadband -> storage -> rules -> twin back half a published
reading takes (`ingest.go.md`). This is the app-integration half of `docs/MYIOTSAN_PLAN.md` §8g
items 1-2; the driver itself (`PollOnce`/`Run`/`WriteConfirm`) predates this file and lives in
`infra/iot/modbus/poller.go` (`poller.go.md`).

## Key Type: ModbusPoller

```go
func NewModbusPoller(devices *DeviceService, profiles *ProfileService, ingest *Ingest, logf func(string, ...any)) *ModbusPoller
func (p *ModbusPoller) Run(ctx context.Context, reconcileEvery time.Duration)
```

Inert until `Run` is called (launched under `safego.Supervise` in `app.go`, alongside the offline
and command sweeps). `Run` reconciles immediately, then on a ticker (`modbusReconcileInterval`,
30s in `app.go`) — this is a **reconcile loop, not a static goroutine set**: a Modbus device added,
edited, disabled or deleted in the UI starts, restarts or stops its poller with no process
restart, the same live-config property the rest of the app has. On `ctx` cancellation every
running poller is stopped (`stopAll`).

A poll is idempotent and connectionless — `modbus.Run` (via `PollOnce` internally) dials fresh
each tick, so a device that drops off the bus simply fails one tick and is retried on the next; a
transient outage never needs recovery logic.

## Key Function: reconcile / planFor

```go
func (p *ModbusPoller) reconcile(ctx context.Context)
func (p *ModbusPoller) planFor(ctx context.Context, d *entities.IotDevice) (modbusPlan, bool, error)
```

`reconcile` lists the device inventory, resolves each enabled device to a `modbusPlan` via
`planFor`, then diffs the **desired** set against the **running** set: stops a poller whose device
vanished, was disabled, or whose `sig` (signature) changed, and starts one for anything new or
just-stopped-for-reconfiguration.

`planFor` is where a device + its profile become "how to poll it, or don't":

- `d.ProfileId <= 0`, or the profile's `Transport` is not `"modbus"` → `(_, false, nil)` — not this
  service's business; the broker path (or nothing) owns it. (This `Transport` is the
  `DeviceProfile`'s PUSH-vs-POLL selector — `"mqtt"` vs `"modbus"` — a different field from the
  device-level wire transport described next.)
- An empty `d.Endpoint` on an otherwise-Modbus device is a **config error**, not silently skipped
  — `planFor` returns it and `reconcile` logs-and-skips rather than crashing, the same posture an
  unprofiled MQTT device gets.
- `ModbusMode == "" | "sunspec"` → `modbus.ModeSunSpec`. `"regmap"` → `modbus.ModeRegMap` plus
  `registerMapFromKeys(detail.Keys)`. Anything else is refused as an unknown mode.
- `PollSeconds <= 0` defaults to 5s.
- The built `modbus.DeviceConf` also carries `Transport: modbusTransportOf(d.Transport)` and
  `Serial: modbus.SerialParams{Baud: d.Baud, DataBits: d.DataBits, Parity: ..., StopBits:
  d.StopBits}` — the device's own wire transport (TCP/RTU-over-TCP/serial), so a poll dials the
  device the same way a guarded write does (`services/commands.go.md`'s `sendModbus`).

## Key Function: modbusTransportOf

```go
func modbusTransportOf(s string) modbus.Transport
```

Maps `entities.IotDevice.Transport`'s string (`""`/`"tcp"` → TCP; `"rtutcp"`/`"rtu-tcp"`/
`"rtuovertcp"` → RTU-over-TCP; `"serial"`/`"rtu"` → serial) to the driver's `modbus.Transport`,
case/whitespace-insensitively. Empty or anything unrecognised falls back to `modbus.TransportTCP`
— the historical default, so every device stored before this field existed keeps polling exactly
as it did. Shared with `services/commands.go.md`'s `sendModbus`, so a device's poller and its
guarded command writes never disagree on how to reach it.

**`sig`** is the reconcile signature: `endpoint|unit|mode|base|pollSeconds|profileUpdatedAt|
deviceUpdatedAt`. It deliberately folds in `prof.UpdatedAt` — a register-map profile's keys are
edited through the profile CRUD API, not the device, so a remapped register only shows up in a
running poller if the profile's own timestamp is part of what "unchanged" means.

## Key Function: registerMapFromKeys / ptypeOf

```go
func registerMapFromKeys(keys []*entities.TelemetryKey) (modbus.RegisterMap, error)
func ptypeOf(kind string) (modbus.PType, error)
```

`registerMapFromKeys` builds a `modbus.RegisterMap` (`regmap.go.md`) from a `"regmap"` profile's
telemetry keys, skipping any key with no `RegKind` (a profile may mix Modbus and non-Modbus keys
harmlessly) and refusing outright if NONE carry one — a register-map device with nothing mapped
can never yield a reading, so that is a config error, not an empty result. It now also threads
`k.RegInput` onto each `modbus.Point`'s `Input` field, so a key marked "input register" is read via
fn 4 instead of fn 3. `ptypeOf` maps the profile's string `RegKind`
(`"u16"`/`"i16"`/`"u32"`/`"i32"`/`"f32"`) to the driver's `modbus.PType`, refusing anything else.

## Notes

- Constructed and run in `app.go`'s `RegisterAppRoutes` immediately after the device/profile/
  ingest wiring, under `safego.Supervise` on `modbusReconcileInterval` (30s) — see `app/app.go.md`.
  That interval governs how fast a newly-added device STARTS polling, not the poll cadence itself
  (the profile's own `PollSeconds`).
- Feeds `Ingest.HandlePolled`, not `Ingest.Handle` — there is no MQTT payload to decode and no
  enrollment quarantine to apply; a polled device is one the operator explicitly configured and
  the app dialled out to. See `ingest.go.md`.
- The five shipped Modbus profiles this service can drive — `generic-sunspec-solar` (SunSpec
  auto-discovery), `huawei-sun2000`, `sungrow-sh-hybrid`, `deye-hybrid` (register map), and
  `eastron-sdm630-meter` (register map, input/float32) — are seeded by
  `services.ProfileService.EnsureBuiltins` from `profile_catalog.go.md`.
- Covered by `modbus_poller_test.go` (`modbus_poller_test.go.md`): `ptypeOf` (now including
  `"f32"`), that a SunSpec profile yields no register bindings, an end-to-end decode of the
  `huawei-sun2000` register map against a fake register bank, and
  `TestBuiltinRegmapProfilesBuildValidMaps` — every builtin register-map profile (including the
  three new ones) must build a non-empty, valid `RegisterMap` via `registerMapFromKeys`, catching a
  typo'd `RegKind` or an unmapped key at test time rather than only when a real device is polled.
