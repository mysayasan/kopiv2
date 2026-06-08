# Module: apps/myidsan/apis/sso.go

## Purpose

Internal SSO fallback APIs for relying apps that cannot share Redis-backed session or policy cache.

## Routes

- `POST /api/sso/introspect`: validates token, issuer/audience, and cache-backed session.
- `POST /api/sso/authorize`: validates token/session, then returns an app-scoped RBAC decision.

## Security

- Requires `X-Myidsan-Internal-Token` or `Authorization: Bearer <token>`.
- The expected token comes from `sso.internalToken` or `SSO_INTERNAL_TOKEN`.

## Notes

- These routes intentionally do not use browser cookie auth or CSRF middleware.
- Authorization reuses `RbacMidware.AuthorizeClaimsForApp` so fallback decisions match normal protected-route decisions.
- Browser redirect login for relying apps is handled separately by `apps/myidsan/apis/federated_auth.go`.
