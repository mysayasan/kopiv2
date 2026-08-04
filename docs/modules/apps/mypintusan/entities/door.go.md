# Module: apps/mypintusan/entities/door.go

## Purpose

One controlled opening: the placement, the lock hardware, the offline and Secure Channel
policy, and the myiotsan bindings a door needs to actuate and to know its own position.

## Fields

- Identity/placement: `Id`, `Name`, `SiteId`/`BuildingId`/`FloorId` — reuses `myseliasan`'s
  site/building/floor tree rather than inventing a second geography, so a door belongs on a
  floor plan next to the cameras already watching it.
- `Class` — `interior`/`perimeter`/`critical`. Drives the offline policy (see
  `DefaultOfflineTTLSeconds` below and `MYPINTUSAN_DATA_MODEL.md` §2), the reader trust matrix,
  and whether Secure Channel is required.
- `LockKind` — `fail-secure`/`fail-safe`. Modelled rather than merely documented because it
  changes both the wiring and the alarm semantics: a fail-safe maglock has no mechanical egress,
  so every safety device must be able to cut its power without software (`MYPINTUSAN_HARDWARE_
  PLAN.md` §6). Egress code (UBBL/BOMBA locally, NFPA 101/IBC elsewhere) is deliberately not
  encoded by this app.
- `UnlockSeconds`/`ExtendedUnlockSeconds`/`HeldOpenSeconds` — strike time, the accessibility
  extension's longer strike time, and the door-held-open alarm threshold. Resolved by
  `StrikeSeconds`/`DefaultOfflineTTLSeconds` below and consumed by `services/doorstate.go.md`'s
  `DoorMachine`.
- `ReaderInId`/`ReaderOutId` — entry- and exit-side reader. `ReaderOutId` is `0` for a REX-only
  door; a populated exit reader is required for hard anti-passback.
- `RelayDeviceKey`/`RelayChannel`/`ContactDeviceKey` — the myiotsan bindings: the relay channel
  that throws the strike, and the contact that reports real door position. Actuation is meant
  to go through myiotsan's guarded `CommandService.Issue` chokepoint, never a direct relay call
  — **this path is not implemented**; see `services/controller.go.md`'s `BusActuator`, which
  drives the reader's own output instead, and `ContactChanged`, which nothing currently calls.
- `RequireSecureChannel` — defaults on for perimeter/critical. No cleartext fallback: if the
  session fails to establish or drops mid-session, the reader is taken out of service and
  alarmed (`services/decision.go.md` gate 2).
- `OfflinePolicy` (`cached`/`deny`) and `OfflineTTLSeconds` (class-default override) — there is
  deliberately no allow-all; see `DefaultOfflineTTLSeconds`.
- `AntiPassback` — `off`/`soft`/`hard`. Soft (log the violation, grant anyway) is the default;
  hard is reserved for `critical` doors with a reliable exit reader. **Nothing computes the
  violation input yet** — see `services/decision.go.md`'s `Snapshot.AntiPassbackViolation`.
- `Enabled` and audit fields.

## Methods

- `DefaultOfflineTTLSeconds()` — the cache lifetime for this door's class when
  `OfflineTTLSeconds` does not override it: `critical` 8h, `perimeter` 24h, `interior` 72h. Past
  the TTL the door denies (`services/decision.go.md` gate 10) — the cache keeps a network blip
  from stranding staff outside a building, but "works offline" must never mean "stops checking".
- `StrikeSeconds(extended bool)` — the unlock duration for a holder, honouring
  `ExtendedUnlockSeconds` when `extended` is set and the holder qualifies
  (`entities.Holder.ExtendedUnlock`); falls back to `UnlockSeconds`, then a hardcoded `5`.

## Notes

- **Not yet persisted.** No repository, no dbsql registration, no migration exists for this
  entity; it is exercised only by in-memory test fakes.
- `Class`/`LockKind`/`OfflinePolicy`/`AntiPassback` constants are enumerated (`Class*`,
  `Lock*`, `Offline*`, `APB*`) as a closed set, matching the reason-code convention in
  `entities/access_event.go.md`.
