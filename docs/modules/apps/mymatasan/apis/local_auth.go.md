# Module: apps/mymatasan/apis/local_auth.go

## Purpose

No longer the implementation. The local-auth middleware, the failed-login guard, and the RBAC
gate now live in `domain/shared/apis` so every appliance app (mymatasan, myiotsan) runs the
same code (myiotsan is the second appliance app and needed exactly what mymatasan had — see
`domain/shared/apis/local_auth.go.md`, `login_guard.go.md`, `authorization.go.md`). This file
is now the **binding**: it supplies the parts that are genuinely mymatasan's — its app name
(which names the session cookie and the Basic realm) and its notification stack (which
surfaces a lockout as a security event) — and re-exports the rest as thin wrappers/aliases.

## Responsibilities

- `NewLocalBasicAuth(userService, guard, notifier)` builds `sharedapis.NewLocalBasicAuth`
  with `LocalAuthConfig{AppName: "mymatasan", OnLockout: ...}`, where `OnLockout` bridges into
  `services.NotifyAuthLockout` — the shared middleware itself has no notification-stack
  dependency.
- `localAuthConfig()` is mymatasan's `LocalAuthConfig{AppName: "mymatasan"}`, reused by
  `local_auth_api.go`'s `NewLocalAuthApi` binding and by `setLocalAuthCookie`/`withLocalUser`
  here so every caller names the same cookie.
- `LoginGuard`/`LoginGuardConfig`/`NewLoginGuard` — type aliases and a constructor
  re-exporting `sharedapis`'s failed-login lockout unchanged.
- `LocalUserFromContext(ctx)` — re-exports `sharedapis.LocalUserFromContext`.
- `withLocalUser(ctx, user)` — re-exports `sharedapis.WithLocalUser`; this is the seam
  `apis/control_dispatch.go` uses to inject its synthetic tunneled-command principal (see
  `control_dispatch.go.md`).
- `setLocalAuthCookie(w, r, user)` — re-exports `sharedapis.SetLocalAuthCookie` bound to
  mymatasan's `localAuthConfig()`; the `*http.Request` is forwarded so the shared
  implementation can set the cookie's `Secure` flag from `middlewares.IsSecureRequest(r)`.
- `NewRequireRolePermission(roles, perms)` — re-exports `sharedapis.NewRequireRolePermission`
  unchanged. This absorbed what used to be a separate file,
  `apps/mymatasan/apis/authorization.go` (now deleted — its content moved to
  `domain/shared/apis/authorization.go`; see `domain/shared/apis/authorization.go.md` for the
  full behavior).

## Notes

- This binding is app-local and does not use MyIDSan JWT sessions or RBAC.
- Behavior for mymatasan is unchanged by the move — verified by booting on a fresh DB: the
  forced-change gate still returns `password_change_required`, the login probe still issues a
  cookie still named `mymatasan_local_auth`, cookie-only auth still works, a bad credential is
  still `401`, and the login-probe-only lockout scoping is unchanged.
- The scoping of the failed-login lockout to only the interactive login probe (`GET
  /auth/session`) — not every protected route a client's stale credential might hit — lives in
  the shared middleware now; see `domain/shared/apis/local_auth.go.md` for the self-lockout
  scenario this fixes.
