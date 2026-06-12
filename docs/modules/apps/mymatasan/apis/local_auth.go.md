# Module: apps/mymatasan/apis/local_auth.go

## Purpose

Provides standalone local Basic Auth middleware for `mymatasan` app routes.

## Responsibilities

- Validate incoming HTTP Basic Auth credentials through `ILocalUserService`.
- Set a short-lived HTTP-only cookie so browser MJPEG image streams can authenticate without custom headers.
- Revalidate auth cookies against the local user database so password resets and inactive users take effect.
- Attach the authenticated local user to request context for admin-only Settings routes.
- Fail closed with `401 Unauthorized` when credentials are missing, wrong, inactive, or not configured.

## Notes

- This middleware is app-local and does not use MyIDSan JWT sessions or RBAC.
- It is intended as a temporary first security layer until the strict `myseliasan` to `mymatasan` device protocol is defined.
