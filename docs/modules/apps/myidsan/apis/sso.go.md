# Module: apps/myidsan/apis/sso.go

## Purpose

Internal SSO fallback API for relying apps that cannot share a Redis-backed session cache.

## Routes

- `POST /api/sso/introspect`: validates token, issuer/audience, and cache-backed session. Returns an `introspectResponse` with `active`, `userId`, `roleId`, `email`, `name`, `sessionId`, `issuer`, `audience`, `appCode`, `policyVersion`, and `expiresAt`.

## Security

- Requires `X-Myidsan-Internal-Token` header or `Authorization: Bearer <token>` matching `sso.internalToken` / `SSO_INTERNAL_TOKEN`.
- `authorizeInternal` compares both header forms with `subtle.ConstantTimeCompare` (`constantTimeMatch`) rather than `==`: a plain string comparison on a secret leaks its length and a prefix of its content through timing, and this endpoint answers "is this token valid" for any relying app that cannot share the session cache. `sso.internalToken` itself is also refused at startup if it is a known placeholder value — see `infra/apphost/run.go.md`'s `isPlaceholderSecret`.

## Removed (accessrbac migration)

- `POST /api/sso/authorize` is removed. That endpoint returned app-scoped RBAC decisions using the legacy `RbacMidware`; relying apps now enforce authorization locally with `AccessSessionMidware` and the shared accessrbac matrix.

## Notes

- These routes intentionally do not use browser cookie auth or CSRF middleware.
- Browser redirect login for relying apps is handled separately by `apps/myidsan/apis/federated_auth.go`.
