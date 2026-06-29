# Module: apps/myidsan/apis/login.go

## Purpose

Provides authentication endpoints for local credentials and optional OAuth providers.

## Responsibilities

- Handles local credential login via `POST /api/login/default`.
- Handles local account registration via `POST /api/login/default/register`.
- Sets HttpOnly JWT session cookies for successful local login/register and OAuth callbacks.
- Sets a readable CSRF cookie that clients echo in `X-CSRF-Token` for unsafe authenticated requests.
- Clears session cookies through logout.
- Mounts Google/GitHub login and callback routes only when each provider config is present.
- Prevents local credential takeover of third-party-managed accounts.

## Local Auth Contract

- Request login body: `username`, `password`.
- Request register body: `username`, `password`, optional `firstName`, `lastName`.
- `username` maps to `user_login.email`.
- Successful login/register responses return `{ result: { ok: true } }` and set the auth/CSRF cookies.
- Logout is available at `POST /api/login/default/logout` and clears both secure and local-development cookie names.

## Change Password

`POST /api/login/default/change-password` is an authenticated endpoint (JWT cookie required). It verifies the caller's current password, hashes and stores the new one, and clears the `must_change_password` flag so the forced first-login gate is released. Returns `{ ok: true }` on success. Error responses distinguish between an incorrect current password (`ErrAuthFailed`) and a third-party-only account with no local password (`ErrLimitedAccess`). New password must be at least 8 characters (enforced by the service layer).

## Notes

- OAuth providers are optional; local credential auth remains available even without Google/GitHub configuration.
- OAuth login start generates per-request state and stores it in an HTTP-only callback cookie.
- OAuth callbacks validate the returned state before exchanging the provider code.
- Third-party accounts (empty password) are rejected for local credential login/register override.
