# Module: apps/myseliasan/services/settings.go

## Purpose

Implements `ISettingsService`, an in-app, superadmin-gated editor over a SAFE SUBSET of
`myseliasan`'s `config.json` — the first in-app settings surface this app has had (mymatasan
already has one; myseliasan previously required a manual file edit + restart for everything).
The scope deliberately excludes the blocks that would take the app offline if mis-set (`db`,
`server`, `bootstrap`): those stay file-only.

## Persistence model

Decided against a pure DB store because myseliasan's editable config is infra-level, read once
by the shared apphost at boot (TLS, CSP, rate limit, cache, logging, telemetry, SSO, pairing,
localAuth) — there is no seam for a DB value to override them at startup:

- The authoritative CURRENT value lives in `config.json`, which the host re-reads on restart;
  `deps.Config` mirrors it in memory. `Get`/`GetAll` read from the live in-memory config.
- `Save` validates, updates the in-memory config so the UI reflects the pending value
  immediately, writes the changed leaves back into `config.json` (`settings_materialize.go`),
  and reports `needsRestart: true` — the change only takes effect once the process relaunches
  (`apis/system.go`'s `POST /api/system/restart`).
- A one-time DB snapshot of the original config (`ControlSetting` row keyed
  `settings.defaults`, captured once on first run via `ensureDefaults` and never overwritten)
  backs `Reset`, so "restore defaults" still works after `config.json` has been hand-edited or
  overwritten since.

Secrets (`localAuth.password`, `sso.clientSecret`, `jwt.secret`, `cache.redis.password`) are
never returned by `Get`/`GetAll`: the leaf is blanked and a sibling `"<field>Set"` boolean is
added so the UI can show whether a value exists without exposing it. `Save` treats a blank (or
omitted) incoming secret as "keep the current value" rather than clearing it.

## Sections

`sectionOrder` (also the display/tab order): `localAuth`, `sso`, `pairing`, `security`,
`storage`, `logging`. `read(section)` builds each section's root-relative nested map straight
from `*config.AppConfigModel` — see the switch in `settings.go` for the exact field list per
section (mirrors `infra/config`'s `LocalAuth`/`SSO`/`Pairing`/`Jwt`/`Tls`/`SecurityHeaders`/
`RateLimit`/`FileStorage`/`Cache`/`Logging`/`ApiLog`/`Telemetry` models).

## ISettingsService

- `Sections() []string` — the editable section ids in display order.
- `Get(section) (map[string]any, error)` — one section's current values, secrets masked.
- `GetAll() map[string]any` — every section keyed by id, secrets masked.
- `Save(ctx, section, body json.RawMessage) (SaveResult, error)` — projects the request body
  onto the section's known shape (`projectOntoShape`, dropping the UI's `"<field>Set"` helpers
  and any stray field so `materializeConfig` can never write an unrecognized key), restores a
  blank secret to its current value, then delegates to `commit`.
- `Reset(ctx, section) (SaveResult, error)` — loads the first-run snapshot, projects it onto
  the section's *current* shape (so a snapshot from an older schema still resets cleanly), then
  delegates to `commit`.
- `TestCache(ctx, body json.RawMessage) error` — pings Redis with the settings in `body`
  (`address`, `password`, `db`, `useTls`, `connectTimeoutMs`, `operationTimeoutMs`), so an operator
  can verify connectivity **before** saving. A blank `address` or `password` falls back to the
  currently configured value (`s.cfg.Cache.Redis.*`), so an existing config can be tested as-is, or
  a new one in progress can be tested without persisting it first. Builds a throwaway
  `cache.NewRedisStore` (`infra/cache`), calls `Ping` under a timeout (`connectTimeoutMs +
  operationTimeoutMs + 1s`, each individually defaulting to 2s when zero), and closes the store
  regardless of outcome — it never touches `s.cfg` or `config.json`, so a test can never leave a
  half-applied side effect even on success.

`commit(ctx, section, data)` — the single write path both `Save` and `Reset` funnel through:
`validateSection` (`settings_apply.go`) → `applyToConfig` (updates `*config.AppConfigModel` in
place) → `materializeConfig` (`settings_materialize.go`, writes the changed leaves into
`config.json`). Always returns `SaveResult{NeedsRestart: true}` on success, since every
editable block is read by the host only at boot.

## Constructor

`NewSettingsService(cfg, cfgPath, db, cipher, logf)` — `cfg` is the live
`*config.AppConfigModel`, `cfgPath` the on-disk `config.json` path (both from
`apphost.Dependencies`), `db` builds the `ControlSetting` generic repo, `cipher` is the fleet
`*atrest.Cipher` (may be nil — encryption at rest off) used to encrypt the first-run defaults
snapshot via the shared `encodeSecret`/`decodeSecret` helpers (`secret_store.go.md`). Calls
`ensureDefaults` once at construction; a capture failure only disables `Reset` (logged via
`logf`, never fails boot). Wired in `app.go`'s `RegisterAppRoutes`.

## Notes

- `ensureDefaults` never overwrites an existing `settings.defaults` row, so a restart (which
  reloads a possibly-already-edited config) can't clobber the original snapshot.
- The nested-map helpers (`projectOntoShape`, `maskCopy`, `flattenPatches`, `leafAny`/
  `leafString`/`setLeaf`) are the data-driven plumbing shared by `Get`/`Save`/`Reset`; they
  operate on the same root-relative shape `read()` returns, so a section's field list only
  needs to be declared once.
- `sectionSecrets` is the single place that lists which dotted leaf per section is a secret —
  both masking (`Get`/`GetAll`) and keep-if-blank (`Save`) are driven off it.
- See `services/settings_apply.go.md` for validation/typed-apply and
  `services/settings_materialize.go.md` for the config.json write-back; `apis/settings.go.md`
  for the HTTP surface and superadmin gate; `apis/system.go.md` for the restart endpoint that
  applies a pending change; `services/filesystem_browse.go.md` for the server-side path picker
  behind the storage/logging/TLS/SSO path fields.
- `TestCache` deliberately bypasses `commit`/`validateSection`/`applyToConfig`/
  `materializeConfig` entirely — it never writes `s.cfg` or `config.json`, so it carries none of
  the "every save needs a restart" behavior the rest of this file has; it is a pure connectivity
  check.
