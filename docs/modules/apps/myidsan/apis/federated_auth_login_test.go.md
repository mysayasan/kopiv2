# Module: apps/myidsan/apis/federated_auth_login_test.go

## Purpose

Validates the rendered federated login page (`renderLoginPage`) served by
`federated_auth.go`.

## Coverage

- `TestLoginPageHasNoExternalReferences` — the rendered page (no providers configured)
  references no external host (`googleapis.com`, `gstatic.com`, `fonts.google`, CDN
  hosts) and every asset it loads (`/assets/fonts.css`, `/assets/favicon.svg`) is a
  same-origin rooted path, not an absolute URL. This is a hard requirement: myidsan and
  the apps that redirect to this page are deployed on an air-gapped intranet.
- `TestSocialButtonsRequireCredentials` — table-driven over `nil`, empty-but-non-nil,
  client-id-only, and fully-configured `OAuthProvidersConfigModel` cases; asserts the
  Google/GitHub buttons (`href="/api/login/..."`) render only when a provider has both
  `ClientId` and `ClientSecret` set. `renderFor` builds the test `federatedAuthApi` with
  `providers: login.BuildRegistry(providers)`, so this test exercises the actual registry
  construction path (`infra/login/provider.go.md`), not a hand-rolled substitute.
- `TestLoginPageDirectoryOption` — directory login enabled (`method="ldap"`, a non-empty
  label) renders the account-type `<select name="method">` with that label AND preserves
  a failed `ldap` attempt's selection (`value="ldap" selected`); directory login disabled
  (empty label) renders no `name="method"` element at all — a disabled directory must
  never offer a dead account-type choice.
