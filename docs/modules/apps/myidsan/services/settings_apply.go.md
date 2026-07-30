# Module: apps/myidsan/services/settings_apply.go

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
- `sso` — `issuer` and `audience` are required; TTLs cannot be negative; `authCodeTtlSeconds`
  is capped at **600 seconds**. An authorization code is redeemed by the relying app within
  seconds of issue; a long-lived one is a credential sitting in a redirect URL, and therefore
  in browser history, proxy logs, and any Referer header along the way.
- `security` — `jwt.secret` must be at least 16 characters; `allowOrigins` cannot be empty;
  `tls.certPath`/`tls.keyPath` are required; every rate-limit tier's `requests`/`windowSeconds`
  must be non-negative; `loginSecurity.maxAttempts` must be **at least 2** once the lockout is
  enabled (0 or 1 locks an account out on its first attempt — refused rather than silently
  defaulted, since an operator who typed 0 meant something and it was not that);
  `loginSecurity` window/lockout/delay values cannot be negative;
  `passwordPolicy.minLength` must be **at least 8** (floored, not merely non-negative — a
  policy admitting a 4-character password reads as due diligence while permitting exactly
  what it appears to forbid); `mfa.policy` must be one of `off`/`optional`/`required`.
- `storage` — `fileStorage.path` is required; `cache.ttlSeconds` cannot be negative.
- `logging` — `logging.maxLineBytes`/`maxFileSizeMb` cannot be negative (0 = uncapped); a
  non-empty `telemetry.prometheus.metricsPath` must start with `/`.
- Unknown section — error.

## `applyToConfig(cfg *config.AppConfigModel, section string, data map[string]any) error`

Writes a validated section's values onto the live config model field-by-field (mirroring
`settings.go`'s `read()` shape in reverse), so the in-memory config reflects the pending
change immediately — the host still needs a restart to consume it.

`loginSecurity.enabled` and `passwordPolicy.blockCommon` are pointer-valued
(`*bool`) on `config.AppConfigModel`, because the pointer is what distinguishes "absent
from config.json, so resolve to the safe default" from "explicitly set" (see
`config.EffectiveLoginSecurity`/`EffectivePasswordPolicy`). Saving through the editor is
always an explicit choice, so `applyToConfig` takes the address of a local bool rather than
leaving either pointer nil.

## `getters` — nested-map accessor

`boundGetters(data)` returns a `getters{data}` with `.s(path)` (string), `.b(path)` (bool),
`.i(path)`/`.i64(path)` (int/int64, coercing `float64`/`int`/`int64`) — all built on
`settings.go`'s `leafAny`. Missing/absent leaves return the zero value rather than an error,
since `applyToConfig` never has to special-case a field the client omitted (it was already
back-filled to the current value by `projectOntoShape` before `commit` is called).

## Notes

- `applyTier(t *config.RateLimitTierConfigModel, g getters, prefix string)` — shared helper for
  the three rate-limit tiers (`devOnly`/`authOnly`/`public`), each `Enabled`/`Requests`/
  `WindowSeconds`.
- Adding a new editable field means updating three places in lock-step: `settings.go`'s
  `read()` (what the caller sees), `validateSection` (what's rejected), and `applyToConfig`
  (what's written) — the field-list source of truth is `read()`.
- `TestSecurityValidationRefusesUselessPolicies` (`settings_test.go`) and
  `TestLongAuthCodeTTLIsRefused` are the regression guards for the floors/caps above.
