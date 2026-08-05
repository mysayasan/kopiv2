# Module: apps/mypintusan/config/config.go

## Purpose

Decodes mypintusan's own slice of `config.json` — the site's access-control behaviour and its
RS-485 bus/reader inventory — kept out of the shared `AppConfigModel` every other app would
otherwise carry, via the same phase-C `apphost.AppConfigDecoder` seam myiotsan's
`apps/myiotsan/config` uses. The `access`/`buses` blocks stay top-level in `config.json`, not
nested under an `"app"` key.

## Key Type: Config

```go
type Config struct {
    Access AccessConfig `json:"access"`
    Buses  []BusConfig  `json:"buses"`
}
```

## Key Type: AccessConfig

- `Timezone` — the site's IANA zone name (`"Asia/Kuala_Lumpur"`). **Configured, not taken from
  the host clock**: schedules and the holiday calendar are local concepts (a 22:00–06:00 night
  shift and a public holiday are both wrong evaluated in UTC), and a controller in a rack
  somewhere may well be set to it.
- `TickSeconds` (default 1) — drives the door state machines' relock/held-open timers. Bounds
  how *late* an alarm can be, not whether it fires — coarsening it to save wakeups directly
  delays every door alarm on site.
- `PINWindowSeconds` (default 15) — how long a keypad entry stays available to be paired with a
  card. Short on purpose: a PIN left buffered would be picked up by whoever badges next.
- `Offline` — marks this controller as running from a cached replica rather than the live
  database (`docs/MYPINTUSAN_DATA_MODEL.md` §2). Not yet exercised by a running deployment —
  the offline-cache `Store` implementation described there does not exist; only `SQLStore` does.

## Key Type: BusConfig / ReaderConfig

- `BusConfig.Port` — `"COM3"`, `"/dev/ttyUSB0"`, or `"tcp://host:port"` against
  `tools/osdp-sim`. Only the `tcp://` form actually dials today — see
  `apps/mypintusan/app/runtime.go.md`'s `dialBus`; serial is build-order step 5 and not wired.
- `BusConfig.SlotMillis` (default 50) / `ReplyTimeoutMillis` (default 200) — passed straight
  into `osdp.Options`; per-PD poll cadence is `SlotMillis` × reader count
  (`MYPINTUSAN_OSDP_PLAN.md` §6.2's open budget question).
- `ReaderConfig.SCBK` — the 16-byte Secure Channel base key as hex. Empty runs the reader in the
  clear.
- `ReaderConfig.RequireSecureChannel` — takes the reader **out of service** rather than falling
  back to cleartext if a session cannot be established or drops. Expressed here (bus config)
  because the Secure Channel session lives on the bus; `Door.RequireSecureChannel` is the
  separate column the decision path itself enforces.

## Responsibilities

- `Load(raw)` — a nil/empty payload yields a usable config with **no buses**, which is exactly
  what a fresh install has before anyone has wired a reader; fills every default above.
- `Location()` — resolves `Access.Timezone` via `time.LoadLocation`. **A bad zone name is an
  error, not a silent fall-back to UTC** — falling back would shift every schedule on the site by
  the offset and deny people at the edges of their shift for reasons nobody could reproduce, far
  worse than refusing to start. `apps/mypintusan/app/app.go.md`'s `RegisterAppRoutes` calls this
  first and aborts boot on error. `""`/`"Local"` resolves to `time.Local`.
- `Tick()` / `PINWindow()` — convenience `time.Duration` conversions fed into
  `services.ControllerConfig`.

## Notes

- **Now a first-boot seed only, not the runtime source of truth.** `apps/mypintusan/app/app.go.md`
  (`DecodeAppConfig`/`appConfig`) still decodes this file, but `RegisterAppRoutes` immediately
  converts it (`settingsFromConfig`) into `services.AccessSettings` and hands it to
  `services.NewAccessSettingsService`, which writes it into the `runtime_setting` table on first
  boot only — every later boot reads the database row and this file is never consulted again. It
  is also that service's `Reset()` target, so `settingsFromConfig`'s output stays reachable as the
  recovery value. `apps/mypintusan/app/runtime.go.md` no longer reads `*Config`/`BusConfig`/
  `ReaderConfig` at all — it takes `services.AccessSettings`/`BusSettings`/`ReaderSettings`
  (`services/runtime_settings.go.md`) instead, which mirror this file's shape field-for-field but
  live in the database. `Location`/`Tick`/`PINWindow` on this type are consequently only exercised
  during that one first-boot conversion; the equivalent methods an operator's edits actually go
  through are `AccessSettings.Location()` and the inlined `time.Duration(...)` conversions in
  `app/runtime.go.md`.
