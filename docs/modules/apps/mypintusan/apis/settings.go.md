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
  one who has to go change a DIP switch.
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
