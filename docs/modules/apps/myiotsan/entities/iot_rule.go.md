# Module: apps/myiotsan/entities/iot_rule.go

## Purpose

Defines a condition over telemetry that fires an alert. A near-1:1 port of mymatasan's
`DetectionRule`, and the correspondence is exact where it matters:

| mymatasan | myiotsan |
|---|---|
| `Threshold` | `Threshold` (the value to compare against) |
| `MinFrames` | `ConsecutiveSamples` (debounce: N in a row before it counts) |
| `CooldownSeconds` | `CooldownSeconds` (do not re-fire for N seconds) |
| `SchedulePolicy` | `SchedulePolicy` (only during these hours) |
| `ZonePolygon` | `DeviceId` / `Tag` (WHICH devices, instead of which pixels) |

The debounce is not decoration: a PIR sensor twitches, a door contact bounces, a temperature
probe spikes for one sample; firing on a single reading means an operator gets woken at 03:00
by a bounce, and an alert channel nobody trusts is worse than no alert channel.

## Fields

- Identity/state: `Id`, `Name`, `Enabled`.
- Scope: `DeviceId` (one device, indexed) or `Tag` (every device carrying it — the replacement
  for mymatasan's per-camera zone: "every door contact on floor 2", written once) — if neither
  is set, every device reporting the key.
- `Key` — the telemetry key watched; required for every condition except `"offline"`, which is
  about the device itself.
- `Condition` — one of `above`, `below`, `equals`, `delta`, `rate`, `stuck`, `offline`. See
  `services.EvaluateCondition`/`conditionMet` (`services/rule_engine.go.md`) for exact meaning.
- `Threshold`.
- `Hysteresis` — widens the gap the value must travel back through before the rule RESETS.
  Without it, a value hovering exactly on the threshold fires, clears, fires, clears — the
  classic flapping alert.
- `ConsecutiveSamples` — the debounce; 0 and 1 both mean "fire on the first".
- `WindowSeconds` — the lookback for `delta` (change over the window), `rate` (change per
  minute across it), `stuck` (no change throughout it), `offline` (nothing heard for this long).
- `CooldownSeconds` — suppresses re-firing. **Survives restart via `LastTriggeredAt`** — the bug
  this port must not re-introduce: mymatasan loaded that field, carried it into the detector,
  and then never read it, so every restart re-armed every rule and produced an alert storm.
- `SchedulePolicy` (`"always"` or `"window"`), `ScheduleStart`, `ScheduleEnd`, `ScheduleDays`
  (comma-separated weekday numbers, 0=Sunday; empty = every day). A loading-bay door opening is
  normal at 09:00 and an intrusion at 03:00.
- `Severity` — `info`/`warning`/`critical`.
- `LastTriggeredAt` (unix seconds) — persisted on every fire and seeded back into the evaluator
  at boot; what makes the cooldown survive a restart.
- Audit fields: created/updated user and timestamps.

## Notes

- `ZonePolygon` from mymatasan's `DetectionRule` is dropped entirely and replaced by
  `DeviceId`/`Tag` scope — there is no equivalent column here.
- The actual cooldown durability is via the **alert log**, not this column alone — see
  `services.RuleService.Reload` (`services/rules.go.md`) for why `LastTriggeredAt` is UI-facing
  while the alert row is the authoritative source re-seeded at boot.
- Bootstrap creates this table from the registered entity when SQLite or another supported DB
  engine starts.
