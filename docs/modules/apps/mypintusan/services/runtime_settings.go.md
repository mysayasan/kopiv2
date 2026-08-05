# Module: apps/mypintusan/services/runtime_settings.go

## Purpose

`AccessSettings` is everything about how this controller behaves that an operator may change,
persisted in the **shared** `runtime_setting` table (`sharedentities.RuntimeSetting`) under the
single key `"access"`. This is a direct port of mymatasan's `services/runtime_settings.go`
pattern, at the user's explicit direction: `config.json` only ever **seeds** the first boot — from
then on the database row wins and the file is never read again. A door system is bought by a
facilities manager, not an engineer; nobody is going to SSH in and hand-edit an RS-485 bus list or
paste a 32-character AES key into a JSON file, so once the app has booted once every one of these
values has to be reachable from a screen (`apis/settings.go.md`, the SPA's Settings page).

## Key Type: AccessSettings

```go
type AccessSettings struct {
    Timezone         string
    TickSeconds      int
    PINWindowSeconds int
    Offline          bool
    Buses            []BusSettings
}
type BusSettings struct {
    Port               string
    SlotMillis         int
    ReplyTimeoutMillis int
    Readers            []ReaderSettings
}
type ReaderSettings struct {
    Address              int
    SCBK                 string // redacted on the way OUT, see MarshalJSON below
    RequireSecureChannel bool
    Label                string
}
```

The same shape `config/config.go.md`'s `Config`/`AccessConfig`/`BusConfig`/`ReaderConfig` already
described — `app/app.go.md`'s `settingsFromConfig` converts one into the other for the first-boot
seed — but this is now the type the running app actually reads: `app/runtime.go.md`'s `runtime.cfg`
is an `AccessSettings`, not a `*pintuconfig.Config`, and `superviseBus`/`runBus` take a
`BusSettings`, not a `pintuconfig.BusConfig`.

## Responsibilities

- `ReaderSettings.MarshalJSON` — redacts `SCBK` on every JSON encode, replacing it with
  `HasSCBK`/`UsingDefaultKey` booleans. Redaction lives on the **type**, not in each handler,
  deliberately: a future endpoint that returns settings (an export, a diagnostic dump, a support
  bundle) gets the redaction for free instead of leaking the key because somebody forgot.
  `UsingDefaultKey` compares against the published OSDP install-mode key (`defaultSCBKHex`), so
  the UI can nag a reader that is technically "encrypted" but not actually secure.
- `NewAccessSettingsService(repo, defaults)` — `IAccessSettingsService`'s only implementation.
  `defaults` is the config.json-derived seed (`app/app.go.md`'s `settingsFromConfig`).
- `Get(ctx)` — reads the `"access"` row via `GetByUnique(ctx, "", "key", accessSettingsKey)` and
  seeds it on first call if absent. Deliberately **not** a `GetById`/unfiltered lookup: this file's
  own comment calls out the `GetByUnique`-against-a-real-`ukey` requirement by name, referencing
  the suite-wide bug where a key group matching no declared `ukey` falls through to an unfiltered
  select and returns the first row in the table.
- `Save(ctx, in, actor)` — validates then persists. Carries forward any `SCBK` the caller omitted
  from the currently-stored settings **before** validating, in that order deliberately: the API
  redacts `SCBK` on read, so a UI that round-trips settings without editing a key sends it back
  empty; validating first would refuse the save ("requires an encrypted session but has no
  security key") for edits that touch nothing about that reader, and worse — skipping the
  carry-forward entirely would let a no-op Save silently **wipe every key and drop every door to
  cleartext**.
- `Reset(ctx, actor)` — rewrites the config.json-derived seed. The recovery path for a settings
  edit that stops the controller booting (a bad timezone, an invalid bus list) without needing
  direct database access.
- `write` — the shared persist path `seed`/`Save`/`Reset` all funnel through; marshals via
  `rawSettings` (the **un-redacted** wire form, so persisting through the entity's own
  `MarshalJSON` — which is what the API responses use — never destroys a stored key), then
  updates the existing row or creates it.
- `normalizeAccessSettings` — fills defaults (timezone `"Local"`, tick 1s, PIN window 15s, bus
  slot/reply-timeout defaults) without ever rejecting; a value read back from an older database row
  is repaired, not fatal. Validation is a separate pass (`validateAccessSettings`), run only on a
  `Save`.
- `validateAccessSettings` — refuses a timezone `time.LoadLocation` cannot parse, a tick interval
  over 60s ("every door alarm would be that late"), a duplicate bus port, a duplicate reader
  address on one bus (readers ship at address 0 — the single most likely onboarding mistake), a
  malformed (non-32-hex-char) `SCBK`, and `RequireSecureChannel` set with no key.
- `AccessSettings.Location()` — resolves `Timezone` the same way `config.Config.Location()` used
  to; `""`/`"Local"` → `time.Local`, anything else through `time.LoadLocation`. `app/app.go.md`'s
  `RegisterAppRoutes` calls this (via the settings service, not the config) and refuses to start on
  error, for the same reason `config/config.go.md`'s original `Location()` did: a silent fall back
  to UTC would shift every schedule on the site.

## Notes

- Copied from `apps/mymatasan/services/runtime_settings.go` "exactly, at the user's explicit
  direction" (this file's own header comment) — the seed/DB-owns split, the redact-on-`MarshalJSON`
  pattern, and the reset-restores-seed recovery path are all the same shape mymatasan already
  shipped for its own runtime settings.
- `config/config.go.md`'s `Config`/`AccessConfig`/`BusConfig`/`ReaderConfig` are now a **seed-only**
  type: nothing in the running app reads them after `app/app.go.md`'s `settingsFromConfig` converts
  them into an `AccessSettings` at first boot. See that file's Notes for what changed.
- Covered by `services/runtime_settings_test.go`.
