# Module: apps/mypintusan/services/doorstate.go

## Purpose

`DoorMachine` — what happens to the physical door after a decision has been made. The decision
path (`services/decision.go.md`) answers "may this person come in?"; this answers the questions
that follow and that nobody asks until something goes wrong: did the door actually open, is it
still open, did it open when nobody was granted, and when should the strike drop. Driven by an
injected clock rather than a timer goroutine, so wall-clock-dependent behaviour (a door propped
at the exact held-open threshold, a REX pressed one second before the shunt expires) is testable
without sleeping.

## Responsibilities

- `DoorState` (`DoorLocked`/`DoorUnlocked`) and `DoorEventKind` (`EvUnlocked`/`EvRelocked`/
  `EvForced`/`EvHeldOpen`/`EvClosed`/`EvFreeAccessBegan`/`EvFreeAccessEnded`), both with
  `String()`. `DoorEvent{Kind, At, Detail}` is the machine's output shape, turned into alarms
  and `entities.AccessEvent`s by `services.Controller.emitDoorEvents`.
- `NewDoorMachine(door)` — starts locked and closed; `ShuntSeconds` (the window after a grant or
  REX during which an open door is expected, not forced) defaults to
  `max(door.UnlockSeconds, 5) + 10`; `RelockOnClose` defaults `true`.
- `Grant(now, seconds)` — unlocks after an access decision has granted (`seconds` from the
  decision, so an accessibility extension applies without the machine needing to know why); sets
  `firstPersonIn = true`, which is what promotes an armed perimeter/critical free-access
  schedule (see `SetFreeAccessSchedule`). Refuses under lockdown even though the decision path
  already denies there too — a second enforcement point so no other caller (an operator
  override, a flow, a future API) can route around it.
- `RequestToExit(now)` — shunts the forced alarm only; does NOT unlock. On the fail-secure
  reference door, egress is the inside lever retracting the latch mechanically, so software's
  only job on a REX press is to stop treating the resulting opening as a break-in — driving the
  strike from REX would put egress on the software path, which the life-safety constraint
  forbids.
- `ContactChanged(now, open)` — the door-position feed. An opening is EXPECTED (no alarm) if the
  door is `DoorUnlocked`, a grant/REX shunt is still live, or a free-access schedule is active;
  anything else raises `EvForced`. A close clears both `forcedActive`/`heldActive` and, if
  `RelockOnClose` and not on free access, relocks immediately — the mechanism that stops a
  second person following the first through a generous strike time.
- `Tick(now)` — advances timers: relocks on unlock-timer expiry (unless on free access), and
  raises `EvHeldOpen` once `HeldOpenSeconds` has elapsed since the door opened, independent of
  whether the opening was legitimate — a door propped after a valid entry is the commonest way a
  secure door stops being one. Idempotent; calling it more often costs nothing, less often
  delays an alarm.
- `SetFreeAccessSchedule(now, on)` — the scheduled-unlock overlay. Gated by
  `requiresFirstPersonIn()` (perimeter/critical): the schedule does not stand the door open until
  one valid holder has actually arrived (`Grant` sets `firstPersonIn`), so a holiday nobody
  entered in the calendar does not leave the front door open all day. Turning it off clears
  `firstPersonIn`, so re-entering the window requires a fresh first person. Lockdown outranks a
  schedule turning on.
- `SetLockdown(now, on)` — overrides every grant and schedule: ends free access and relocks an
  open door. Coming OUT of lockdown does not restore free access on its own — a perimeter door
  has to earn it again with a live first person, since the building was sealed and whoever was
  inside may not still be. Explicitly does not touch egress.
- `ContactBound()` — reports whether `Door.ContactDeviceKey` is set. A door with no contact can
  report that it energised the strike but not that anyone went through: no forced alarm, no
  held-open alarm, no evacuation roster. Meant as a capability gap to surface in the UI, not
  degrade silently.

## Notes

- Pure state machine, no I/O, no goroutine of its own — `Tick`/`ContactChanged`/etc. are called
  by `services.Controller` (`services/controller.go.md`).
- `ContactChanged` and `Tick`'s held-open detection are exercised in `doorstate_test.go`, but
  **`Controller.ContactChanged` is never invoked in a real deployment** — see
  `services/controller.go.md`'s Notes.
- Covered by `doorstate_test.go`.
