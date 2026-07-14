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
3. **Bounds are server-side.** `validateValue` enforces `Kind` (`switch` = 0 or 1 only;
   `setpoint` = within `Min..Max`) against the *declaration*, not anything the caller sent. A
   setpoint declaring `Min == 0 && Max == 0` refuses every value — an unbounded setpoint on a
   physical device is an omission, not permission.
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
  confirm against has no twin to update.
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
devices connect to; a command is just another MQTT publish from the app's perspective. `audit` is
wired to `notificationService.Publish`, `logf` to `deps.Logger.Infof`. `metrics` is `deps.Metrics`
(nil-safe) — `countCommand(outcome)` increments `MetricCommandsTotal` (`myiotsan_commands_total`,
`services/metrics.go.md`) at each of the three terminal sites: `refused` in `Issue`, `confirmed`
in `confirmPending`, `failed` in `SweepUnconfirmed`. Counted directly rather than sampled, unlike
the ingest gauges — commands are rare (rate-limited, human-triggered), so a labelled counter per
call site costs nothing.

## Notes

- `renderPayload`/`trimNum` are pure helpers: `{value}` substitution and clean (no scientific
  notation, no float noise) numeric rendering — a relay that receives `"1e+00"` instead of `"1"`
  does nothing, silently. Covered by `commands_test.go.md`.
- Wired in `app.go`'s `RegisterAppRoutes`: constructed after `deviceService`/`broker`/
  `notificationService`, then `ingest.SetTwin(commandService)` and a `safego.Supervise`d sweep
  loop. See `app/app.go.md`.
- Exposed over HTTP by `apis/commands.go.md`.
