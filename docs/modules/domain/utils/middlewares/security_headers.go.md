# Module: domain/utils/middlewares/security_headers.go

## Purpose

Sets a fixed set of hardening response headers on every request, and strips the
`Server` header so the server software/version is never advertised. Replaces
the old `greet` middleware (`domain/utils/middlewares/greet.go`, removed),
whose only job was setting `Server: r450k`.

## Responsibilities

- `NewSecurityHeaders(cfg SecurityConfig)` resolves hardened defaults once; the
  per-request handler (`Middleware`) only copies precomputed strings.
- Sets `X-Content-Type-Options: nosniff` unless `ContentTypeOptions` disables it.
- Sets `X-Frame-Options` — default `SAMEORIGIN`; a pointer to `""` omits it
  (e.g. when CSP `frame-ancestors` is authoritative).
- Sets `Referrer-Policy` — default `strict-origin-when-cross-origin`; a
  pointer to `""` omits it.
- Sets `Strict-Transport-Security` only over TLS connections (`r.TLS != nil`);
  default `max-age=31536000; includeSubDomains`, disableable via
  `HSTSEnabled`, tunable via `HSTSMaxAgeSeconds`, `HSTSIncludeSubDomains`,
  `HSTSPreload`.
- Sets `Content-Security-Policy` only when `ContentSecurityPolicy` is
  non-empty — CSP is opt-in per app because a wrong policy silently breaks a
  SPA, so each app enables it only after its own front-end is verified.
- Sets the `Server` header when `ServerHeader` is non-empty; otherwise strips
  any `Server` header a downstream handler might add.
- Runs first in the middleware chain (`infra/apphost/run.go`) so headers are
  present on every response, including auth 401s, rate-limit 429s, static
  assets, and the setup page.

## Notes

- The zero-value `SecurityConfig{}` is safe: every nil pointer falls back to a
  hardened default, so an app that supplies no `securityHeaders` config block
  still gets baseline protection.
- `infra/apphost/run.go`'s `securityHeadersConfig` helper maps
  `AppConfigModel.SecurityHeaders` (see `infra/config/config_models.go.md`)
  onto `SecurityConfig`.
- `AppConfigModel.SecurityHeaders.Disabled` skips registering the middleware
  entirely (escape hatch, not recommended).
- Shared infra: wired once in `infra/apphost/run.go` for all apps
  (`mymatasan`, `myseliasan`, `myidsan`); CSP remains opt-in per app config.
- Covered by `security_headers_test.go`: default headers, HSTS only sent over
  TLS, CSP emitted only when configured, and override behavior (custom
  frame options, disabled nosniff/HSTS, custom Server brand, custom HSTS
  max-age/subdomains/preload).
