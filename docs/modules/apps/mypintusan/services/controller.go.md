# Module: apps/mypintusan/services/controller.go

## Purpose

Joins the OSDP bus (`infra/access/osdp`) to the decision path: a badge arrives on a reader, a
decision is made, and — if it is a grant — a strike fires. `Controller` is the top of the P1
runtime and the only consumer of the bus's event channel. It also owns lockdown, PIN buffering
(keypad-then-card pairing), and the per-door state machine (`services/doorstate.go.md`).

## Responsibilities

- `Store` — everything the controller reads (readers, doors, credentials, holders, grants,
  schedules, holidays, and `RecordEvent`), as an interface so the SAME controller can run
  against a live database or an offline cached replica — "works offline" is then a property of
  which `Store` is plugged in, not a second code path that can drift from the first. Two
  implementations exist: the in-memory test fake (`controller_test.go`'s `memStore`) and the
  database-backed `SQLStore` (`store_sql.go.md`), built on the shared `dbsql` generic repo. Both
  are exercised against the identical end-to-end scenario (`newRigWithStore` in
  `controller_test.go`), which is what proves them interchangeable. `SQLStore` is now the one
  `apps/mypintusan/app/runtime.go.md` wires into a running process, one `Controller` per
  configured OSDP bus.
- `Actuator.Unlock(ctx, door, seconds, ev)` — the ONE actuation chokepoint. Every unlock (badge,
  operator override, schedule, flow, API) is meant to funnel through one audited implementation,
  exactly as `myiotsan` funnels all actuation through `CommandService.Issue`, so "who opened
  door 3 at 02:14, and through what" stays answerable. The only implementation shipped is
  `BusActuator` (below), which drives the reader's own relay output via a `StrikeResolver`; **the
  `Door.RelayDeviceKey` → myiotsan `CommandService.Issue` path is not implemented.**
- `Alarmer.Raise(ctx, kind, ev, detail)` — alarms that are not access decisions (duress, tamper,
  a reader going out of service, door-forced, door-held-open — the `Alarm*` constants), routed
  separately from the access log because these need to reach a human now.
- `ControllerConfig` — `BusPort`, `Location`, `Offline`/`CacheAge` (describe whether this
  controller runs on a cached replica; a live controller leaves them zero), `Now` (injectable),
  `TickInterval` (default 1s — bounds how LATE a relock/held-open alarm can be, not whether it
  fires), `PINWindow` (default 15s — deliberately SHORT: a PIN left buffered indefinitely would
  be picked up by whoever badges next, misattributing a duress alarm or opening the door on a
  credential that never authenticated), `Decisions` (**new**, nil-safe — when set, told about
  every access decision after `record` writes it, feeding the notification stream the fleet
  control plane correlates across nodes; see below).
- `NewController` / `Machine(ctx, doorId)` — lazily creates and caches one `DoorMachine` per
  door, loaded from `Store.Door`.
- `ContactChanged(ctx, doorId, open)` — the seam that turns "we energised the strike" into
  "somebody actually went through" (door-forced/held-open detection). Two things can feed it and
  for a long time neither did: the **near path**, a contact on the OSDP reader's own supervised
  input, now drives it via `contactReported`; the **far path**, a contact wired to a myiotsan relay
  board (`Door.ContactDeviceKey`), is still unbuilt. Until it was live-benched the state machine,
  the events, the audit rows, the severities and four translations all existed and *nothing called
  this function*, so `door-forced` and `door-held-open` could not fire on any installation.
- `contactReported(ctx, ev)` — maps an `osdp.EventInput` to a door: reader by bus address, then its
  `DoorId`. **Input 0 is the door contact** — OSDP says nothing about what any input means, the
  reference kit wires door position to the first, and the reader profile that would let a site say
  otherwise is not built. A reader reporting no inputs, or not yet bound to a door, is silently
  ignored — that is the state of every reader between being plugged in and being commissioned.
- `markReader(ctx, addr, state, seen)` — persists what the bus just observed onto the reader row,
  best-effort, because failing to update a screen must never interrupt the alarm that prompted it.
  It is the second half of an alarm: `Reader.TamperState` was previously written once as `ok` at
  enrolment and never again, and `LastSeenAt` never at all, so a reader could be offline, alarmed
  and out of service while the readers screen still read "ok, last seen never". `LastSeenAt` is
  deliberately NOT stamped when a reader is declared offline — it was last seen when it last
  answered, not when we gave up on it.
- `RequestToExit` / `SetFreeAccess` / `SetLockdown` / `Lockdown()` — thin wrappers that drive
  `DoorMachine` and turn its returned `DoorEvent`s into alarms/audit rows via `emitDoorEvents`.
  `SetLockdown` seals every door already being tracked, not just future decisions, because a
  door standing open on a free-access schedule would otherwise stay open through a lockdown.
- `ErrUnknownDoor` / `IsUnknownDoor(err)` — the sentinel a caller holding several controllers
  (one per bus, `apps/mypintusan/app/runtime.go.md`) uses to try the next one when a door is not
  on this bus.
- `OperatorUnlock(ctx, door, actor, actorName)` — opens a door from an operator action rather
  than a badge, through the **same** `Actuator` and the **same** `AccessEvent` row shape as a
  badge (`RawCredential: "operator"`), deliberately: a remote unlock is the most abusable
  capability in this application — it opens a door with no credential at all — so it must never
  bypass the audit trail. Checks `ownsDoor` first (`ErrUnknownDoor` if not), then lockdown
  (lockdown outranks an operator — someone who can lift it may open the door, someone who cannot
  must not route around it with the unlock button) and `door.Enabled`, before calling
  `Actuator.Unlock` and telling the `DoorMachine` a legitimate open is coming, exactly like
  `handleCard`'s grant path. Called by `apps/mypintusan/app/runtime.go.md`'s `Unlock`, which in
  turn backs `apis.doorApi.unlock`'s `POST /doors/{id}/unlock`.
- `ownsDoor(ctx, doorId)` — true if the door is already tracked (a `DoorMachine` exists for it)
  or `Store.Door` resolves it; used only by `OperatorUnlock`.
- `Run(ctx)` — drains `bus.Events()` until cancelled (the bus drops events if not drained, and a
  dropped event is a badge that never opened a door) and ticks every `DoorMachine` on
  `TickInterval`, on the same goroutine as event handling so a relock and a badge can never race.
- `handle` dispatches OSDP bus events: `EventOnline`/`EventOffline` update per-reader state
  (offline additionally raises `AlarmReaderOffline` — every door on that reader is now unusable
  and a dashboard nobody is watching would not surface it), `EventStatus` tamper raises
  `AlarmTamper` and records the state on the reader row, `EventInput` goes to `contactReported`,
  `EventFault` raises `AlarmSecureChannel`, `EventKeypad` buffers a PIN, `EventCard` goes to
  `handleCard`.
- `takePIN` consumes (not merely expires) a buffered PIN on read, so it can never be offered to
  a second card — otherwise the next person in the queue could open the door, or trigger a
  duress alarm attributed to them, on someone else's PIN. **This implements PIN-then-card only**:
  a holder types a PIN, then badges. Card-then-PIN — the commoner convention — would need a
  pending-decision state held open awaiting digits, which does not exist; a card requiring a PIN
  with none buffered is safely denied (`ReasonBadPin`) but not the flow a card-then-PIN site
  expects.
- `handleCard` — the badge path: decode (`DecodeCard`) → resolve reader/door → build a
  `Snapshot` (`snapshot`) → `Decide` → on grant, call `Actuator.Unlock` then tell the
  `DoorMachine` a legitimate open is coming (`Machine.Grant`, so the person walking through
  their own grant does not trip a forced-door alarm) → `record` the `AccessEvent` → if `Duress`,
  raise `AlarmDuress` AFTER the door has opened, deliberately silent (no LED/buzzer change) so a
  coercer standing at the reader learns nothing.
- `snapshot` — gathers everything `Decide` needs: reader state (from `handle`'s bookkeeping,
  not asked of the bus mid-flight), the matched credential/holder/grants/schedules, and today's
  holiday resolved in the door's SITE-LOCAL date (a public holiday is a calendar day, not an
  instant; resolving in UTC would shift it near midnight).
- `record` — writes the `AccessEvent`; a `Store.RecordEvent` failure is itself raised as
  `AlarmTamper` ("FAILED TO RECORD ACCESS EVENT"), since losing the log is an incident, not
  merely an error to swallow. **New**: after the write attempt, if `cfg.Decisions != nil` and the
  row has a presented credential (`ev.RawCredential != ""`), it calls `cfg.Decisions(ctx, ev)` —
  this covers badges AND operator unlocks (`RawCredential: "operator"`) but deliberately excludes
  door-state audit rows (forced/held-open have no `RawCredential`; they already raise their own
  alarms via `Alarmer.Raise`). Published even when the audit write above failed — the tamper
  alarm has already surfaced that failure, and the correlator must still see the decision that
  actually happened at the door.
- `DoorStrike{Address uint8, Output byte}` / `StrikeResolver func(ctx, door) (DoorStrike, error)`
  — replace what used to be `BusActuator`'s bare `Output byte` + `Address func(door) uint8`
  fields. `Address` is the reader's OSDP PD address on the RS-485 segment; `Output` is the relay
  channel on that device — **different numbers**, and conflating them was a real, shipped bug
  (see Notes).
- `BusActuator{Bus, Resolve}` — the shipped `Actuator`: resolves the door's strike via `Resolve`
  (in production, `SQLStore.StrikeFor`, `store_sql.go.md`), then drives a TIMED unlock
  (`Bus.Output`) on that address/channel, so a process death mid-unlock re-locks on the reader's
  own timer rather than leaving the door open until somebody notices.

## Notes

- **App wiring now exists.** `apps/mypintusan/app/app.go.md` + `apps/mypintusan/app/runtime.go.md`
  are the composition root: one `Controller` per configured OSDP bus, rebuilt on every
  reconnect, run against the real `SQLStore`. This app is now bootable and was live-verified
  against `tools/osdp-sim` (see `app/app.go.md`'s Notes). `Controller` itself is still also
  exercised directly by `controller_test.go` (in-memory store) and `store_sql_test.go`'s
  `TestEndToEndThroughSQLite` (real SQLite via `SQLStore`).
- **`BusActuator`'s previous shape shipped a bug, found only by booting the app**: it took
  `Output byte` and an `Address func(door) uint8` directly, and the one caller outside tests
  passed `door.RelayChannel` as the address — the relay channel and the PD address are different
  numbers. A badge decision granted, the audit row said `ok`, and the door never opened, with
  nothing in the test suite catching it because the test rig supplied its own (correct)
  `Address` func. Fixed by replacing both fields with a single `StrikeResolver` — see
  `store_sql.go.md`'s `StrikeFor` for the production resolution logic (entry reader → PD
  address, `Door.RelayChannel` → output).
- `Controller.ContactChanged` is now driven by the reader's own supervised input (OSDP ISTAT).
  The **myiotsan** door-contact binding (`Door.ContactDeviceKey`) remains unwired, so a site whose
  contacts terminate on a relay board rather than on the reader still has no forced/held-open
  detection. Live-benched by `tools/fleetbench/bench_pintusan_alarms.py`, which drives all six
  alarm kinds and the false-positive case (a granted entry must not raise a forced alarm).
- `Snapshot.AntiPassbackViolation` is never computed by anything in this file — anti-passback
  detection (comparing the last in/out passage) does not exist yet; it is a P3 feature.
- `TestRecordPublishesDecisionsButNotDoorStateRows` (`controller_test.go`) pins the `Decisions`
  boundary: a badge grant and an operator unlock are both published, a door-state row
  (forced/held-open, no `RawCredential`) is not, and a `Store.RecordEvent` failure does not
  suppress the publish.

## Unenrolled readers (`handleCard`)

A badge from a reader with no row is logged rather than dropped — it means the bus has a device
on it that nobody enrolled, which is worth someone's attention. It is filed under
`ReasonReaderNotEnrolled` rather than `ReasonReaderOffline`: the reader is answering, it just
delivered a card. See `entities/access_event.go.md`.

## What the first live bench confirmed

`tools/fleetbench/bench_pintusan_door.py` drives this controller over a real OSDP bus (32/32).
The snapshot builder is the half a pure-function test cannot reach, and it holds up: a revoked
credential stops opening the door within a second, lockdown denies above every grant, a duress
PIN grants while raising a critical alarm, and the operator unlock is recorded with the actor.
Note that `Snapshot.AntiPassbackViolation` is still never computed here — anti-passback is a
declared-but-unbuilt P3 feature, correctly hardcoded off at door creation and exposed nowhere,
so `Decide()`'s GATE 9 cannot currently fire.
