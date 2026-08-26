# Module: domain/utils/middlewares/auth.go

## Purpose

Cookie-backed JWT authentication middleware and session cookie helper.

## Behavior

- Reads the JWT from the HttpOnly auth cookie.
- Uses `__Host-kopiv2_access` / `__Host-kopiv2_csrf` on secure requests and `kopiv2_access` / `kopiv2_csrf` for local non-TLS development.
- Parses and validates JWT using HMAC secret.
- When configured, validates issuer and one of the accepted audiences.
- Issues tokens with `iss`, multi-value `aud`, `exp`, `iat`, `jti`, `sid`, `appCode`, and `policyVersion`.
- Stores issued sessions in the configured cache under `sso:session:<sid>` and validates that cache entry when a token contains `sid`.
- Requires `X-CSRF-Token` to match the readable CSRF cookie for unsafe methods (`POST`, `PUT`, `PATCH`, `DELETE`).
- Maps claims into `models.JwtCustomClaims`.
- Injects claims into request context (`enumauth.Claims`).

## Failure Responses

Returns permission errors when:

- token missing
- token signature/method invalid
- token invalid
- CSRF token missing or mismatched on unsafe methods
- required claim (`Email`) empty

## Session Role Validation

The `validateSession` function no longer checks whether the session-stored `RoleId` matches `claims.RoleId`. Role is now a dynamic property resolved live from the user store on each request by `AccessSessionMidware`; invalidating a session because the role changed would bounce users to the login screen every time an admin updated their role. An otherwise-valid session (matching `UserId`, `Email`, and non-expired `sid`) remains valid regardless of role changes.

## Utility

- `JwtToken(claims)` generates signed JWT for login/session issuance.
- `ClaimsFromToken(ctx, rawToken)` validates a raw bearer/cookie token for service-to-service introspection.
- `IssueAuthCookies(w, r, claims)` writes the auth and CSRF cookies.
- `ClearAuthCookies(w, r)` expires both secure and local-development cookie names.

## Relying-app revocation (`SetRevocationChecker`)

`validateSession` answers entirely from THIS app's session cache, which is exactly the problem
when this app is a relying party: the identity server can end a session and this cache never hears
about it. An optional `RevocationChecker` (`revocation.go.md`) re-asks on a TTL as the last step of
`validateSession`, and is attached after construction with `SetRevocationChecker` because apphost
builds this middleware for every app before any app wires its own services. An app that wires none
is bit-for-bit unchanged.
