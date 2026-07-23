# Module: apps/myidsan/apis/login.go

## Purpose

Provides authentication endpoints for local credentials and any registered federated
identity provider (`infra/login.Registry`, see `infra/login/provider.go.md`).

## Responsibilities

- Handles local credential login via `POST /api/login/default`.
- Handles local account registration via `POST /api/login/default/register`.
- Sets HttpOnly JWT session cookies for successful local login/register and federated callbacks.
- Sets a readable CSRF cookie that clients echo in `X-CSRF-Token` for unsafe authenticated requests.
- Clears session cookies through logout.
- `NewLoginApi` builds the provider registry via `login.BuildRegistry(oAuth2Conf)` (Google/GitHub register only when both `ClientId` and `ClientSecret` are present) and **returns it**, so `apps/myidsan/app/app.go` can thread the same registry into `NewFederatedAuthApi` — one registry, one provider list, shared by the server-rendered login page, the SPA, and the OAuth routes.
- One generic route pair serves every registered provider: `GET /api/login/{provider}` (`providerLogin`) and `GET /api/callback/{provider}` (`providerCallback`), matched by `mux` against `{provider:[a-z][a-z0-9_.:-]*}` — registered *after* the fixed `/default*`/`/providers` routes so those still win the match. An unregistered provider key 404s with `ErrLimitedAccess` (`"<key> login is not configured"`).
- Prevents local credential takeover of third-party-managed accounts.
- Exposes `GET /api/login/providers` (`listProviders`) — public, no auth — returning the registry's authoritative `list: [{key, displayName}]` (render order) plus legacy `{"google": bool, "github": bool}` booleans for older SPA builds that have not switched to reading `list`.

## Federated Callback Flow (`providerCallback`)

1. Resolve the provider from the registry; 404 if the key is unknown.
2. `provider.Callback(r)` exchanges the code and returns a normalized `login.Identity`; a provider error becomes `ErrStatusUnprocessableEntity`.
3. An identity with no email (e.g. a GitHub account with no public email) is rejected with `ErrStatusUnprocessableEntity` before it ever reaches the user service — no email means no account to show and no valid `Email` claim to issue.
4. `m.userService.UpsertFederated(ctx, *identity)` resolves the identity to a local account (see `apps/myidsan/services/user_login.go.md`). Errors map to responses: `ErrFederatedIdentityConflict`/`ErrInactiveAccount` → `ErrLimitedAccess`; `ErrFederatedIdentityInvalid` → `ErrStatusUnprocessableEntity`; anything else → `ErrInternalServerError`.
5. `setOAuthSession` issues the session cookies from the resolved `*entities.UserLogin` and the `*login.Identity` (display name falls back from `identity.Name` to the stored user's first/last name, then to the account email).
6. Redirect to the `continue` target consumed via `consumeOAuthContinue`.

## Local Auth Contract

- Request login body: `username`, `password`.
- Request register body: `username`, `password`, optional `firstName`, `lastName`.
- `username` maps to `user_login.email`.
- Successful login/register responses return `{ result: { ok: true } }` and set the auth/CSRF cookies.
- Logout is available at `POST /api/login/default/logout` and clears both secure and local-development cookie names.

## Change Password

`POST /api/login/default/change-password` is an authenticated endpoint (JWT cookie required). It verifies the caller's current password, hashes and stores the new one, and clears the `must_change_password` flag so the forced first-login gate is released. Returns `{ ok: true }` on success. Error responses distinguish between an incorrect current password (`ErrAuthFailed`) and a third-party-only account with no local password (`ErrLimitedAccess`). New password must be at least 8 characters (enforced by the service layer).

## Notes

- Federated providers are optional; local credential auth remains available even with an empty registry.
- Provider login start generates per-request state and stores it in an HTTP-only callback cookie (`setOAuthContinue`/provider's own `Login`).
- Provider callbacks validate the returned state before exchanging the provider code.
- Third-party accounts (empty password) are rejected for local credential login/register override.
- Federated login now carries the pending `continue` path (e.g. an `/api/auth/authorize` URL from a relying-app SSO redirect) through the round-trip. `setOAuthContinue` stores a base64-encoded, validated path in a short-lived provider-scoped HttpOnly cookie before the provider redirect; `consumeOAuthContinue` reads and clears it in the callback. `setOAuthSession` no longer writes a response body — control passes to the caller, which performs the redirect to the consumed `continue` target (or `/` when absent). This means a user arriving at myidsan's federated login page via a relying-app SSO redirect lands back at the relying app after completing federated login, not on a raw JSON payload.
- Account resolution for a federated login is centralized in `services.UpsertFederated` (strict `(provider, subject)` matching; a same-email account with no bound identity may claim it once, a bound one is refused) — this file no longer does its own `GetByEmail`-then-`Create` per provider, which is what let Google and GitHub each hand-roll a slightly different account-matching path before.
