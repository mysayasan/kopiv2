# Module: infra/vision/cooldown.go

## Purpose

Makes a detection rule's cooldown survive a process restart. Cooldown state
(`lastTriggered map[int64]int64`, keyed by rule ID) lives only in-process and starts
empty on every boot, so — before this file — every rule's cooldown read as zero
immediately after a restart: a busy scene re-fired the instant `minFrames` was met, and a
crash-restart loop turned a 30-minute cooldown into an alert storm, a notification storm,
and (since every alert extracts a clip) an ffmpeg storm on top.
`DetectionRule.LastTriggeredAt` was already loaded from the database and carried all the
way into the detector — it was simply never read back into the cooldown map (and never
written to, either; see `apps/mymatasan/services/vision.go.md` for the write side,
`IVisionService.MarkRuleTriggered`).

## Responsibilities

- `seedCooldown(lastTriggered, rule)` — the first time this process sees `rule` (no entry
  yet in `lastTriggered`), copies its persisted `LastTriggeredAt` into the in-process map.
  Once the rule fires in-process, the live value takes over and seeding is a no-op for
  that rule for the rest of the process's life. A rule that has never triggered
  (`LastTriggeredAt <= 0`) is not seeded, so it can still fire immediately.
- `cooldownActive(lastTriggered, rule, now, cooldown) bool` — the single cooldown check
  every detector now routes through: seeds first (so no call site can forget to), then
  reports whether `rule` is still inside its cooldown window.

## Notes

- Call sites: `infra/vision/object.go` (`Detect`), `infra/vision/motion.go` (three sites:
  zone motion, motion line-crossing per-line, and end-of-sequence), and
  `infra/vision/line_crossing.go` via `ruleCooldownElapsed(state, rule, now, cooldown)`,
  whose signature takes the whole `DetectionRule` (not just its ID) specifically so it can
  seed.
- Seeding is intentionally one-directional and one-time-per-rule-per-process: the DB value
  never overwrites a live in-process trigger, so a rule that fires again while the process
  is up always uses the fresher in-memory time.
- Persisting the trigger time back to the database (so a *future* restart sees it) is the
  vision monitor's job, not this file's: `apps/mymatasan/services/vision_monitor.go`
  collects the latest trigger time per rule per sample and calls
  `IVisionService.MarkRuleTriggered` once per rule.
- Tests: `cooldown_test.go` — a persisted trigger time keeps a rule cooling across a
  simulated restart (`TestSeedCooldown_CarriesPersistedTriggerTimeIntoAFreshProcess`); live
  in-process state wins over the persisted value once the rule fires again
  (`TestSeedCooldown_LiveStateWinsOverPersisted`); a never-triggered rule is not seeded and
  fires immediately (`TestSeedCooldown_NeverTriggeredRuleFiresImmediately`); and an
  end-to-end regression through a fresh `ObjectRuleDetector`
  (`TestObjectRuleDetector_HonoursPersistedCooldownAfterRestart`).
