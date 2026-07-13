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
  `ClientId` and `ClientSecret` set.
