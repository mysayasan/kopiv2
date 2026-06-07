# Module: infra/login/google.go

## Purpose

Implements Google OAuth login and callback user lookup.

## Responsibilities

- Redirect login requests to Google's OAuth consent URL with per-request state.
- Validate callback state from the HTTP-only state cookie.
- Exchange OAuth code using the request context.
- Fetch Google user profile using a bearer token request.
- Decode Google profile data for login API session issuance.

## Notes

- User info responses are checked for non-2xx status and response bodies are closed.
