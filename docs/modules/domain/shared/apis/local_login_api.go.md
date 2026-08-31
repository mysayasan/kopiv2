# Module: domain/shared/apis/local_login_api.go

## Purpose

`NewLocalLoginApi` registers the PUBLIC appliance sign-in routes: an explicit login endpoint
that exchanges a username and password for a session cookie, and a logout that clears it.
New with the myiotsan P0 scaffolding, shared from the start so the other appliance apps could
adopt it later without a fork — `mypintusan` did at launch, and `mymatasan` adopted it too
(`apps/mymatasan/apis/local_auth.go`'s `NewLocalLoginApi` binding,
`apps/mymatasan/app/wire_routes.go`), so all three now share this as their primary sign-in
path.

## Why this exists (given Basic auth already works)

`NewLocalBasicAuth` (`local_auth.go`) already accepts an HTTP Basic header on every request.
mymatasan used to work that way — the SPA replayed the stored Basic credential on every
request — and it cost real throughput: every request paid a full bcrypt verification, and a k6
load test put the ceiling at roughly 300 requests/sec. The fix at the time was a bcrypt result
cache (`domain/shared/services/local_user.go`'s `authCache`), which is a cache of password
verifications and exactly as uncomfortable as it sounds.

Exchanging the credential once for a session cookie pays bcrypt once per **sign-in** instead
of once per **request**, and needs no verification cache. All three appliance apps
(`myiotsan`, `mypintusan`, `mymatasan`) use this as their primary sign-in path; Basic still
works for API clients and scripts (and still hits `NewLocalBasicAuth`'s Basic branch, cache and
all). mymatasan's SPA (`apps/mymatasan/views/react-webpack/src/views/App.js`) now also probes
`GET /api/auth/session` once on boot to restore a session across a page reload — previously the
SPA held the password in memory and lost it on every refresh, which looked exactly like being
signed out.

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
- `POST /auth/logout` — clears the session cookie (`MaxAge: -1`), mirroring the same
  conditional `Secure` flag (`middlewares.IsSecureRequest(r)`) `setLocalAuthCookie` used when
  it set the cookie, so the clearing `Set-Cookie` matches and actually deletes it. Does not
  require the caller to be authenticated: signing out a session that is already gone is a
  success, not an error.

## Notes

- Body is capped at 64 KiB and decoded with `DisallowUnknownFields`.
- `myiotsan`'s and `mypintusan`'s `app.go`, and now `mymatasan`'s `app/wire_routes.go`, all
  register this on the raw `api` router (public) before mounting `NewLocalBasicAuth` +
  `NewRequireRolePermission` on a `protected` subrouter beneath it — order matters: `/auth/login`
  must not be behind the middleware that demands the credential it exists to issue.
- Distinct from `local_auth_api.go`'s `NewLocalAuthApi`, which is the *authenticated*
  self-service surface (session probe + change-password).
