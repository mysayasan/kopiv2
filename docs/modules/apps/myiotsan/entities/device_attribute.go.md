# Module: apps/myiotsan/entities/device_attribute.go

## Purpose

The device twin: what an operator asked for (desired) and what the device says is actually true
(reported), one row per `(DeviceId, Key)`. The gap between the two is the only honest answer to
"is the door locked?" — "we sent a lock command" is not an answer; "the device reports it is
locked" is. See `services/commands.go.md` (`OnReported`/`setDesired`/`Twin`) and
`docs/MYIOTSAN_PLAN.md` §3.4.

## Fields

- `DeviceId`, `Key` (unique together, `ukey:"dev_key"`) — one twin row per device attribute.
- `Desired`/`HasDesired`/`DesiredAt` — what was last asked for, and when.
- `DesiredExpiresAt` — when the desire stops being actionable (5 minutes after issue; see
  `desiredTTL` in `commands.go`). **Past it, an expired desire is not re-applied.** This is the
  hazard the twin pattern otherwise invites: the obvious implementation re-applies desired state
  whenever a device reconnects, which is fine for a light bulb and dangerous here — a door
  controller offline for a month would come back and immediately apply a month-old "unlock"
  somebody issued for thirty seconds during a delivery, with nobody watching. The twin still
  **shows** the disagreement past expiry — an operator must see that what they asked for never
  took effect — it is simply no longer acted on. If the state still needs changing, an operator
  re-issues the command, which takes five seconds and is exactly the moment a human should be in
  the loop anyway.
- `Reported`/`HasReported`/`ReportedAt` — what the device last said is true. **The only field
  that describes physical reality**; `Desired` describes an intent.

## Notes

- Written from two places: `CommandService.setDesired` (the desired half, on every successful
  `Issue` that has a `ConfirmKey`) and `CommandService.OnReported` (the reported half, called
  from `services.Ingest.Handle` for every decoded sample — see `ingest.go.md`'s `SetTwin`/twin
  wiring). A reading is a fact about the world regardless of whether any command caused it, so
  `OnReported` runs unconditionally, not just when a command is outstanding.
- When a report matches an outstanding desire, `OnReported` also marks the matching `sent`
  `DeviceCommand` rows `confirmed` (`confirmPending`) — this is the only path that can, since
  "the relay closed" is a fact only the device can report.
- Bootstrap creates this table from the registered entity (`app/app.go`'s `Entities()`).
