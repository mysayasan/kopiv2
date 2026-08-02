# Module: apps/myidsan/apis/webauthn.go

## Purpose

The self-service and admin security-key management surface — the WebAuthn counterpart of
`apis/mfa.go.md`. A signed-in user manages their **own** keys (list, register, rename,
remove); a superadmin can clear **another** user's keys for the lost-device case. The
pre-session login ceremony itself (fetch a challenge, present the signed assertion) lives
on `apis/login.go.md` instead — it runs before any session exists, so it cannot live behind
`auth.Middleware` the way this file's routes do.

## Routes

Self-service, `auth.Middleware`-protected, no RBAC matrix (mirrors `apis/mfa.go.md` — every
route acts on the caller's own account, identified from the JWT claims):

- `GET /api/mfa/webauthn` (`list`) — returns `{enabled, keys: [WebAuthnCredentialView]}`.
  `enabled` travels with the list so the UI can explain an empty state ("switched off on
  this server") instead of offering an Add button that will fail.
- `POST /api/mfa/webauthn/register/begin` (`beginRegister`) — issues a creation challenge
  via `IWebAuthnService.BeginEnroll`, keyed by `enrollStateKey(claims.Id)`.
- `POST /api/mfa/webauthn/register/finish` (`finishRegister`) — body
  `{label, credential}`; verifies the attestation and persists the credential
  (`IWebAuthnService.FinishEnroll`). Records `services.ActionWebAuthnEnroll` — a new second
  factor on an account is exactly what an attacker holding a stolen session would add to
  keep access, so the owner must be able to see it happened.
- `PUT /api/mfa/webauthn/{id}` (`rename`) — body `{label}`; retitles one of the caller's own
  keys. Records `services.ActionWebAuthnRename`.
- `DELETE /api/mfa/webauthn/{id}` (`remove`) — body `{password, code}`; removes one of the
  caller's own keys. **Re-proves identity first** (`reproveIdentity`), for the same reason
  disabling TOTP does: a hijacked session must not be able to strip the factor that would
  have stopped it. Accepts either the account password (via
  `IUserLoginService.AuthenticateDefault`) or a valid TOTP code, when one is enrolled; a
  third-party-only account with no local password has nothing to prove and is let through
  (`services.ErrThirdPartyOnlyAccount` tolerated). Records `services.ActionWebAuthnRemove`.

Admin, `auth.Middleware` + `access.Middleware` + `access.RequireSuperadmin`:

- `DELETE /api/mfa-admin/{id}/webauthn` (`adminResetAll`) — clears **every** security key on
  another user's account (`IWebAuthnService.DeleteAllForUser`), the lost-device recovery
  path for a non-superadmin user. **Gated by `requireStepUp`** (`apis/stepup.go.md`), the
  same tier as clearing a TOTP factor at `DELETE /api/mfa-admin/{id}` — removing someone
  else's second factor is exactly what an attacker with a stolen cookie would do to keep an
  account after being locked out of it otherwise. Records
  `services.ActionWebAuthnAdminReset`.

## Notes

- `maxWebAuthnBody = 128 << 10` caps a ceremony response body — an attestation object with
  a long certificate chain is a few KiB; 128 KiB is far beyond any legitimate response.
- `writeErr` maps `IWebAuthnService` errors onto status codes and deliberately does **not**
  always echo the library's own message verbatim — precise in a log, needlessly
  instructive in a response.
- `webAuthnApi` embeds `auditRecorder` (`apis/audit.go.md`), same pattern as `mfaApi`.
- Mounted in `apps/myidsan/app/app.go`'s `RegisterAppRoutes` via
  `apis.NewWebAuthnApi(api, *deps.Auth, deps.Access, webauthnService, userLoginService,
  mfaService, auditService, stepUpService, deps.Config.RateLimit.TrustedProxies)` — see
  `apps/myidsan/app/app.go.md`.
- Frontend: the Profile page's "Security keys" card
  (`views/react-webpack/src/views/components/webauthn_keys.js`), driven by
  `views/react-webpack/src/lib/webauthn.js`'s ceremony helpers (unpadded base64url ↔
  `ArrayBuffer` conversions, `DOMException` → actionable message mapping).
