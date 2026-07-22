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
- Registers local accounts without overriding third-party-only accounts.
- `EnsureStockSuperadmin(ctx, username, password, superRoleId)` — seeds the bootstrap admin from `config.localAuth`, forced first-login password change (`MustChangePassword = true`). While the account is still untouched (MustChangePassword + IsActive), the password is refreshed from config on each startup so the operator can correct a typo before first login. Once the operator has changed the password, config no longer overrides it. The account is also pinned to the superadmin role if the role ID drifts.
- `ChangePassword(ctx, userId, current, next)` — local accounts only; minimum 8 characters; verifies the current password (bcrypt or legacy plain-text); hashes and stores the new password; clears `MustChangePassword`.
- `UpsertFederated(ctx, id login.Identity)` — resolves any federated login (Google/GitHub today; LDAP/OIDC/etc. later) to a local account with strict `(provider, subject)` matching:
  1. `getBySsoIdentity` looks the account up by its bound `SsoProvider`/`SsoSubject` pair (an `Equal`-filtered `repo.GetSingle`, not `GetByForeign` — which hardcodes `limit=1` on one column and cannot express a two-column match). A hit is refreshed (`applyIdentityProfile`) and returned.
  2. No bound account: an account with the same email is considered ONLY if it has no `SsoProvider`/`SsoSubject` bound yet (a pre-upgrade social user, or an operator pre-provisioning by email) — that account claims the identity once. An email match that is **already** bound to a different identity is refused with `ErrFederatedIdentityConflict` rather than merged — merging by email alone would let a same-email account at another provider take over the existing one (the security fix this change makes: previously Google/GitHub callbacks each did their own `GetByEmail`-then-`Create`, an account-takeover risk).
  3. A full miss creates a new account with `UserRoleId: 0` — **pending clearance**, same as any other new-user path, until a superadmin assigns a role.
  4. An inactive account is refused (`ErrInactiveAccount`) at any of the above paths.
  5. `id.Provider`/`id.Subject`/`id.Email` all being non-empty is a precondition; a provider identity missing any of them is refused with `ErrFederatedIdentityInvalid` rather than creating an unmatchable account.
  `applyIdentityProfile` refreshes only the display fields the provider owns (`FirstName`/`LastName`/`PicUrl`, falling `GivenName` back to `Name` when the provider has no split name) — it never touches credential or role fields.
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
