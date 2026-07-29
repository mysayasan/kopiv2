# Module: apps/myidsan/apis/session.go

## Purpose

Session listing and revocation, over `services.ISessionService` (`services/session.go.md`).

## Routes

Self-service (authenticated, own account only — deliberately **not** behind
`access.Middleware`'s RBAC matrix, mirroring `/api/mfa` and
`/api/login/default/change-password`: reviewing/ending YOUR OWN sessions is not a
privileged action, and gating it behind a grant would mean an ordinary user could not
respond to their own laptop being stolen):

- `GET /api/session` (`listOwn`) — the caller's own sessions.
- `DELETE /api/session/{sessionId}` (`revokeOwn`) — ends one of the caller's own.
  Ownership is checked against the caller's own session list rather than taken on trust
  (without this, any signed-in user could end any session on the server by guessing or
  replaying an id); a session not found in that list gets the same `404` as one that does
  not exist at all, so this is not a probe for real session ids. Registered **last** among
  the fixed `/session` paths so `/revoke-all` still matches first.
- `POST /api/session/revoke-all` (`revokeAllOwn`) — ends all of the caller's sessions
  except the current one, so "sign out everywhere else" leaves the person who asked for it
  still signed in.

Administration (superadmin, `access.Middleware` + `access.RequireSuperadmin` — acting on
another account is privilege-affecting, the same split `/api/mfa` vs `/api/mfa-admin` uses):

- `GET /api/session-admin/user/{userId:[0-9]+}` (`listForUser`) — someone else's sessions;
  `currentSessionId` is passed empty since none of the target's sessions is the caller's.
- `POST /api/session-admin/user/{userId:[0-9]+}/revoke` (`revokeAllForUser`) — ends all of
  theirs.

## Notes

- `callerClaims(r)` reads the requesting session's JWT claims, which carry both the user id
  and the session id — the latter is how "this is the session you are using right now" is
  determined without trusting anything the client sent. Shared with `apis/stepup.go.md`.
- Every revocation writes an audit entry (`services.ActionSessionRevoke`/
  `ActionSessionRevokeAll`) via the api's own `record` helper (mirrors, but is distinct
  from, `auditRecorder` in `apis/audit.go.md` — this type stores `trustedProxies` directly
  rather than embedding `auditRecorder`, since it also needs `access` for the admin
  subrouter).
- `trustedProxies` must resolve the same way the login API's does, or the same operator
  could be recorded under two different addresses depending on which action they took —
  both are parsed from the shared `deps.Config.RateLimit.TrustedProxies` via
  `middlewares.ParseTrustedProxies`.
- Mounted from `apps/myidsan/app/app.go`'s `RegisterAppRoutes` (`app/app.go.md`).
