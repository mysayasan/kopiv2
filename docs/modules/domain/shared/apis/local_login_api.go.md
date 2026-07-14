# Module: domain/shared/apis/local_login_api.go

## Purpose

`NewLocalLoginApi` registers the PUBLIC appliance sign-in routes: an explicit login endpoint
that exchanges a username and password for a session cookie, and a logout that clears it.
New with the myiotsan P0 scaffolding, shared from the start so mymatasan can adopt it later
without a fork.

## Why this exists (given Basic auth already works)

`NewLocalBasicAuth` (`local_auth.go`) already accepts an HTTP Basic header on every request.
mymatasan works that way, and it cost real throughput: because the SPA replays the stored
Basic credential on every request, every request pays a full bcrypt verification — a k6 load
test put the ceiling at roughly 300 requests/sec, and the fix was a bcrypt result cache
(`domain/shared/services/local_user.go`'s `authCache`), which is a cache of password
verifications and exactly as uncomfortable as it sounds.

Exchanging the credential once for a session cookie pays bcrypt once per **sign-in** instead
of once per **request**, and needs no verification cache. `myiotsan` uses this as its primary
sign-in path; Basic still works for API clients and scripts (and still hits
`NewLocalBasicAuth`'s Basic branch, cache and all).

## Key Function: NewLocalLoginApi

```go
func NewLocalLoginApi(router *mux.Router, cfg LocalAuthConfig, userServ services.ILocalUserService, guard *LoginGuard)
```

Must be mounted on the router BEFORE the authenticated subrouter — it is the endpoint that
authenticates, so it cannot itself sit behind the auth middleware. Registers under `/auth`:

- `POST /auth/login` — verifies a credential (`ILocalUserService.Authenticate`) and issues the
  session cookie (`setLocalAuthCookie`). A failure is deliberately indistinguishable from a
  wrong username: the response never says which half was wrong, so the endpoint cannot be
  used to enumerate accounts. The failed-login lockout (`LoginGuard`) is applied here — this
  is where credential guessing actually happens, now that a session-cookie login exists as a
  dedicated, always-hit endpoint (rather than being inferred from `GET /auth/session` as
  `local_auth.go`'s `isLoginAttemptPath` does for Basic-only appliances).
- `POST /auth/logout` — clears the session cookie (`MaxAge: -1`). Does not require the caller
  to be authenticated: signing out a session that is already gone is a success, not an error.

## Notes

- Body is capped at 64 KiB and decoded with `DisallowUnknownFields`.
- `myiotsan`'s `app.go` registers this on the raw `api` router (public), then mounts
  `NewLocalBasicAuth` + `NewRequireRolePermission` on a `protected` subrouter beneath it —
  order matters: `/auth/login` must not be behind the middleware that demands the credential
  it exists to issue.
- Distinct from `local_auth_api.go`'s `NewLocalAuthApi`, which is the *authenticated*
  self-service surface (session probe + change-password).
