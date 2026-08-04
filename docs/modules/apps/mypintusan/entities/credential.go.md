# Module: apps/mypintusan/entities/credential.go

## Purpose

One physical token belonging to a holder — a card, a PIN, a plate, a face, a mobile credential.
A holder may have several, and any of them opens a door the holder is granted.

## Fields

- `HolderId` — the owning holder, `idx:"holder"`.
- `Kind` — `card`/`pin`/`plate`/`face`/`mobile`. `plate` and `face` are satisfied by
  `mymatasan`'s LPR and face recognition rather than by a reader on the RS-485 bus — the same
  decision path, a different sensor (see `services/decision.go.md`'s `Snapshot`, and
  `MYPINTUSAN_DATA_MODEL.md` §5.2). This is the cheapest large win available to the app: gate by
  plate works the day LPR is wired in, with no new decision logic.
- `Format` — the credential encoding: `wiegand26`/`wiegand34`/`raw-uid`/`desfire-ev2`/`seos`/
  `plate`/`face-vector` (see the `Format*` constants). It is part of the MATCH KEY, not
  decoration — the same number in two encodings is two different credentials. Decoded by
  `services/wiegand.go.md`'s `DecodeCard`.
- `FacilityCode`/`CardNumber` — together identify a Wiegand credential, both indexed
  `idx:"card"`. Matching on `CardNumber` alone is a real-world collision, not a theoretical one:
  card 1234 exists in every facility code ever issued, so a site with two card batches will,
  sooner or later, open a door for the wrong person.
- `PinHash` — a bcrypt hash, `json:"-"`. A PIN is a secret and is stored the way `myidsan`
  stores passwords: never a plaintext or reversible column.
- `DuressPinHash` — when set, grants access AND raises a silent alarm (see
  `services/decision.go.md` gate 6 and `services/controller.go.md`'s `AlarmDuress`).
  Conventionally the normal PIN with the last digit incremented. The coercer must see nothing:
  no different LED, no different buzzer.
- `Status` — `active`/`lost`/`stolen`/`suspended`/`expired`/`revoked`. `lost`/`stolen` are
  distinct from `revoked` on purpose — a card reported stolen and then presented at a door is an
  incident worth surfacing (`entities.ReasonCredentialRevoked` with the status in the detail),
  not merely a denial. `ValidFrom`/`ValidUntil` bound the credential's own validity window,
  independent of the holder's.
- Issuance/revocation audit: `IssuedBy`/`IssuedAt`/`RevokedBy`/`RevokedAt`/`RevokeReason`.

## Notes

- **Not yet persisted.** No repository, no dbsql registration, no migration exists for this
  entity; it is exercised only by in-memory test fakes.
- A profile emitting `raw-uid` only is a security note, not just a format note: UID-only
  credentials clone with a cheap phone app, which is why the hardware plan
  (`docs/MYPINTUSAN_HARDWARE_PLAN.md`) caps such a reader at `interior` doors regardless of how
  well the reader itself is verified.
- `services.Store.CredentialByCard` is the lookup keyed on `(Format, FacilityCode, CardNumber)`
  (`services/controller.go.md`); no such lookup by `plate`/`face` credential kinds exists yet
  — those sensors are not wired in.
