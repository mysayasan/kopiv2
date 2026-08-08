# Module: domain/shared/apis/local_auth_api.go

## Purpose

`NewLocalAuthApi` registers the authenticated-user self-service routes, shared by every
appliance app: the session probe an SPA reads to discover the forced-change flag, and the
password-change endpoint that clears it. Moved here from
`apps/mymatasan/apis/local_auth_api.go` (behavior-preserving: mymatasan now binds it via a
one-line wrapper, `apps/mymatasan/apis/local_auth_api.go`).

## Key Function: NewLocalAuthApi

```go
func NewLocalAuthApi(router *mux.Router, cfg LocalAuthConfig, userServ services.ILocalUserService)
```

Registers under `/auth` on the given router (the app's protected `/api` subrouter):

- `GET /auth/session` — returns the current authenticated identity (`AuthenticatedUser`),
  including whether a forced password change is pending. This is also the request
  `isLoginAttemptPath` (`local_auth.go`) treats as "an interactive login attempt" for the
  failed-login lockout.
- `POST /auth/change-password` — verifies the current password and sets a new one
  (`ILocalUserService.ChangePassword`), rotating the session cookie
  (`setLocalAuthCookie(w, r, cfg, ...)`) so the old credential's session hash is invalidated.
  Body is capped at 64 KiB and decoded with `DisallowUnknownFields`.

Both routes stay reachable while a user is gated by `NewLocalBasicAuth`'s forced-change check
(`isPasswordChangeAllowedPath`).

## Notes

- `cfg LocalAuthConfig` is the same per-app binding `NewLocalBasicAuth` takes (app name →
  cookie name), so the rotated cookie on password change matches the one the auth middleware
  reads.
- Distinct from `local_login_api.go`'s `NewLocalLoginApi`: this is the *authenticated*
  self-service surface (session probe + change-password); the login API is the *public*
  sign-in/sign-out surface.
