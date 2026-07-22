# Module: apps/myidsan/services/directory.go

## Purpose

Implements `IDirectoryService`: the admin-facing directory (LDAP/AD) configuration
CRUD, the "Test connection" probe, and the login-time bridge from
`infra/login.LdapAuthenticate` (and, for Kerberos, `infra/login.LdapLookup`) to a
local `entities.UserLogin` account with group→role mapping applied.

## Responsibilities

- `GetView(ctx)` / `Save(ctx, payload, actorId)` — singleton `DirectoryConfig` row
  (`Name = "default"`) read/write. `GetView` returns a zero `DirectoryConfigView` when
  unconfigured (so the admin form renders empty rather than 404ing) and never returns
  the bind password, only `HasBindPassword`. `Save` validates the LDAP settings
  (`LdapSettings.Validate`) only when `Enabled` is being set true; a blank
  `BindPassword` in the payload preserves the stored one, so editing unrelated fields
  never forces re-entering the secret.
- `Test(ctx, payload)` — runs `infra/login.LdapTest` against the **submitted** form
  values (not the saved row), so an admin can validate an edit before saving it. A
  blank submitted bind password falls back to the stored, decrypted one. The result
  (including a failure) is returned as a normal 200 from the API — "the test ran and
  here is what happened" is itself a successful admin operation.
- `LoginOption(ctx) (enabled bool, label string)` — whether directory login should be
  offered right now and under what label (`DisplayLabel`, defaulting to "Domain
  account"). Consumed by `GET /api/login/providers` and both login pages so a
  disabled directory never renders a dead account-type choice.
- `AuthenticateLdap(ctx, username, password) (*entities.UserLogin, error)` — the
  password login-time path: `loadEnabled` (below) then
  `infra/login.LdapAuthenticate`, then `admitIdentity` (below).
- `ResolveDirectoryUser(ctx, username) (*entities.UserLogin, error)` — the
  Kerberos SPNEGO login-time path: `apps/myidsan/apis/login.go`'s
  `kerberosLogin` has already verified the caller's identity via a ticket
  (`infra/login.KerberosAuthenticator.Negotiate`); this resolves the verified
  **username only** (no password, no credential check performed here — the doc
  comment says so explicitly) via `loadEnabled` then `infra/login.LdapLookup`
  (service-bind search, no user bind), then the same `admitIdentity` tail as
  `AuthenticateLdap` — so a Kerberos-verified principal resolves to the exact
  same `(ldap, objectGUID)` account, with the same group→role mapping, that a
  password login for the same person would. `ErrDirectoryDisabled` when no
  directory is configured/enabled — the caller (`kerberosLogin`) then falls
  back to `login.StandaloneKerberosIdentity` for directory-less installs.
- `loadEnabled(ctx) (*DirectoryConfig, login.LdapSettings, error)` — shared
  config-load helper factored out of `AuthenticateLdap`/`ResolveDirectoryUser`:
  loads the singleton row (`ErrDirectoryDisabled` if missing/disabled), decrypts
  the bind password, and returns ready-to-use `LdapSettings`.
- `admitIdentity(ctx, cfg, identity) (*entities.UserLogin, error)` — shared tail
  of every directory-backed login, factored out of the old single
  `AuthenticateLdap` body:
  1. Resolves the `Identity` to a local account via the existing
     `IUserLoginService.UpsertFederated` (same strict `(provider, subject)` matching
     every other federated login uses — see `user_login.go.md`).
  2. `resolveRole` looks up `FederatedGroupMapping` rows for `Provider == "ldap"` and
     calls the package-level `ResolveMappedRole` against the identity's groups. A
     match is applied via `IUserLoginService.AssignRole` when `cfg.Authoritative` is
     true (every login re-applies the mapped role) **or** the account's current
     `UserRoleId == 0` (a still-pending account is seeded once). A manually assigned
     role on a non-authoritative directory is never overwritten.
- `ResolveMappedRole(mappings, groups) (roleId int64, matched bool)` — pure resolution
  logic, exported for direct unit testing: group matching is case-insensitive on the
  full name/DN; among matches, highest `Priority` wins, ties break to the lowest `Id`
  for a deterministic outcome; no match returns `(0, false)` and the caller must not
  touch the account's role.
- `encodeDirectorySecret` / `decodeDirectorySecret` — the bind password's at-rest
  encryption, mirroring the suite-wide convention (`apps/myseliasan/services/secret_store.go.md`):
  AES-256-GCM via `infra/atrest`, base64-wrapped for the TEXT column. `encode` is a
  passthrough when the cipher is nil (encryption-at-rest disabled) or the plaintext is
  empty. `decode` passes stored plaintext through unchanged when it doesn't decode as
  base64 or isn't recognized as an `atrest`-encrypted blob (`atrest.IsEncrypted`) — so
  enabling encryption later, or running with it off, never locks an existing
  configuration out.

## Errors

`ErrDirectoryDisabled` — a directory-backed login attempted while no enabled
directory is configured. `AuthenticateLdap`'s caller (`apis/login.go`'s
`ldapLogin`) maps it to `ErrLimitedAccess`; `ResolveDirectoryUser`'s caller
(`kerberosLogin`) instead treats it as "no directory to resolve through" and
falls back to `login.StandaloneKerberosIdentity` — a Kerberos ticket alone is
still enough to sign in on a directory-less install.

## Notes

- `directoryService` depends on `IUserLoginService` for both `UpsertFederated` and
  the new `AssignRole` — it never touches the `user_login` table directly.
- Covered by `directory_test.go`: `ResolveMappedRole`'s priority/tie-break/no-match/
  inert-role cases, and the secret encode/decode round trip + legacy-plaintext/
  nil-cipher passthrough.
