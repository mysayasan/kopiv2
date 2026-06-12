# Module: domain/models/jwt_custom_claims_model.go

## Purpose

Shared JWT claims model used by auth, RBAC, API logging, and SSO fallback APIs.

## Notes

- Embeds standard JWT registered claims for `iss`, `aud`, `exp`, `iat`, and `jti`.
- Adds application claims for user id, email, display name, role id, session id (`sid`), app code, and policy version.
- `sid` links a token to the cache-backed SSO session entry.
