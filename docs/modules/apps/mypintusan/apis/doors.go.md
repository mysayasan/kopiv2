# Module: apps/mypintusan/apis/doors.go

## Purpose

The estate-and-actuation surface: `GET`/`POST /doors`, `GET /doors/{id}`, `GET /readers`, plus
the two powers a receptionist and a security desk actually need — `POST /doors/{id}/unlock` and
`GET`/`POST /lockdown`. Every route here is authorized by the shared accessrbac matrix declared
in `services.Policy()` (`services/rbac.go.md`), which is deny-by-default.

Also the file that opens `package apis`'s doc comment: the API surface is deliberately small at
this stage — doors and their live state, people and their badges, the access log, and lockdown —
and everything routes through the controller's audited chokepoint rather than driving hardware
directly from an HTTP handler.

## Key Type: Unlocker

```go
type Unlocker interface {
    Unlock(ctx context.Context, door entities.Door, actor int64, actorName string) error
    SetLockdown(ctx context.Context, on bool)
    Lockdown() bool
}
```

The runtime seam this file depends on, satisfied by `app.runtime` (`app/runtime.go.md`). It is
an interface specifically so the HTTP layer cannot reach an `osdp.Bus` or a
`services.Controller` directly — every unlock has to pass through the controller's audited
`OperatorUnlock`/`Actuator.Unlock` chokepoint, the same one a badge uses.

## Responsibilities

- `NewDoorApi(router, store, rt, db)` — registers `/doors` and `/readers` on the given
  (protected) subrouter. `/doors/{id}/unlock` is its **own** matrix entry, separate from
  `/doors` itself, because seeing a door and opening it are different powers held by different
  people (`services/rbac.go.md`).
- `list` / `get` — door and reader listing/lookup; `get` goes through `store.Door` (the
  `SQLStore` method, `services/store_sql.go.md`) rather than the raw repo, so a missing door
  reads as a clean 404 rather than the `GetById` not-found trap documented there.
- `create` (`POST /doors`) — creates a door **and its entry reader in one call**. A door with no
  reader is inert (nothing can badge at it) and a reader with no door drives nothing, so the two
  are never created separately: that would invite a half-configured state that looks fine in two
  list screens and does nothing at the wall — exactly the confusion a non-technical installer
  must be spared. Admin-only (`user.IsAdmin`), on top of the matrix, for the same reason
  `lockdown.set` is: a wrong hardware binding here does not produce a bad reading, it produces a
  door that opens for the wrong person or an alarm that never comes. Validates the name, the bus
  port, and the OSDP address range (0–127); refuses a reader address already claimed on that
  cable via `store.ReaderByBus` (readers ship at address 0, so a second reader left at the
  factory default is the out-of-box collision this catches). Creates the `Door` row first, then
  the `Reader` row pointed at it; if the reader insert fails the door is rolled back
  (`DeleteById`) rather than left as a door nobody can ever open. On success, updates the door's
  `ReaderInId` to the new reader — this is what `StrikeFor` (`services/store_sql.go.md`) resolves
  to find the PD address, so a door left with `ReaderInId == 0` would grant a badge and then fail
  to open. This is the first-door step in the SPA's first-run wizard
  (`views/react-webpack/src/views/Wizard.js`).
  `RequireSecureChannel` on the request struct is a **`*bool`**, not a `bool`: a nil pointer
  (the field omitted) resolves through `entities.SecureChannelDefault(body.Class)` — on for
  `perimeter`/`critical`, off otherwise — while an explicit `false` is honoured as the caller's
  own escape hatch. Before this the field was passed straight through like a plain default-`false`
  bool, so a `critical` or `perimeter` door created without mentioning Secure Channel silently got
  none; measured live, a card on a plaintext reader opened a `critical` door. It matters more than
  the neighbouring defaults (`UnlockSeconds`, `HeldOpenSeconds`, `OfflinePolicy`, all defaulted
  already) because there is no `PUT /api/doors` — a door created with the wrong policy keeps it
  for good.
- `unlock` — reads the acting `LocalUser` from context (never from the request body — an
  attacker-supplied actor name in the audit log next to a door opening would be worse than no
  name at all, it would be a forged record), resolves the door, then calls `rt.Unlock`. A
  refusal is already recorded in the access log by the time the handler returns (the controller
  records either outcome), so a denied remote unlock is exactly as auditable as a granted one.
- `NewLockdownApi(router, rt)` — registers `GET`/`POST /lockdown`.
- `state` — reports the current site-wide seal via `rt.Lockdown()`.
- `set` — admin-only **on top of** the shared matrix entry (`services/rbac.go.md` already denies
  it to viewer/operator; this is a second, explicit `user.IsAdmin` check in the handler).
  Lockdown is the one control that stops a building working: it cannot trap anyone, because
  egress is hardware and no software here can override it, but an operator who triggers it
  during a fire drill has still turned a nuisance into an incident.

## Notes

- `doorApi.store` is the concrete `*services.SQLStore`, not the `services.Store` interface —
  this API layer is wired directly to the production store, unlike `Controller`, which is
  written against the interface so it can run over either the SQL store or a test fake.
- Live-verified alongside the rest of the API surface: `GET /doors`, `GET /doors/{id}`,
  `GET /readers`, `POST /doors/{id}/unlock` (both while live and refused during lockdown),
  `GET`/`POST /lockdown` all exercised against a booted app with `tools/osdp-sim`.
- `POST /doors`'s Secure Channel default (above) is live-benched by
  `tools/fleetbench/bench_pintusan_securechannel.py`, which provisions a `critical`/`perimeter`
  door mentioning nothing about Secure Channel and asserts on the STORED door, not just the
  response the handler sent back.
- `POST /doors` was exercised the same way, driven through the wizard's door step
  (`views/react-webpack/src/views/Wizard.js`) rather than by a direct API call. That path is what
  first exposed the shared SPA's request-double-encoding bug (`lib/api.js` — every write in the
  SPA failed to unmarshal until fixed), since it was the first write the wizard performs.
