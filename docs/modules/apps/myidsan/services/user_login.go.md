# Module: apps/myidsan/services/user_login.go

## Purpose

Implements user credential persistence operations for myidsan identity APIs.

## Responsibilities

- Lists user credential records with caller-provided filters and sorters.
- Uses stable ascending-id order as the default sort (previously newest-first; changed so the user list is deterministic in the UI).
- Resolves user credentials by unique email.
- Creates, updates, and deletes user credentials through the shared generic repository.
- Enforces credential policy for create operations.
- Authenticates local username/password logins.
- `AuthenticateDefault(ctx, username, password) (*UserLogin, error)` no longer discloses account **state** before a credential has been proven. Previously an unknown username, a federated-only account (empty `Userpwd`), and a disabled account each returned a distinct error (or none) immediately off the lookup — a positive account-existence/state oracle: an attacker could learn which usernames were real, and which of those were federated, with no credential at all. Now: an unknown username or a federated-only account both spend a dummy `bcrypt.CompareHashAndPassword` against a fixed `dummyBcryptHash` (a valid cost-10 hash of a value nobody can present) before returning the same generic `ErrInvalidCredential`, so the "no such account" and "wrong password" paths cost about the same amount of time; `ErrInactiveAccount` is only returned **after** the password has verified — a disabled account discloses nothing to a caller who hasn't proven they hold its password. Legacy plaintext passwords are compared with `subtle.ConstantTimeCompare` rather than `!=`, since a byte-wise inequality leaks the stored password's length and a matching prefix through timing.
- Registers local accounts without overriding third-party-only accounts.
- `EnsureStockSuperadmin(ctx, username, password, superRoleId) (StockSeedResult, error)` — seeds the bootstrap admin from `config.localAuth`, forced first-login password change (`MustChangePassword = true`). The password is resolved by `resolveBootstrapPassword` with precedence `LOCAL_ADMIN_PASSWORD` env → `config.localAuth.password` → a freshly `generateBootstrapPassword`'d 16-character value (`crypto/rand`, unambiguous charset excluding `O`/`0`/`I`/`l`/`1`) — an empty config password no longer falls back to a hard-coded default. While the account is still untouched (MustChangePassword + IsActive), a config/env-supplied password is refreshed in on each startup so the operator can correct a typo before first login; a **generated** password is never refreshed in — one is minted fresh each boot instead, and rewriting the hash would invalidate the credential the operator just read from the recovery file. Once the operator has changed the password, config no longer overrides it either way. The account is also pinned to the superadmin role if the role ID drifts. Returns a `StockSeedResult{Username, Password, Generated, Seeded}`: `Seeded` is true only when the account was freshly created (the caller uses this to decide whether to announce the credential — see `apps/myidsan/app/firstrun.go.md`); `Generated` is true when the password had to be invented.
- `ResetStockSuperadmin(ctx, username, password, superRoleId) (StockSeedResult, error)` — the lock-out recovery path, reached only via the `RESET_ADMIN` marker file (`apps/myidsan/app/firstrun.go.md`'s `consumeAdminResetMarker`). Force-sets the password (same `resolveBootstrapPassword` precedence), re-flags `MustChangePassword`, reactivates the account, and re-pins the superadmin role — recovering an account that was deactivated or role-changed at handoff. Recreates the account via `EnsureStockSuperadmin` if it is missing entirely. Always reports `Seeded: true` on success.
- `ChangePassword(ctx, userId, current, next)` — local accounts only; minimum 8 characters; verifies the current password (bcrypt or legacy plain-text); hashes and stores the new password; clears `MustChangePassword`.
- `AdminResetPassword(ctx, userId) (string, error)` — backs the operator forgot-password queue (`services/password_reset.go.md`'s `Resolve`): force-sets a fresh `generateBootstrapPassword`'d temporary password (the same generator the stock-superadmin seed uses), flags `MustChangePassword = true`, and returns the plaintext once for an operator to hand over out-of-band. Refuses `ErrThirdPartyOnlyAccount` when the account has no local password (federated/SSO-only — the reset is meaningless there). Deliberately never touches `IsActive` — a reset must not silently reactivate a disabled account.
- `SetPasswordSelfService(ctx, userId, newPassword) error` — backs the optional SMTP self-service reset link (`services/password_reset.go.md`'s `CompleteSelfService`): the caller already proved possession via a single-use token, so there is no current-password check, but the new password must still meet the 8-character minimum and the account must still be local (`ErrThirdPartyOnlyAccount` otherwise). Clears `MustChangePassword`.
- `UpsertFederated(ctx, id login.Identity)` — resolves any federated login (Google/GitHub today; LDAP/OIDC/etc. later) to a local account with strict `(provider, subject)` matching:
  1. `getBySsoIdentity` looks the account up by its bound `SsoProvider`/`SsoSubject` pair (an `Equal`-filtered `repo.GetSingle`, not `GetByForeign` — which hardcodes `limit=1` on one column and cannot express a two-column match). A hit is refreshed (`applyIdentityProfile`) and returned.
  2. No bound account: an account with the same email is considered ONLY if it has no `SsoProvider`/`SsoSubject` bound yet (a pre-upgrade social user, or an operator pre-provisioning by email) — that account claims the identity once. An email match that is **already** bound to a different identity is refused with `ErrFederatedIdentityConflict` rather than merged — merging by email alone would let a same-email account at another provider take over the existing one (the security fix this change makes: previously Google/GitHub callbacks each did their own `GetByEmail`-then-`Create`, an account-takeover risk).
  3. A full miss creates a new account with `UserRoleId: 0` — **pending clearance**, same as any other new-user path, until a superadmin assigns a role.
  4. An inactive account is refused (`ErrInactiveAccount`) at any of the above paths.
  5. `id.Provider`/`id.Subject`/`id.Email` all being non-empty is a precondition; a provider identity missing any of them is refused with `ErrFederatedIdentityInvalid` rather than creating an unmatchable account.
  `applyIdentityProfile` refreshes only the display fields the provider owns (`FirstName`/`LastName`/`PicUrl`, falling `GivenName` back to `Name` when the provider has no split name) — it never touches credential or role fields.
- `AssignRole(ctx, userId, roleId)` — sets only the account's `UserRoleId`; credentials and profile fields are untouched. Added for federated group→role mapping (`apps/myidsan/services/directory.go.md`'s `AuthenticateLdap`); a no-op (no write) when the account is already on the target role. Like every other role change in the suite, it takes effect on the account's next login/session refresh, not mid-session.
- `Update` preserves the existing stored password when the incoming `userpwd` field is blank, and hashes the password when a plaintext value is supplied, so role/active toggles from the admin UI do not erase or store the password in plain text. The password-preserve lookup now uses `repo.GetById` (primary key) instead of `GetByUnique(ctx,"","id",id)`, which matched no field and would return the first user's password.
- `ChangePassword` similarly uses `repo.GetById` to fetch the user before verifying the current password.

## Credential Policy

- User creation rejects identical username/email and password pairs by default.
- The legacy single exception for `superadmin`/`superadmin123` has been replaced by `EnsureStockSuperadmin`, which reads credentials from `config.localAuth` and does not hard-code any credential.

## Local Auth Notes

- Local login maps `username` to the `email` column in `user_login`.
- Accounts with empty password are treated as third-party-managed and blocked from local login/register override.
- New local passwords are hashed using bcrypt before storage.
- Legacy plain-text local passwords are upgraded to bcrypt on successful login.
