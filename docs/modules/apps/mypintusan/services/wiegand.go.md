# Module: apps/mypintusan/services/wiegand.go

## Purpose

Wiegand decoding: turning the bits an OSDP reader hands over into a facility code and a card
number. Deliberately small and boring but written out explicitly, because this is the single
easiest place in the app to produce a plausible WRONG answer — a misread card number does not
error, it silently identifies a different person, or nobody, and the symptom is "the system
sometimes doesn't recognise my card".

## Responsibilities

- `Card` — a decoded presentation: `Format`, `FacilityCode`, `Number`, `Raw` (the presented
  value in a stable, loggable hex form, retained even when decoding fails — an unrecognised card
  at a perimeter door at 03:00 is worth investigating and the raw bits are the only evidence),
  `BitCount`.
- `Card.Key()` returns `(Format, FacilityCode, Number)`, the match key `services.Store.
  CredentialByCard` looks up by. Never the number alone — card 1234 exists in every facility
  code ever issued.
- `DecodeCard(bits, data)` — dispatches on `bits`: `26` and `34` go to the explicit-layout
  decoders below; anything else is passed through as `entities.FormatRawUID` rather than
  rejected, so a DESFire/Seos reader emitting its own lengths stays usable. (UID-only credentials
  are cloneable, which is why the hardware plan caps such a reader at `interior` doors — the cap
  is the mitigation, not a refusal to read.) Returns an error if `bits` claims more than `data`
  can carry.
- `decodeWiegand26` — the classic H10301 layout (leading even parity, 8-bit facility code,
  16-bit card number, trailing odd parity). `decodeWiegand34` — the common H10302/H10304-style
  split (16/16 with parity bits); explicitly noted as not a single agreed standard — some
  vendors use all 32 payload bits as one number with no facility code, and a site whose cards
  disagree needs a per-profile format rather than a change here.
- Parity is CHECKED, not assumed, in both decoders (`evenParity`/`oddParity`/`bit`): a bit error
  from a long or badly terminated run is otherwise indistinguishable from a different card,
  which is to say it opens the door for the wrong person.
- `EncodeWiegand26(facility, number)` — builds a 26-bit payload with correct parity. Exists for
  the simulator and for tests: presenting a SPECIFIC card end-to-end without owning a card
  printer is what makes the decision path testable at all.

## Notes

- Pure functions, no I/O, no dependency on `services.Store` or the OSDP bus.
- Called from `services.Controller.handleCard` (`services/controller.go.md`) on every
  `osdp.EventCard`; a decode failure there is treated as `ReasonUnknownCredential`, never a
  guess — a card whose parity failed may be one bit away from a valid credential belonging to
  somebody else.
- Covered by `wiegand_test.go`.
