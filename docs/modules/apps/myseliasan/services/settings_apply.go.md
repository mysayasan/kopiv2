# Module: apps/myseliasan/services/settings_apply.go

## Purpose

The typed half of the settings editor (`settings.go`): validates a section's merged values and
writes them onto the strongly-typed `*config.AppConfigModel`. Values arrive as either native Go
types (from `settings.go`'s `read()`) or JSON-decoded types (`float64` for numbers), so every
getter coerces both.

## `validateSection(section string, data map[string]any) error`

Rejects clearly unsafe values before anything is written or persisted. Deliberately lenient
about values an operator legitimately might want (e.g. disabling a block), strict only where a
bad value would break boot or security:

- `localAuth` — a username is required when local login is enabled.
- `sso` — session/policy-cache TTLs cannot be negative.
- `pairing` — `mtlsPort`/`controlPort`/`mediaPort` must each be `0` (unset) or in `1..65535`;
  a non-empty multicast address must be `host:port`.
- `agent` — `llm.mode` must be `off`/`external`/`sidecar`; `external` mode additionally requires
  a non-empty `llm.endpoint` starting `http://`/`https://`. `digest.localHour` must be `0..23`;
  `digest.windowHours` `0..168`; `digest.retentionDays` cannot be negative; `digest.weekday` must
  be `0..6` (Sunday..Saturday); `digest.language` (when set) must be `en`/`ms`/`zh`/`ar`.
  `llm.timeoutSeconds` must be `0..600`; `llm.maxTokens` cannot be negative; `llm.sidecar.port`
  must be `0` (unset) or `1..65535`.
- `security` — `jwt.secret` must be at least 16 characters; `allowOrigins` cannot be empty;
  `tls.certPath`/`tls.keyPath` are required; every rate-limit tier's `requests`/`windowSeconds`
  must be non-negative.
- `storage` — `fileStorage.path` is required; `cache.ttlSeconds` cannot be negative.
- `logging` — `logging.maxLineBytes`/`maxFileSizeMb` cannot be negative (`maxFileSizeMb`
  0 = uncapped); a non-empty `telemetry.prometheus.metricsPath` must start with `/`.
- Unknown section — error.

## `applyToConfig(cfg *config.AppConfigModel, section string, data map[string]any) error`

Writes a validated section's values onto the live config model field-by-field (mirroring
`settings.go`'s `read()` shape in reverse), so the UI reflects the pending change immediately —
the host still needs a restart (`apis/system.go`) to consume them. `pairing.enabled` is stored
as a `*bool` (`config.PairingConfigModel.Enabled`) so a config that omits the key keeps
defaulting to enabled; `applyToConfig` always sets it explicitly once the section is saved. The
`agent` case is the same shape: `Digest.Enabled`/`Digest.LocalHour`/`AllowDownloads` are also
written as explicit `*bool`/`*int` pointers on every save, same reasoning; `Digest.WeeklyEnabled`
gets the identical `*bool` treatment (always set explicitly), and `Digest.Weekday` is a plain
`int`.

## `getters` — nested-map accessor

`boundGetters(data)` returns a `getters{data}` with `.s(path)` (string), `.b(path)` (bool),
`.i(path)`/`.i64(path)` (int/int64, coercing `float64`/`int`/`int64`) — all built on
`settings.go`'s `leafAny`. Missing/absent leaves return the zero value rather than an error, so
`applyToConfig` never has to special-case a field the client omitted (it was already
back-filled to the current value by `projectOntoShape` before `commit` is called).

## Notes

- `applyTier(t *config.RateLimitTierConfigModel, g getters, prefix string)` — shared helper for
  the three rate-limit tiers (`devOnly`/`authOnly`/`public`), each `Enabled`/`Requests`/
  `WindowSeconds`.
- Adding a new editable field means updating three places in lock-step: `settings.go`'s
  `read()` (what the UI sees), `validateSection` (what's rejected), and `applyToConfig` (what's
  written) — the field-list source of truth is `read()`; the other two are driven off the same
  dotted paths.
