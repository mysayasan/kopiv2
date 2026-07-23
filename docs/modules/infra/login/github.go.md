# Module: infra/login/github.go

## Purpose

Implements GitHub OAuth login as a `login.RedirectProvider` (see `provider.go.md`).

## Responsibilities

- `Key()` returns `"github"`; `DisplayName()` returns `"GitHub"`.
- Redirect login requests to GitHub's OAuth consent URL with per-request state.
- Validate callback state from the HTTP-only state cookie.
- Exchange OAuth code using the request context.
- Fetch GitHub user profile using a bearer token request.
- `Callback` normalizes the GitHub profile into a `login.Identity`: `Subject` is the
  account's numeric GitHub id (`strconv.FormatInt`), `Name`/`GivenName` fall back to the
  `login` handle when the profile has no display name set, `EmailVerified` is always
  true (the `/user` endpoint only returns a verified public email), and a response with
  no numeric id is rejected as an error rather than producing an unmatchable identity.

## Notes

- GitHub login requires a public email in the `/user` profile response; the caller
  (`apps/myidsan/apis/login.go`'s `providerCallback`) rejects a callback whose identity
  has no email before it reaches `UpsertFederated`.
- User info responses are checked for non-2xx status and response bodies are closed.
- `NewGithubLogin` no longer takes an `AuthMidware` — session issuance moved out of the
  provider and into `apps/myidsan/apis/login.go`'s `setOAuthSession`, which the provider
  has no reference to.
