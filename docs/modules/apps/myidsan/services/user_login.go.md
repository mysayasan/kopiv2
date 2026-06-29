# Module: apps/myidsan/services/user_login.go

## Purpose

Implements user credential persistence operations for myidsan identity APIs.

## Responsibilities

- Lists user credential records with caller-provided filters and sorters.
- Uses default newest-first sorting when callers do not provide sorters.
- Resolves user credentials by unique email.
- Creates, updates, and deletes user credentials through the shared generic repository.
- Enforces credential policy for create operations.
- Authenticates local username/password logins.
- Registers local accounts without overriding third-party-only accounts.
- `EnsureStockSuperadmin(ctx, username, password, superRoleId)` — seeds the bootstrap admin from `config.localAuth`, forced first-login password change (`MustChangePassword = true`). While the account is still untouched (MustChangePassword + IsActive), the password is refreshed from config on each startup so the operator can correct a typo before first login. Once the operator has changed the password, config no longer overrides it. The account is also pinned to the superadmin role if the role ID drifts.
- `ChangePassword(ctx, userId, current, next)` — local accounts only; minimum 8 characters; verifies the current password (bcrypt or legacy plain-text); hashes and stores the new password; clears `MustChangePassword`.
- `Update` now preserves the existing stored password when the incoming `userpwd` field is blank, and hashes the password when a plaintext value is supplied, so role/active toggles from the admin UI do not erase or store the password in plain text.

## Credential Policy

- User creation rejects identical username/email and password pairs by default.
- The legacy single exception for `superadmin`/`superadmin123` has been replaced by `EnsureStockSuperadmin`, which reads credentials from `config.localAuth` and does not hard-code any credential.

## Local Auth Notes

- Local login maps `username` to the `email` column in `user_login`.
- Accounts with empty password are treated as third-party-managed and blocked from local login/register override.
- New local passwords are hashed using bcrypt before storage.
- Legacy plain-text local passwords are upgraded to bcrypt on successful login.
