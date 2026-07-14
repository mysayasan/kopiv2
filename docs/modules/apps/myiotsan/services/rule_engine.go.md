# Module: apps/myiotsan/services/rule_engine.go

## Purpose

The rule evaluator: pure logic, state in, decision out, no database and no clock of its own.
Everything that made mymatasan's detector painful in production is carried over deliberately
here rather than re-learned: debounce, hysteresis, and a cooldown that survives a restart.

## Conditions

`above`, `below`, `equals`, `delta` (moved more than the threshold across the window), `rate`
(changing faster than the threshold per minute, computed from the REAL elapsed time between
samples, not the nominal window — two samples 8 seconds apart in a 60-second window describe a
much steeper rate than the window implies), `stuck` (the value has NOT moved at all across the
window — a sensor that has frozen reads perfectly healthy to every other condition, which is
exactly why this exists), `offline` (nothing heard from the device for the window — driven by
the offline sweep, not by a reading).

## The three defences against a nuisance alert

- **Debounce** (`ConsecutiveSamples`) — N readings in a row must satisfy the condition.
- **Hysteresis** — once fired, the value must travel back PAST the threshold by this much
  before the rule re-arms (`clearedHysteresis`); without it a value hovering on the threshold
  fires/clears/fires/clears.
- **Cooldown, which SURVIVES RESTART.** This is the bug mymatasan actually shipped:
  `LastTriggeredAt` was loaded from the database, carried into the detector, and then never
  read — so every restart re-armed every rule and produced an alert storm. `SeedCooldown` exists
  so that cannot happen here.

## Key Type: RuleEngine

```go
func NewRuleEngine() *RuleEngine
func (e *RuleEngine) SeedCooldown(ruleId, deviceId int64, lastTriggeredAt int64)
func (e *RuleEngine) Forget(ruleId int64)
func (e *RuleEngine) Evaluate(rule entities.IotRule, obs Observation) Decision
func (e *RuleEngine) LastTriggered(ruleId, deviceId int64) int64
```

State is keyed by `seriesKey{ruleId, deviceId}` — **the device half is load-bearing**. A rule
scoped to a TAG watches every device carrying it ("cold store above 5C" over ten fridges), and
each device must get its own debounce, firing flag and cooldown. Keying by rule alone (the
first version of this code) meant the first fridge to trip the rule set `firing`, and every
other fridge was then silently suppressed as "already firing" — fridge A alerts, fridges B
through J are defrosting and nobody is told. **A missed alert is the one failure a monitoring
product may never have** — worse than a duplicate, worse than a storm. Found by pointing an
offline rule at 20 real devices and watching exactly one of them fire; fixed by adding the
device to the state key, and pinned by `TestRule_TagScopedRuleFiresPerDeviceNotOnce` and
`TestRule_CooldownIsPerDevice` in `rule_engine_test.go`.

`Evaluate` also resets the debounce counter (not the firing/cooldown state) when a rule falls
outside its schedule window — a condition half-satisfied when the window closed must not carry
over and fire on the first sample of the next window.

## Key Type: Observation / Decision

`Observation` — what the evaluator needs for one check: `DeviceId`, `Value`, `Previous`/
`HasPrevious`/`ElapsedSeconds` (for delta/rate/stuck), `SilentSeconds` (for offline), `Now`
(passed in for testability). `Decision` — `Fire`, `Reason` (the human sentence), `Suppressed`
(why a satisfied condition did NOT fire — answers "the value is clearly over the line, why is
there no alert?", otherwise the single most common support question about a rules engine).

## Key Function: withinSchedule

Evaluates `SchedulePolicy`/`ScheduleStart`/`ScheduleEnd`/`ScheduleDays`. Correctly handles a
window whose end is before its start (`22:00`-`06:00`) as spanning MIDNIGHT — precisely when a
security rule is most likely to be wanted; getting this wrong would silently disable overnight
rules, and rules that never fire are the ones nobody notices are broken. Pinned by
`TestRule_ScheduleSpansMidnight`.

## Notes

- `conditionMet` formats the alert `Reason` sentence inline with the predicate, using `num()`
  (trims float noise: `21.4`, not `21.399999999999999`) and `humanSeconds()`.
- Covered extensively by `apps/myiotsan/services/rule_engine_test.go`
  (`rule_engine_test.go.md`).
