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

- `NewLocalBasicAuth(userService, guard, notifier)` builds `sharedapis.NewLocalBasicAuth` with
  `lockoutAuthConfig(notifier)`.
- `NewLocalLoginApi(router, userService, guard, notifier)` — new. Registers mymatasan's PUBLIC
  `POST /api/auth/login` and `POST /api/auth/logout` (`sharedapis.NewLocalLoginApi`, bound with
  `lockoutAuthConfig(notifier)`). Mounted in `wire_routes.go` before the protected subrouter.
  This is what lets the SPA exchange a credential once for the session cookie instead of
  replaying HTTP Basic (and paying a bcrypt verification) on every request, and what lets a
  page reload restore a session via `GET /api/auth/session` instead of signing the user out.
- `lockoutAuthConfig(notifier)` — new. Builds `localAuthConfig()` plus the `OnLockout` hook that
  bridges into `services.NotifyAuthLockout`; shared by `NewLocalBasicAuth` and
  `NewLocalLoginApi` so a lockout tripped by either the Basic middleware or the explicit login
  endpoint is reported the same way. The shared middleware/login-API package itself has no
  notification-stack dependency.
- `localAuthConfig()` is mymatasan's `LocalAuthConfig{AppName: "mymatasan"}`, reused by
  `lockoutAuthConfig`, `local_auth_api.go`'s `NewLocalAuthApi` binding, and by
  `setLocalAuthCookie`/`withLocalUser` here so every caller names the same cookie.
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
- The session cookie (`mymatasan_local_auth`) now slides: a cookie-authenticated request past
  half its 12h TTL is re-issued, so an active session is not dropped mid-shift by a fixed
  clock started at sign-in — see `domain/shared/apis/local_auth.go.md`'s "Sliding session
  cookie" section. Behavior is identical to `myiotsan`/`mypintusan`, since all three bind the
  same shared middleware.
- HTTP Basic remains available server-side for scripts and API clients and still hits
  `NewLocalBasicAuth`'s Basic branch (bcrypt-cached) — but the SPA no longer uses it: it now
  signs in once through `POST /api/auth/login`, so a bad credential from the SPA comes back
  `403` (the login endpoint's answer, deliberately indistinguishable from a wrong username);
  a bad Basic credential on a protected route is still `401`. See `App.js`'s login handler,
  which treats both the same way in the UI.
- The failed-login lockout now has two enforcement points sharing the same `LoginGuard`: the
  shared middleware's login-probe scoping (`GET /auth/session`, `isLoginAttemptPath`, for
  Basic-only clients) and `NewLocalLoginApi`'s own guard check inside the `POST /auth/login`
  handler (the path the SPA actually hits now) — see `domain/shared/apis/local_auth.go.md` and
  `local_login_api.go.md` for the self-lockout scenario this fixes and why both exist.
