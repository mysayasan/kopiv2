# Module: apps/myiotsan/services/commands.go

## Purpose

Actuation: the point where a bug stops being a wrong number on a chart and becomes a relay that
physically fires. A camera is read-mostly; an IoT device gets **written to**, and a bad write is
dangerous in a way a bad PTZ move is not — it opens a door, trips a breaker, sets a thermostat to
200°C. Every gate `docs/MYIOTSAN_PLAN.md` §3.4 asked for is enforced here, server-side, plus one
the plan did not name.

## The gates, in `Issue`'s order

1. **Read-only by default.** `Issue` refuses unless `dev.ActuationEnabled` (`iot_device.go.md`)
   is explicitly on. Adoption never sets it.
2. **Only declared commands.** `declaration` looks the command name up in the device's profile's
   `ProfileCommand`s (`profile_command.go.md`) and refuses with a specific, configuration-shaped
   error if the device type declares no such command — there is no generic publish-to-any-topic
   path.
3. **Bounds are server-side.** `validateValue` enforces `Kind` against the *declaration*, not
   anything the caller sent: `switch` = 0 or 1 only; `setpoint`/`cct` = within `Min..Max` (a
   setpoint or cct declaring `Min == 0 && Max == 0` refuses every value — an unbounded setpoint on
   a physical device is an omission, not permission); `dimmer`/`position` = a percentage fixed
   `0..100`; `mode` = one of the integer values `Options` enumerates (`modeValues`, itself a
   refusal on empty/malformed `Options`); `color` = a whole number `0..0xFFFFFF`. **The `switch`
   is a `default` case that REFUSES an unrecognised `Kind`** — before the home-automation kinds
   this had no default, so a misconfigured/unknown kind published unvalidated; that hole is
   closed.
4. **Rate-limited.** `lastSent[deviceId]` (mutex-guarded) enforces `minCommandInterval` (2s) —
   the floor a relay's duty cycle needs; something that can chatter it can destroy it.
5. **Admin only.** Not enforced in this file — `services.Policy()` (`rbac.go.md`) denies
   `/api/devices/*/commands` to everyone below admin, and this rule predates the command path
   itself (written in P0).
6. **Audited, including every refusal.** Every `refuse(...)` path still writes a `DeviceCommand`
   row (`Status: "failed"`) and calls `audit(...)` — a notification, wired in `app.go` to
   `notificationService.Publish` under `CategorySystem`/`Warning`. "Somebody tried to unlock the
   front door at 03:00 and was refused" must not be thrown away just because it did not succeed.
   A command is recorded **before** it is published, too — a command that was sent but never
   written down would be a physical action with no audit trail, the worst possible ordering.
   The row also carries `RequestedByName`, supplied by the caller (`apis.actorName(r)`) — a
   command can arrive over the fleet tunnel from a control-plane operator with no local account
   (`actor == 0`), and without the name the audit trail would say "System" switched the relay
   instead of the person it actually was. See `entities/device_command.go.md`.

## Modbus actuation: the write half of the poller

`Issue` supports two transports, decided by the command's own declaration
(`entities.ProfileCommand.Transport`), and **both arrive at the SEND step having passed every gate
above unchanged** — the gates are transport-blind by design:

- `""`/`"mqtt"` (the default, everything above): publishes `renderPayload(decl, value)` to
  `TopicTemplate`, same as before this landed.
- `"modbus"`: `Issue` branches to `sendModbus`, which WRITES a holding register on the device
  addressed by `dev.Endpoint`/`dev.Unit`, over the device's own wire transport
  (`dev.Transport`/`Baud`/`Parity`/`DataBits`/`StopBits`, `iot_device.go.md`), via a guarded-write
  seam (`modbusWrite`, production = `infra/iot/modbus.WriteConfirm` directly, a fake in tests)
  instead of publishing. `sendModbus` builds a `modbus.DeviceConf{Endpoint, Unit, Transport:
  modbusTransportOf(dev.Transport), Serial: ...}` (`modbus_poller.go.md`'s `modbusTransportOf`)
  before writing, so a serial or RTU-over-TCP device is actuated over the identical transport its
  poller reads it on — a control write is not TCP-only any more:
  - `encodeRegister(decl.RegKind, decl.ScaleFactor, value)` turns the human value into the raw
    register word: `raw = round(value / ScaleFactor)` (the read-side scale applied in reverse),
    then range-checks it against the declared `RegKind` (`u16`: `0..65535`; `i16`:
    `-32768..32767`, encoded as its two's-complement `uint16` bit pattern). **Only single-register
    kinds are written** — `u32`/`i32` are refused rather than half-written, since a torn multi-
    register write on real hardware is worse than refusing the command outright.
  - `modbusWriteTimeout` (5s) bounds the write — it is synchronous (a flow's `command` node waits
    on it) and, like every actuation path in this file, **never retried**: `WriteConfirm` writes
    once and only re-*reads* to confirm, so a lost confirmation cannot become a second physical
    write. A failed/unconfirmed write ends the command (`Status: "failed"`), audited, exactly like
    an MQTT refusal.
  - A **successful** guarded write sets `Status: "confirmed"` directly (`ConfirmedAt = now`) —
    stronger than MQTT's `"sent"`, because `WriteConfirm` already read the register back and saw
    the value land; there is no separate wait for a reported reading the way an MQTT command's
    `ConfirmKey` requires. A Modbus command therefore never passes through `"sent"` at all.
  - A device with no `Endpoint` configured refuses before attempting anything ("this device has no
    Modbus endpoint (host:port) to write to").

## The hazard the plan did not name: never auto-retry

Re-sending a relay write is a **second physical action**. If the first one landed but its
confirmation was lost, a retry fires the relay again — the door opens twice — and nothing at
this layer can distinguish that from the first one never arriving. So:

- `confirmTimeout` (30s) bounds how long a `sent` command waits to be confirmed.
- `SweepUnconfirmed` (called every `commandSweepInterval` = 10s from `app.go`) marks anything
  still `sent` past that window `failed`, with `Error` reading plainly: *"the device never
  reported the new state — it may or may not have acted. Not retried automatically: re-sending
  could act twice."* It does **not** resend. The decision is left to a human.
- Verified live with a relay simulator that obeys but never reports back: the relay physically
  switched, the command was recorded `failed`, and exactly ONE command was ever sent.

## The twin

- `setDesired` writes the desired half of a `DeviceAttribute` (`device_attribute.go.md`) after a
  successful `Issue`, stamping `DesiredExpiresAt = now + desiredTTL` (5 minutes). Only commands
  whose declaration has a `ConfirmKey` get a desired-state row — a command with nothing to
  confirm against has no twin to update. **A Modbus command never reaches this call** —
  `sendModbus` returns directly from `Issue`, so a Modbus-transport command has no desired-state
  twin row regardless of `ConfirmKey`; it confirms (or fails) inline instead, which is the
  stronger guarantee `WriteConfirm`'s read-back already gives.
- `OnReported(ctx, deviceId, key, value, nowSec)` is called from `services.Ingest.Handle` for
  **every** decoded sample (see `ingest.go.md`), unconditionally — a reading is a fact regardless
  of whether a command is outstanding. It updates the reported half and, when the reported value
  matches an outstanding (unexpired-or-not — matching is not gated by `DesiredExpiresAt`, only
  *re-application* would be, and this app does not re-apply) desire, clears `HasDesired` and
  confirms the matching `sent` `DeviceCommand` rows via `confirmPending`.
- **Desired state is never re-applied on its own.** `DesiredExpiresAt` exists so a stale desire
  is visibly stale (the twin still returns it, `HasDesired: true`, past expiry) rather than
  either vanishing or being silently re-sent to a device that just reconnected. See
  `device_attribute.go.md` for the door-controller-offline-a-month scenario this exists to
  prevent — nothing in this file re-applies a desire; only `Issue`, driven by a human, does.

## Key Type: CommandService

```go
func NewCommandService(db dbsql.IDbCrud, devices *DeviceService,
    publish func(topic string, payload []byte, retain bool, qos byte) error,
    audit func(ctx context.Context, msg string, data map[string]any),
    metrics telemetry.Metrics,
    logf func(string, ...any)) *CommandService

func (s *CommandService) CommandsFor(ctx, profileId) ([]*entities.ProfileCommand, error)
func (s *CommandService) Issue(ctx, deviceId, req IssueRequest, actor, actorName) (*entities.DeviceCommand, error)
func (s *CommandService) OnReported(ctx, deviceId, key, value, nowSec)
func (s *CommandService) SweepUnconfirmed(ctx)
func (s *CommandService) History(ctx, deviceId, limit, offset) ([]*entities.DeviceCommand, uint64, error)
func (s *CommandService) Twin(ctx, deviceId) ([]*entities.DeviceAttribute, error)
```

`publish` is `broker.Publish` (`infra/iot/mqtt`, wired in `app.go`) — the same embedded broker
devices connect to; a command is just another MQTT publish from the app's perspective. `modbusWrite`
(unexported field, `modbusWriteFunc`) is the guarded-write seam for the Modbus transport —
`type modbusWriteFunc func(conf modbus.DeviceConf, reg int, value uint16, timeout time.Duration)
error`, taking a whole `DeviceConf` (not a bare endpoint/unit pair) so a guarded write inherits the
device's transport (TCP/RTU-over-TCP/serial) exactly as a poll does. `NewCommandService` wires it
directly to `modbus.WriteConfirm` (`infra/iot/modbus`) — the seam's signature is now exactly that
function's, so no wrapper closure is needed in production; tests substitute a fake, same pattern
as `publish`. `audit` is wired to `notificationService.Publish`, `logf` to
`deps.Logger.Infof`. `metrics` is `deps.Metrics` (nil-safe) — `countCommand(outcome)` increments
`MetricCommandsTotal` (`myiotsan_commands_total`, `services/metrics.go.md`) at each of the three
terminal sites: `refused` in `Issue`, `confirmed` in `confirmPending`, `failed` in
`SweepUnconfirmed`. Counted directly rather than sampled, unlike the ingest gauges — commands are
rare (rate-limited, human-triggered), so a labelled counter per call site costs nothing. The
inline Modbus path counts too: `sendModbus` calls `countCommand("confirmed")` on a confirmed
write-with-read-back and `countCommand("failed")` on its `fail` path, so `myiotsan_commands_total`
covers Modbus outcomes at parity with MQTT.

## Home-automation kinds (dimmer, position, cct, mode, color)

Added alongside `scenes`/`schedules` to make lamps, blinds and thermostats commandable, not just
relays/setpoints. None of it introduces a new authority — every kind still goes through `Issue`'s
same six gates above:

- `packRGB(r, g, b)`/`unpackRGB(v)` carry an RGB colour through the single-float command/audit
  model: `0xRRGGBB` (max 16,777,215) is exactly representable in a `float64` mantissa, so the
  `DeviceCommand.Value`/twin-equality machinery needs no change for this kind. A `color` command
  typically declares no `ConfirmKey` — a bulb that reports colour back per-channel cannot be
  equality-confirmed against one packed float, so "sent, never confirmed" is the honest status
  rather than a fabricated confirmation.
- `modeValues(options string) ([]float64, error)` parses a `mode` command's `Options`
  (`[{"value":int,"label":string}]`) to its allowed values; empty or malformed `Options` is a
  refusal, the same "an omission means no" rule the unbounded setpoint follows.
- `renderPayload` now also substitutes `{r}`/`{g}`/`{b}` (from `unpackRGB`) when `decl.Kind` is
  `color`, alongside the `{value}` substitution every kind gets.
- See `entities/profile_command.go.md` for the full `Kind` list and `services/profile_catalog.go.md`
  for `smart-lamp`, the shipped worked example.

## Notes

- `renderPayload`/`trimNum` are pure helpers: `{value}` (and, for `color`, `{r}`/`{g}`/`{b}`)
  substitution and clean (no scientific notation, no float noise) numeric rendering — a relay
  that receives `"1e+00"` instead of `"1"` does nothing, silently. Covered by
  `commands_test.go.md`.
- Wired in `app.go`'s `RegisterAppRoutes`: constructed after `deviceService`/`broker`/
  `notificationService`, then `ingest.SetTwin(commandService)` and a `safego.Supervise`d sweep
  loop. See `app/app.go.md`.
- Exposed over HTTP by `apis/commands.go.md`. Also depended on as `commandIssuer` (the one-method
  `Issue` interface) by `services/scenes.go.md` and, through `SceneService`, `services/schedules.go.md`
  — a scene or a schedule fans out through this exact same gated entry point, never a shortcut
  around it.
