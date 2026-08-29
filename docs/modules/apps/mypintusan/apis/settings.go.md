# Module: apps/mypintusan/apis/settings.go

## Purpose

The HTTP surface over `services.IAccessSettingsService` (`services/runtime_settings.go.md`) — the
screen that makes this app configurable by a facilities manager instead of by somebody editing
`config.json` over SSH. `config.json` only ever seeds the first boot; from then on these three
routes are the only way anything about the controller's runtime behaviour changes. Backed by the
SPA's Settings page (`views/react-webpack/src/views/Settings.js`).

## Responsibilities

- `NewSettingsApi(router, settings)` — mounts `GET`/`PUT /settings/access` and
  `POST /settings/access/reset` on the given (protected) subrouter.
- `get` — returns the live settings. Site keys (`ReaderSettings.SCBK`) are redacted by the entity's
  own `MarshalJSON` (`services/runtime_settings.go.md`), so this handler cannot leak one by
  forgetting to.
- `save` (`PUT`) — admin-only (`user.IsAdmin`), on top of whatever the matrix says: these values
  decide which readers are polled, which are encrypted, and how a door behaves — not operator
  settings, since a wrong entry here does not produce a bad reading, it produces a door that opens
  for the wrong person or an alarm that never comes. A validation failure is returned as a 400 with
  the plain-language reason (e.g. "two readers at address 1"), because the person reading it is the
  one who has to go change a DIP switch. A save that turns `offline` on or off now reaches the
  RUNNING controllers immediately (`services/runtime_settings.go.md`'s `IAccessSettingsService.
  OnChange` → `app/runtime.go.md`'s `runtime.ApplySettings`) — before this, the setting persisted
  and read back correctly while every door carried on deciding under the old value until the
  process restarted. Every other field on this page (buses, readers, tick, PIN window, timezone)
  still needs a restart.
- `reset` (`POST`) — admin-only, same reasoning as `save`. Restores the `config.json` seed; the
  recovery path for an edit that stopped the controller working, so a mistyped timezone does not
  need a site visit with database access.

## Notes

- Registered in `app/app.go.md`'s `RegisterAppRoutes`, built from the same `settings` service the
  runtime itself reads at boot (`app/app.go.md`'s `settingsFromConfig`/`NewAccessSettingsService`).
- Unlike myidsan's `apis/settings.go` (a multi-section config editor with an audit trail and a
  cache-connectivity test), this is a single section with no audit log yet — the settings surface
  here is new and deliberately small: timezone/tick/PIN-window/offline plus the bus/reader/SCBK
  inventory, nothing else.

## The administrative trail

A save writes `settings.change` and a reset writes `settings.reset` to the append-only trail
(`apis/audit.go.md`) — and both **name what actually moved**.

The handler reads the live values *before* the write and diffs them (`describeSettingsChange`).
"Settings changed" is the least useful entry an audit log can hold, and the request body cannot
answer it either: the screen posts the whole object on every save, so every field looks submitted
whether or not it changed. The entry says `timezone Asia/Kuala_Lumpur -> UTC` and nothing else.

Only the values that change how the controller **decides** are named — the site timezone (every
schedule and holiday is evaluated in it), the offline flag, the timer cadences, the shape of the bus
— plus, individually, a reader's Secure Channel requirement and a rekey, because those are the two
edits on this screen that weaken or strengthen the wire itself.

Readers are keyed by **bus port and PD address**, never by position: a reader removed from the
middle of a list would otherwise report every reader after it as changed.

**A rekey is recorded as a fact and never as a value.** The trail is readable by every administrator
and exported to CSV; a site base key in it is a key handed out, and anyone holding it can decrypt the
bus and impersonate a reader — the exact attack Secure Channel exists to stop. The live bench asserts
this over *every* row in the table, not only the ones it wrote.

A reset diffs the same way, because the entries describing the edits it undid are still in the trail
and nothing else would say they stopped being true.
