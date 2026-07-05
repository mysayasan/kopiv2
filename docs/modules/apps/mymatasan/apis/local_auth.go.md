# Module: apps/mymatasan/apis/local_auth.go

## Purpose

Provides standalone local Basic Auth middleware for `mymatasan` app routes.

## Responsibilities

- Validate incoming HTTP Basic Auth credentials through `ILocalUserService`.
- Set a short-lived HTTP-only cookie so browser MJPEG image streams can authenticate without custom headers.
- Revalidate auth cookies against the local user database so password resets and inactive users take effect.
- Attach the authenticated local user to request context for admin-only Settings routes.
- Fail closed with `401 Unauthorized` when credentials are missing, wrong, inactive, or not configured.
- Throttle failed sign-ins via `LoginGuard` (per-IP, escalating lockout → `429`), but **only for the interactive login probe** `GET /auth/session` (`isLoginAttemptPath`). A wrong Basic credential on any other protected route (background polls, the notification SSE reconnect, media tiles, page-load data fetches — all of which the SPA replays its stored credential on) is denied `401` but does **not** count toward or trip the lockout.

## Notes

- This middleware is app-local and does not use MyIDSan JWT sessions or RBAC.
- It is intended as a temporary first security layer until the strict `myseliasan` to `mymatasan` device protocol is defined.
- Scoping the lockout to the login probe fixes a self-lockout: when a client's stored credential goes stale (a reinstall/factory-reset reseeds the admin password, or a password change in another tab), one page load fires a burst of parallel protected requests that previously drained the whole attempt budget and locked a legitimate user out before they typed anything. Volumetric abuse of other endpoints is bounded separately by the global rate limiter.
