# Module: apps/mypintusan/entities/access_event.go

## Purpose

The append-only record of every access decision, modelled on `myidsan`'s existing audit log.
EVERY decision is recorded, including denials and unknown cards — a denied unknown credential
at 03:00 on a perimeter door is the single most valuable row this table will ever hold, and a
system that logs only successes cannot produce it.

## Fields

- `At`/`DoorId`/`ReaderId` — when and where, `At` indexed.
- `CredentialId`/`HolderId` — nullable: an unknown card has neither. `HolderName` is
  DENORMALISED on purpose — an access log must still read correctly years after a holder record
  is deleted or renamed, and "holder 4471" helps nobody in a report.
- `RawCredential` — the value as presented, stored even — especially — when it matches nothing,
  so an operator can enrol it or investigate it. Decoded by `services/wiegand.go.md`'s
  `DecodeCard`, whose `Card.Raw` feeds this field regardless of whether decoding succeeded.
- `Decision` (`granted`/`denied`) and `Reason` — the machine-readable why, enumerated because
  "denied" alone is useless at 3am. The `Reason*` constants are the closed set every one of
  `services/decision.go.md`'s ten gates returns, plus three (`ReasonDoorForced`,
  `ReasonDoorHeldOpen`, `ReasonDoorClosed`) that are not access decisions at all — nobody
  presented a credential — but belong in the same log so "what happened at this door" does not
  require interleaving two tables by timestamp. `Detail` is free text for the operator, never
  parsed.
- `Duress` — access was granted under a duress PIN; the event is stored exactly like a normal
  grant and the alarm is raised out of band (`services.Alarmer`), so nothing about this row
  alone tips off anyone reading it in real time at the reader.
- `Offline` — the decision was served from a cached replica rather than live data.
- `SnapshotRef`/`ContactRef` — correlation hooks: the image `mymatasan` captured at this door at
  this instant, and the myiotsan contact event confirming the door actually opened. The second
  is what separates "we energised the strike" from "somebody went through" — **neither hook is
  populated yet**; there is no snapshot integration and `ContactChanged` is not wired to a real
  contact sensor (see `services/controller.go.md`).

## Notes

- **Not yet persisted.** No repository, no dbsql registration, no migration exists for this
  entity. `services.Store.RecordEvent` is an interface method with only an in-memory test
  implementation; nothing survives a restart.
- Written by `services.Controller.record` on every badge (`services/controller.go.md`), and by
  `emitDoorEvents` for the two door-state-machine alarm kinds above.
