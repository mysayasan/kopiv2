# Module: domain/utils/middlewares/auth.go

## Purpose

Cookie-backed JWT authentication middleware and session cookie helper.

## Behavior

- Reads the JWT from the HttpOnly auth cookie.
- Uses `__Host-kopiv2_access` / `__Host-kopiv2_csrf` on secure requests and `kopiv2_access` / `kopiv2_csrf` for local non-TLS development.
- Parses and validates JWT using HMAC secret.
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

## Utility

- `JwtToken(claims)` generates signed JWT for login/session issuance.
- `IssueAuthCookies(w, r, claims)` writes the auth and CSRF cookies.
- `ClearAuthCookies(w, r)` expires both secure and local-development cookie names.
