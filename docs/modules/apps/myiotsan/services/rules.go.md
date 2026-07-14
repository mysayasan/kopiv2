# Module: apps/myiotsan/services/rules.go

## Purpose

Owns the `IotRule` records, drives the `RuleEngine` evaluator, and turns a firing `Decision`
into a persisted `AlertEvent` plus a notification. Caches the rule set in memory, indexed
implicitly by key inside `rulesFor`, because `OnReading` is called for **every** decoded sample
of every device — re-reading rules from the database per sample would put a query on the hot
path, the one thing the ingest design refuses to do.

## Key Type: RuleService

```go
func NewRuleService(db dbsql.IDbCrud, engine *RuleEngine, series *TelemetryService, notify *notification.Service, devices *DeviceService, logf func(string, ...any)) *RuleService
```

### Reload — the cooldown-durability seam

```go
func (s *RuleService) Reload(ctx context.Context) error
```

Refreshes the in-memory rule cache AND re-seeds every (rule, device) cooldown from the database.
Skipping the seeding is the alert storm mymatasan actually shipped: without it, the cooldown
resets on every restart and a reboot re-fires every rule that is still true.

**The cooldown is seeded from the ALERT LOG, not from a column on the rule** — this is the
correct source, not a shortcut: an alert row already records exactly when a rule last fired on a
given device, so the log IS the durable record and there is no second table to keep in step. A
rule-wide `LastTriggeredAt` column cannot express this at all — a tag-scoped rule over ten
fridges needs ten independent cooldowns, and collapsing them into one would let fridge A's
alert suppress fridge B's. `Reload` reads the recent alert tail (2000 rows, newest first) and
calls `engine.SeedCooldown` once per (rule, device) pair — the first (newest) row per pair wins.

### OnReading — evaluated on every sample, including suppressed ones

```go
func (s *RuleService) OnReading(ctx context.Context, dev *entities.IotDevice, key string, value float64, nowSec int64)
```

Called by `Ingest.Handle` for every decoded sample **regardless of the deadband's admit
decision** — this is deliberate and load-bearing: the deadband is a STORAGE decision, a value
sitting 3 degrees over the limit without moving is not worth another row but is absolutely
worth an alert. Gating rules behind the deadband would mean a steady overheat is never alerted
on — "the worst possible bug this app could contain."

For `delta`/`rate`/`stuck` conditions only, looks up the value at the start of the rule's window
via `TelemetryService.ValueAt` — the one place this hot path DOES touch the database, and only
for the minority of rules that use those conditions; a plain threshold rule never does.

`rulesFor(dev, key)` selects rules by scope ladder: named device (`DeviceId > 0`) > tag
(case-insensitive match against `dev.Tag`) > every device reporting the key. `offline` rules are
excluded here — they are driven by `SweepOffline`, never by a reading.

### fire — the alert write

```go
func (s *RuleService) fire(ctx context.Context, rule *entities.IotRule, dev *entities.IotDevice, key string, value float64, reason string, nowSec int64)
```

Writes the `AlertEvent` **straight to disk**, not through the batched `ReadingWriter` — an
alert lost in a buffer during the very crash it was warning about would be worse than useless.
Also persists `rule.LastTriggeredAt` (UI-facing "when did this last fire") from the engine's own
`LastTriggered` — but the DURABLE cooldown source is the alert row just written, not this
column; see `Reload`. Publishes a `notification.Notification` with
`Category: notification.CategoryDeviceAlert` (distinct from `vision.alert` — see
`infra/notification/types.go.md`) when a `notify.Service` is wired.

### SweepOffline

```go
func (s *RuleService) SweepOffline(ctx context.Context)
```

Fires `offline` rules. They cannot be driven by a reading because their whole subject is the
ABSENCE of readings — a device that has gone silent will never call `OnReading` again, which IS
the event. Called on a 1-minute ticker from `app.go`; walks every enabled device, computes
`silent = now - LastSeenAt` (or `now - CreatedAt` if never heard from at all), evaluates every
matching `offline` rule, and on fire also calls `DeviceService.MarkHealth(dev.Id, "offline")`.

### Alerts

`ListAlerts(ctx, deviceId, limit, offset)`, `AckAlert(ctx, id, actor, actorName)`. **There is
deliberately no `DeleteAlert`** — acknowledging is an operator power, deleting is not; the same
evidentiary line mymatasan draws for its own alert log. `actorName` (from `apis.actorName(r)`) is
stamped onto `AlertEvent.AckedByName` — an ack can arrive over the fleet tunnel from a
control-plane operator with no local account (`actor == 0`), and without the name the alert log
would record "System" as having acknowledged it.

## Key Type: SaveRuleRequest / validConditions

`validate(req)` rejects an unknown `Condition` (the closed set: `above`/`below`/`equals`/
`delta`/`rate`/`stuck`/`offline`) — "an unknown condition would silently never fire, and a rule
that silently never fires is the most dangerous thing a monitoring product can contain: the
operator believes they are covered." Also requires a `Key` for every condition except
`offline`, and a `WindowSeconds` for `delta`/`rate`/`stuck`/`offline`.

`Create`/`Update`/`Delete` each call `Reload` afterward so the cache and cooldown seeding stay
current; `Update`/`Delete` also call `engine.Forget(id)` first — a debounce counter half-filled
against the OLD thresholds means nothing under new ones, and could fire immediately on edit.

## Notes

- `severityOf` maps the rule's string severity onto `notification.Severity`
  (`critical`/`info`/default `warning`).
