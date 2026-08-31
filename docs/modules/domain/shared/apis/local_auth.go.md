# Module: domain/shared/apis/local_auth.go

## Purpose

The appliance Basic + session-cookie auth middleware, shared by every appliance app
(`mymatasan`, `myiotsan`, `mypintusan`). Moved here from `apps/mymatasan/apis/local_auth.go`
(behavior-preserving: mymatasan binds it via `apps/mymatasan/apis/local_auth.go`, now a thin
wrapper) so a second appliance app runs the same middleware instead of a fork.

## Key Type: LocalAuthConfig

```go
type LocalAuthConfig struct {
    AppName   string
    OnLockout func(ctx context.Context, info LockoutInfo)
}
```

What used to be hardcoded (mymatasan's app name, and a direct call into mymatasan's
notification stack) is now supplied by the app:

- `AppName` names the session cookie (`<AppName>_local_auth`) and the Basic realm. Two
  appliance apps served from the same host must not collide on a cookie, and a realm that
  lies about which app is asking is a phishing aid.
- `OnLockout`, when set, is called once when a lockout trips — a `func` rather than a
  notification-service interface, so this package does not depend on any app's notification
  stack. mymatasan's binding (`apps/mymatasan/apis/local_auth.go`) wires it to
  `services.NotifyAuthLockout`; myiotsan's (`apps/myiotsan/app/app.go`) publishes directly
  through its own `notification.Service`.

## Key Function: NewLocalBasicAuth

```go
func NewLocalBasicAuth(cfg LocalAuthConfig, userService services.ILocalUserService, guard *LoginGuard) func(http.Handler) http.Handler
```

- Validates incoming HTTP Basic Auth credentials through `ILocalUserService`.
- Sets a short-lived HTTP-only cookie (`cfg.cookieName()`) so browser MJPEG/video elements
  that cannot send a Basic header can authenticate.
- Falls back to the session cookie when no Basic header is present, revalidating it against
  the local user database (`AuthenticateSession`) so password resets and inactive users take
  effect immediately. A cookie-authenticated request past half the cookie's TTL re-issues the
  cookie (`shouldRenewLocalAuthCookie`) — see "Sliding session cookie" below.
- Honors an already-injected principal (`LocalUserFromContext`) without re-authenticating —
  the seam a control-channel dispatcher (`apis.NewControlDispatcher`,
  `WithLocalUser`/`withLocalUser`) uses to inject a pre-verified synthetic principal.
- Attaches the authenticated user to request context for downstream handlers and the
  authorization middleware.
- Fails closed with `401` when credentials are missing, wrong, inactive, or not configured —
  deliberately **without** a `WWW-Authenticate: Basic` header, since the SPA owns its own
  login UI and sending that header would pop the browser's native Basic-auth dialog on any
  401 (including `<img>`/`<video>` media tiles).
- Enforces the forced-password-change gate: a `MustChangePassword` user reaches nothing but
  `isPasswordChangeAllowedPath` routes until they change it.
- Throttles failed sign-ins via `LoginGuard` (per-IP, escalating lockout → `429`), but
  **only for the interactive login probe** `GET /auth/session` (`isLoginAttemptPath`) — this
  is the enforcement point for a Basic-only client. `NewLocalLoginApi` (`local_login_api.go`)
  shares the same `*LoginGuard` and applies it at its own `POST /auth/login`, which is the
  path an app mounting that endpoint (`mymatasan`, `myiotsan`, `mypintusan`) actually hits.
  A wrong Basic credential on any other protected route (background polls, an SSE reconnect,
  media tiles, page-load data fetches — all of which a client replays its stored credential
  on) is denied `401` but does **not** count toward or trip the lockout — see the function
  comment for the self-lockout scenario this fixes.

## Sliding session cookie

The cookie value is `username:sessionHash:issuedAtUnix` (`localAuthCookieValue`) — the third
field over the pre-existing two is what lets a stateless middleware answer "how old is this
cookie" without a server-side session table to look it up in.

- `localAuthCookieTTL` is 12h; `localAuthCookieRenewAfter` is half that. A cookie-authenticated
  request older than the half-life gets a fresh `Set-Cookie` (`shouldRenewLocalAuthCookie`),
  so an active session is continuously extended instead of dying on a fixed clock started at
  sign-in. Before this, only a request carrying a Basic header re-issued the cookie, so a
  cookie-only client (every SPA that signs in through `NewLocalLoginApi` and never replays
  Basic) had a hard 12h wall regardless of activity.
- `parseLocalAuthCookie` reads both shapes: the legacy two-field cookie already in a browser at
  upgrade time parses with a zero `issuedAt`, which `shouldRenewLocalAuthCookie` treats as due —
  so it is silently upgraded to the three-field form on that browser's next request. No forced
  re-login on upgrade.
- `shouldRenewLocalAuthCookie` also treats a future-dated `issuedAt` (`age < 0`, e.g. a clock
  that moved) as due, on the same "re-issuing is the safe answer" logic.
- The client could lie about the issued-at field, but the only thing that buys it is a
  re-issued cookie for a session whose hash already authenticated it — the hash, not this
  field, is the credential.

## Notes

- `WithLocalUser(ctx, user) context.Context` and `SetLocalAuthCookie(w, r, cfg, user)` are
  exported specifically for an app's control-channel dispatcher and login API to use; the
  context key itself stays unexported so no network client can forge a principal.
- `setLocalAuthCookie`/`SetLocalAuthCookie` take the inbound `*http.Request` and set the
  cookie's `Secure` flag from `middlewares.IsSecureRequest(r)` — on whenever the request
  arrived over TLS (directly or via a trusted proxy's `X-Forwarded-Proto`), off otherwise.
  It is conditional, not hardcoded, because these appliances are legitimately deployed on a
  LAN over plain HTTP (`server.nonTlsPorts`); hardcoding `Secure` would make the browser
  silently drop the cookie there. `local_login_api.go`'s logout clears the cookie with the
  same conditional flag, since a mismatched `Secure` attribute makes the browser treat the
  clearing `Set-Cookie` as a different cookie and the session survives "logout".
- `LocalUserFromContext(ctx)` is the read side of the same context key.
- `clientIP` deliberately uses `RemoteAddr`, not `X-Forwarded-For`, so a client cannot spoof a
  header to dodge its own lockout — deployments behind a trusted proxy should terminate here.
- `myiotsan`, `mypintusan`, and now `mymatasan` additionally mount `NewLocalLoginApi`
  (`local_login_api.go.md`) as an explicit session-cookie login endpoint, so their SPAs do not
  need to hold the password and replay Basic on every request — see that doc for why. Basic
  remains available on all three for scripts and API clients (and still hits this middleware's
  Basic branch, cache and all).
- **CSRF posture, now documented at `setLocalAuthCookie`**: the appliance session cookie carries
  NO companion CSRF token, deliberately — `SameSite=Lax` IS the defence. A browser will not
  attach a `Lax` cookie to a cross-site `POST`/`PUT`/`DELETE` (the classic CSRF vector, a form
  on `evil.com` submitting to the appliance); the only cross-site requests `Lax` still carries
  are top-level GET navigations, which change nothing here. This is distinct from the JWT stack
  (`domain/utils/middlewares`), which DOES issue a double-submit CSRF token because it serves a
  federated SSO app where a session may legitimately arrive mid-redirect — an appliance on a LAN
  has no such flow. If a token is ever added to this cookie, it must be added to mymatasan,
  myiotsan, and mypintusan at the same time, since all three run this same middleware.
