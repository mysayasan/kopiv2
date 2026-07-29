# Module: infra/apidocs/swaggerui_test.go

## Purpose

Validates the self-hosted Swagger UI asset serving added to `openapi.go` — proves
`/swagger` never reaches off-box and never violates the apps' own
`script-src 'self'` Content-Security-Policy.

## Coverage

- `TestSwaggerPageReferencesNoExternalHost` — the rendered `/swagger` page body must not
  contain `cdn.jsdelivr.net`, `unpkg.com`, `//cdn.`, `http://`, or `https://`; a CDN
  reference here breaks air-gapped installs and trips the CSP either way.
- `TestSwaggerPageHasNoInlineScript` — every `<script>` tag in the page must carry a
  `src=` attribute and no inline body; `script-src 'self'` forbids inline `<script>` as
  firmly as it forbids a CDN, and a regression here is invisible locally (no CSP in dev)
  and only shows up on a customer install.
- `TestSwaggerAssetsAreServedFromTheBinary` — `GET /swagger/assets/swagger-ui-bundle.js`,
  `/swagger-ui.css`, and `/swagger-init.js` each return `200` with the expected
  content-type and a non-empty body (`swagger-init.js`'s body must contain
  `SwaggerUIBundle`) — catches the embed pattern silently matching nothing.
- `TestSwaggerPageAssetReferencesResolve` — every `href="/swagger/..."`/`src="/swagger/..."`
  the page references actually resolves to `200`; a typo'd path would otherwise degrade
  to a blank docs page rather than an error anyone notices.
- `TestSwaggerUnknownAssetIs404` — an unknown asset name is `404`, and a path-traversal
  attempt (`/swagger/assets/../openapi.go`) is not served from outside the embedded set.

## Notes

- See `docs/modules/infra/apidocs/openapi.go.md` for the vendored asset serving these
  tests exercise.
