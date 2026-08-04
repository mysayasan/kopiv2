# Module: apps/mypintusan/entities/reader.go

## Purpose

Two related types: `Reader`, one physical reader INSTANCE on an OSDP bus, and `ReaderProfile`,
the model template it is provisioned from — the same instance/template split `myiotsan` uses
for `IotDevice`/`DeviceProfile`, with one addition `ReaderProfile` needs and `DeviceProfile`
does not: a trust level, because a wrong reader binding does not produce a bad reading, it
produces a door that opens for the wrong person.

## Fields — `Reader`

- `ProfileId`/`DoorId` — the profile this instance was provisioned from and the door it serves,
  both indexed. `Direction` (`in`/`out`) — an `out` reader is what makes hard anti-passback
  possible at all.
- `BusPort` — the OSDP bus this PD sits on (`"COM3"`, `"/dev/ttyUSB0"`, or `"tcp://host:port"`
  for the simulator). OSDP and Modbus RTU cannot share one RS-485 pair — two polled masters
  transmitting into each other produce intermittent CRC failures indistinguishable from a
  cabling fault, so a door needs its own adapter.
- `OsdpAddress` — 0-126, unique per bus. Readers ship at `0`, so address assignment is a real
  onboarding step, not an afterthought.
- `ScbkState` — `default`/`rekeyed`/`failed`. A reader still on the well-known default base key
  (SCBK-D) is not secure whatever else it claims — that key is printed in every vendor's manual
  — and is capped at `interior` doors until a `KEYSET` lands (see
  `docs/MYPINTUSAN_OSDP_PLAN.md` §2.3).
- `LastSeenAt`, `TamperState` (`ok`/`tamper`/`offline`), `Enabled`, audit fields.

## Fields — `ReaderProfile`

- Identity: `Slug` (`ukey`), `Name`, `Vendor`, `Model`, `Description`.
- `Transport` — selects the driver: `"osdp"` at launch, later `"wiegand"`/`"zkteco"`/
  `"hikvision-isapi"`.
- `OsdpBaud`/`OsdpDefaultAddress` — a provisioning hint, not the live address; a multi-drop bus
  cannot have two PDs at `0`.
- `SupportsSecureChannel` — a capability CLAIM by the profile. Whether Secure Channel is
  REQUIRED is a per-door policy (`entities.Door.RequireSecureChannel`), never a property of the
  reader. `ShipsWithDefaultSCBK` records the out-of-box key state the profile expects.
- `HasKeypad`/`HasBiometric`/`HasTamper`/`HasLedControl`/`HasBuzzerControl` — capability flags.
  A reader that silently ignores an LED/buzzer command it does not support is worse than one
  that declares it cannot: the operator sees no red flash on deny and concludes the system is
  broken.
- `CardFormats` — a JSON array of the encodings this reader emits, most-preferred first.
- `Verification` — `unverified`/`community`/`verified`/`reference` (`Verify*` constants,
  ordered). Does the real work in the trust matrix and is explicitly NOT the same axis as
  `Builtin` — a builtin profile may still be `unverified` if it was seeded from a datasheet
  rather than benched. `VerifiedFirmware`/`VerifiedAt`/`SourceUrl` record what was actually
  benched; a verification with no firmware string is worthless because vendors change reader
  behaviour in point releases.
- `Builtin` — shipped-in-the-binary, cannot be deleted, so a site cannot break its own
  onboarding by tidying up. Audit fields.

## Notes

- **Not yet persisted, and not yet consumed at runtime.** No repository, no dbsql registration,
  no seeded builtin catalog (`myiotsan`'s `ProfileService.EnsureBuiltins` has no `mypintusan`
  counterpart yet), and nothing in `services/decision.go.md` or `services/controller.go.md`
  currently reads `ReaderProfile.Verification` to enforce the trust matrix — the entity exists,
  the enforcement described in `docs/MYPINTUSAN_HARDWARE_PLAN.md` does not.
- `Reader` is looked up by `services.Store.ReaderByBus(busPort, address)` in
  `services/controller.go.md`'s badge path.
