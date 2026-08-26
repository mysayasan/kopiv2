# Module: apps/myseliasan/apis/auth.go

## Purpose

Implements MySeliaSan as a relying app for MyIDSan authorization-code login.

## Routes

- `GET /api/auth/start`: creates a local state nonce and redirects to MyIDSan `/api/auth/authorize`.
- `GET /api/auth/callback`: validates state, exchanges code at MyIDSan `/api/auth/token`, and issues the MySeliaSan session cookie.
- `POST /api/auth/logout`: clears the MySeliaSan session cookie.
- `POST /api/auth/local-login` (stock superadmin): authenticates local credentials and issues a session cookie. The issued JWT carries the `Email` claim (falls back to `username` when the account has no real email) so the shared auth middleware's non-empty email check is satisfied. Previously, logging in with the stock superadmin returned HTTP 200 but every subsequent request to `/api/session/me` returned 401 because the email claim was empty.
- `POST /api/auth/change-password`: rotates the signed-in local account's own password. Reachable while a user is flagged must-change (the control-session middleware is not mounted on `/auth`).
- `GET /api/auth/config` (public, no auth): returns `{"ssoEnabled": bool}` — `true` when `sso.providerBaseUrl` is non-empty. Lets the login screen hide the "Continue with myidsan" button on a standalone install where federated sign-in cannot work (the packaged/shipped config leaves `sso.providerBaseUrl` empty by default).

## Failed-login lockout

Both credential surfaces — `POST /api/auth/local-login` and `POST /api/auth/change-password` — go through the shared `sharedapis.LoginGuard`. Before this they went through nothing at all: `local-login` called `AuthenticateLocal` and returned, and a live two-instance cluster bench (`tools/fleetbench/bench_myseliasan_lockout.py`) served **13 consecutive password guesses** at ~61ms each and then accepted the correct password as if nothing had happened. `change-password` served **11 current-password guesses** to a plain session cookie, which is a password oracle for a stolen one.

- **Keys**: `ip:<RemoteAddr host>` and `user:<lowercased identifier>` (`sharedapis.LoginGuardKeys`). The identifier is the submitted username on login and the session's own email on change-password, so a hijacked cookie cannot aim the account counter at somebody else.
- **Order**: the guard is consulted **before** the credential, so a locked caller costs no bcrypt work and cannot clear the lockout by supplying the right password. The consequence is that the attempt which *crosses* the threshold still receives its credential verdict; the lockout applies from the next request.
- **Config**: the shared `loginSecurity` block. It is absent from this app's `config.json`, and an absent block resolves to **enabled** with suite defaults — so the configuration had been promising this lockout since before the code existed.
- **Clustering**: `WithSharedStore(deps.Cache, "myseliasan")` is attached in `app.go` when `cache.provider` is `redis`/`redis-cluster`, so the lockout spans the deployment. The in-memory guard remains the fallback half, so an unreachable cache degrades to per-process throttling rather than to none.
- **Not counted**: a rejected *new* password on change-password (too short, unchanged). That is a policy refusal by a caller already holding a valid session, and counting it would let a user lock themselves out choosing a password the policy keeps refusing.
- **Refusal shape**: `429` with `Retry-After` and `retryAfterSeconds` in the body (`sharedapis.WriteLockoutJSON`). The body field is what distinguishes this refusal from the generic rate limiter's, which answers the same status on the same paths.

## Audit trail

The trail recorded node adoptions, policy edits and key rotations, and had never recorded a single authentication event — so a complete brute-force run left nothing in the product's own record. This file now writes:

- `login.success` — actor id/email/role, target the user id, `method: local`.
- `login.failure` — outcome `denied`, actor email and target set to the **attempted** identifier (which may name no real account; that is the point of recording it). Written for a disabled account too, since a refused credential is still a refused credential.
- `login.lockout` — written **once**, on the attempt that engages the lockout, with `retryAfterSeconds` in metadata. Writing it on every subsequent refusal would bury the event worth alerting on.
- `password.change` — a successful self-service rotation.

**The attempted password is never recorded on any of them.** `ClientIp` is the **peer** address, not the `X-Forwarded-For` this app's other audit entries use: on a login entry the address is the evidence, a header the caller writes is evidence of nothing, and a forged one would make the trail disagree with the lockout about who was attacking. A proxy's claim is kept in `Metadata` as `claimedForwardedFor`, labelled as claimed rather than observed.

Both the guard and the audit service are **nil-safe**: an app or test that wires neither behaves exactly as this file did before.

## Security

- State is random, cache-backed, and short-lived.
- Callback rejects invalid state before token exchange.
- Token exchange is server-to-server and uses the relying app client secret.
- Token exchange uses the default OS trust store unless `sso.caCertPath` is configured; then MySeliaSan adds that PEM CA/certificate bundle to the HTTPS client trust roots.
- `sso.caCertPath` does not disable TLS verification. Hostname, expiry, and certificate-chain checks still apply.
- `sso.redirectBaseUrl` makes the callback URL stable even when users open the app through another local host alias or proxy host.
- Local session cookies are HttpOnly and issued by shared auth middleware.

## User Provisioning

On successful token exchange, the callback calls `IControlUserService.UpsertFederated(ssoUserId, email, name)` to provision or refresh a `ControlUser` row (kind=`"federated"`). New federated users are assigned the `viewer` role by default. Disabled federated accounts are rejected before the session cookie is issued. The myseliasan session JWT carries the federated user's `ControlUser.Id` and `RoleId`, not the myidsan user ID directly.
