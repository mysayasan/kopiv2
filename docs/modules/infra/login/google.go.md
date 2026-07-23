# Module: infra/login/google.go

## Purpose

Implements Google OAuth login as a `login.RedirectProvider` (see `provider.go.md`).

## Responsibilities

- `Key()` returns `"google"`; `DisplayName()` returns `"Google"`.
- Redirect login requests to Google's OAuth consent URL with per-request state.
- Validate callback state from the HTTP-only state cookie.
- Exchange OAuth code using the request context.
- Fetch Google user profile using a bearer token request.
- `Callback` normalizes the Google userinfo response into a `login.Identity`
  (`Subject` = the userinfo `id`; `EmailVerified` carries Google's own
  `verified_email` flag). A response with no subject id is rejected as an error
  rather than producing an unmatchable identity.

## Notes

- User info responses are checked for non-2xx status and response bodies are closed.
- `NewGoogleLogin` no longer takes an `AuthMidware` — session issuance moved out of the
  provider and into `apps/myidsan/apis/login.go`'s `setOAuthSession`, which the provider
  has no reference to.
