# Module: infra/login/github.go

## Purpose

Implements GitHub OAuth login and callback user lookup.

## Responsibilities

- Redirect login requests to GitHub's OAuth consent URL with per-request state.
- Validate callback state from the HTTP-only state cookie.
- Exchange OAuth code using the request context.
- Fetch GitHub user profile using a bearer token request.
- Decode GitHub profile data for login API session issuance.

## Notes

- GitHub login requires a public email in the `/user` profile response for local account mapping.
- User info responses are checked for non-2xx status and response bodies are closed.
