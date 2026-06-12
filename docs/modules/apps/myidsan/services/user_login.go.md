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

## Credential Policy

- User creation rejects identical username/email and password pairs by default.
- A single exception is allowed for first-run bootstrap compatibility: `superadmin` / `superadmin123`.
- The exception only applies when no existing `superadmin` login record is present.

## Local Auth Notes

- Local login maps `username` to the `email` column in `user_login`.
- Accounts with empty password are treated as third-party-managed and blocked from local login/register override.
- New local passwords are hashed using bcrypt before storage.
- Legacy plain-text local passwords are upgraded to bcrypt on successful login.
