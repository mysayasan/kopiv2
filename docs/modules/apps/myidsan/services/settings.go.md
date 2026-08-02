# Module: apps/myidsan/services/settings.go

## Purpose

Implements `ISettingsService`, an in-app, superadmin-gated editor over a SAFE SUBSET of
`myidsan`'s `config.json` — the first in-app settings surface myidsan has had; ported from
`myseliasan`'s pattern (`docs/MYIDSAN_PRODUCTIZATION_PLAN.md` §4.1), now with its own React
frontend (`views/react-webpack/src/views/components/settings.js`, `apis/settings.go.md`).
The scope deliberately excludes the blocks that would take the app offline if mis-set
(`db`, `server`, `bootstrap`): those stay file-only.

## Persistence model

Identical to myseliasan's, because myidsan's editable config is likewise infra-level, read
once by the shared apphost at boot — there is no seam for a DB value to override it at
startup:

- The authoritative CURRENT value lives in `config.json`, which the host re-reads on restart;
  `deps.Config` mirrors it in memory. `Get`/`GetAll` read from the live in-memory config.
- `Save` validates, updates the in-memory config so the caller sees the pending value
  immediately, writes the changed leaves back into `config.json` (`settings_materialize.go`),
  and reports `needsRestart: true` — the change only takes effect once the process relaunches.
- A one-time DB snapshot of the original config (`RuntimeSetting` row keyed
  `settings.defaults`, captured once on first run via `ensureDefaults` and never overwritten)
  backs `Reset`.

Secrets (`localAuth.password`, `sso.internalToken`, `jwt.secret`, `cache.redis.password`) are
never returned by `Get`/`GetAll`: the leaf is blanked and a sibling `"<field>Set"` boolean is
added. `Save` treats a blank (or omitted) incoming secret as "keep the current value".

## Sections

`sectionOrder`: `localAuth`, `sso`, `security`, `storage`, `logging`. `read(section)` builds
each section's root-relative nested map straight from `*config.AppConfigModel` — see the
switch in `settings.go` for the exact field list per section.

## Deliberately NOT editable here (the differences from a straight myseliasan port)

Beyond the `db`/`server`/`bootstrap` blocks myseliasan also withholds:

- **`audit.retention`.** The security trail's one removal path is config-file-only ON
  PURPOSE, so trimming it needs filesystem access to the server rather than a session on
  it — the superadmin whose own actions the trail records cannot reach for it from inside
  the product. Surfacing it in this editor would hand back exactly the capability that
  design withholds. Guarded by two tests in `settings_test.go`
  (`TestAuditRetentionIsNotReachableFromTheSettingsEditor`,
  `TestSavingAuditRetentionIsRefused`), including that it cannot be smuggled through the
  payload of a section that *is* editable — `projectOntoShape` drops any key not in the
  target section's shape.
- **`kerberos`.** Enabling it requires placing a keytab on the host anyway, so it is
  already a filesystem-level setup step.
- **`login.oidc[]` and the social providers.** These carry per-provider client secrets and
  are better handled by the dedicated Apps/Federation screens than a generic settings form.
- **`webauthn`.** Not yet one of the editable sections (`sectionOrder` above predates it) —
  `webauthn.relyingPartyId` in particular still needs a direct `config.json` edit today,
  including ahead of a disaster-recovery restore onto a differently-named host (see
  `apps/myidsan/services/backup.go.md` and `entities/user_webauthn_credential.go.md`).

`sso` also differs from a straight port: myidsan is the SSO **provider**, so only the
issuing side of the shared config block is exposed. `providerBaseUrl`/`clientId`/
`clientSecret`/`redirectBaseUrl`/`redirectPath` describe a relying app pointing *at* an IdP
and mean nothing on the IdP itself, so `read("sso")` omits them entirely.

## ISettingsService

- `Sections() []string` — the editable section ids in display order.
- `Get(section) (map[string]any, error)` — one section's current values, secrets masked.
- `GetAll() map[string]any` — every section keyed by id, secrets masked.
- `Save(ctx, section, body json.RawMessage) (SaveResult, error)` — projects the request body
  onto the section's known shape (`projectOntoShape`), restores a blank secret to its current
  value, then delegates to `commit`.
- `Reset(ctx, section) (SaveResult, error)` — loads the first-run snapshot, projects it onto
  the section's *current* shape, then delegates to `commit`.
- `TestCache(ctx, body json.RawMessage) error` — pings Redis with the settings in `body`
  (`address`, `password`, `db`, `useTls`, `connectTimeoutMs`, `operationTimeoutMs`); a blank
  `address`/`password` falls back to the currently configured value. Builds a throwaway
  `cache.NewRedisStore`, calls `Ping` under a timeout, and closes the store regardless of
  outcome — it never touches `s.cfg` or `config.json`.

`commit(ctx, section, data)` — the single write path both `Save` and `Reset` funnel through:
`validateSection` (`settings_apply.go`) → `applyToConfig` (updates `*config.AppConfigModel`
in place) → `materializeConfig` (`settings_materialize.go`). Always returns
`SaveResult{NeedsRestart: true}` on success.

## `security` section: resolved policy, not raw struct

`read("security")` populates `loginSecurity`/`passwordPolicy`/`mfa` from
`s.cfg.LoginSecurity.Effective()` / `s.cfg.PasswordPolicy.Effective()` / `s.cfg.Mfa.Effective()`
rather than the raw config fields. This is the section that makes the editor matter more on
myidsan than elsewhere: those three blocks are **absent from the shipped config.json** and
resolve through defaults, so reading the zero-valued struct directly would report the
lockout as off, the password floor as 0, and the MFA policy as blank — all wrong, and each
one an invitation for an operator to "fix" a setting that was never broken. See
`effectiveLoginSecurityMap`/`effectivePasswordPolicyMap`/`effectiveMfaPolicyMap`.

## Constructor

`NewSettingsService(cfg, cfgPath, db, cipher, logf)` — `cfg` is the live
`*config.AppConfigModel`, `cfgPath` the on-disk `config.json` path, `db` builds the
`RuntimeSetting` generic repo, `cipher` is myidsan's `*atrest.Cipher` (may be nil) used to
encrypt the first-run defaults snapshot. **`db` is nil-tolerant**: a nil database disables
only the defaults snapshot (and therefore `Reset`); `Get`/`Save`/`TestCache` still work
because they never touch the database. This is what lets `settings_test.go` construct the
service with `nil, nil` for db/cipher. Calls `ensureDefaults` once at construction when `db`
is non-nil; a capture failure only disables `Reset` (logged via `logf`, never fails boot).
Wired in `app.go`'s `RegisterAppRoutes`.

## Notes

- `ensureDefaults` never overwrites an existing `settings.defaults` row.
- The nested-map helpers (`projectOntoShape`, `maskCopy`, `flattenPatches`, `leafAny`/
  `leafString`/`setLeaf`) are the data-driven plumbing shared by `Get`/`Save`/`Reset`.
- `sectionSecrets` is the single place that lists which dotted leaf per section is a secret.
- See `services/settings_apply.go.md` for validation/typed-apply,
  `services/settings_materialize.go.md` for the config.json write-back, and
  `apis/settings.go.md` for the HTTP surface and superadmin gate.
- `TestCache` bypasses `commit`/`validateSection`/`applyToConfig`/`materializeConfig`
  entirely — it never writes `s.cfg` or `config.json`.
