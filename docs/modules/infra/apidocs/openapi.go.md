# Module: infra/apidocs/openapi.go

## Purpose

Provides a shared runtime OpenAPI/Swagger implementation for all app modules.

## Responsibilities

- Exposes Swagger UI at `/swagger`.
- Serves the Swagger UI assets themselves from `GET /swagger/assets/{file}` — `swagger-ui-bundle.js` and `swagger-ui.css` are read from the vendored `//go:embed swaggerui/*` bundle (`swagger-ui-dist@5.32.11`, see `swaggerui_test.go.md`); `swagger-init.js` is served from an in-binary constant (`swaggerInitJS`) rather than embedded, since it is the small `SwaggerUIBundle({...})` call, not a vendored library file. All three get `Cache-Control: public, max-age=86400` since the asset path is build-pinned, not query-string-versioned.
- Exposes generated OpenAPI JSON at `/swagger/openapi.json`.
- Walks Gorilla Mux routes to auto-discover endpoint paths and methods.
- Converts discovered routes into OpenAPI 3.0 path operations.
- Adds reusable request/response schema components for key endpoints under `components.schemas`.
- Documents top-level `durationMs` on default, paging, and error JSON response wrappers.
- Documents paging responses with offset-window metadata: `limit`, `offset`, `resCnt`, `totalCnt`, `hasNext`, and `nextOffset`.
- Maps key endpoints to endpoint-specific response wrapper schemas (typed `Default*Response` / `Paging*Response`).
- Maps shared write endpoints to `*InputDto` request schemas and shared read responses to `*OutputDto` result schemas.
- Models non-JSON routes with explicit status/content contracts (e.g. OAuth redirect status codes and binary download media type).
- Includes cache-service admin endpoint contracts (`GET/DELETE /api/cache-service`, `POST /api/cache-service/wipe`, `GET /api/cache-service/health`).
- Includes app-registry endpoint contracts (`GET/POST/PUT/DELETE /api/app-registry`).
- Includes myidsan SSO fallback endpoint contracts (`POST /api/sso/introspect`, `POST /api/sso/authorize`).
- Includes API log endpoint contracts (`GET /api/log`, `DELETE /api/log`).
- Includes runtime log endpoint contracts (`GET /api/log-service`, `DELETE /api/log-service`).
- Includes runtime version endpoint contract (`GET /api/version`) without cookie auth.
- Includes file-storage upload contracts for synchronous upload, async upload, ID-based download, inline view, job status, security level, and expiry fields.
- Marks protected `/api/*` routes with cookie session auth security requirements.
- Adds `X-CSRF-Token` header parameters for unsafe protected methods.
- Adds path parameters to OpenAPI operation parameters.
- Adds `limit`, `offset`, `filters`, and `sorters` query parameters for DB-backed shared paging endpoints.
- Documents `429` responses for API routes affected by rate limiting.
- Supports app-provided metadata and endpoint descriptions through `apidocs.Provider`.

## Notes

- Route discovery happens from runtime registration, so shared APIs and app-specific APIs are documented together.
- Request bodies are attached for key write endpoints (for example user group, endpoint RBAC, camera stream, file upload).
- Request bodies include local auth endpoints: `POST /api/login/default` and `POST /api/login/default/register`.
- File-storage download documents `id` for a single binary file, `ids` for ZIP download, and `view` for inline browser rendering.
- File-storage download is documented without cookie auth so public downloads work from Swagger.
- SSO fallback endpoints are documented without cookie auth because they require the internal service token header instead.
- File upload multipart schema includes `documents`, `securityLvl`, `expiredAt`, `expiresIn`, and `expiresInUnit`.
- Operation job responses use `OperationJobOutputDto` and `DefaultOperationJobResponse`.
- Legacy `*Payload` component names remain as aliases for now while shared endpoints reference DTO-named components.
- App modules can improve endpoint summaries/descriptions by implementing `APIDocs()`.
- The Swagger UI is now fully self-hosted: it previously loaded `swagger-ui.css`/`swagger-ui-bundle.js` from `cdn.jsdelivr.net` and had its init call inlined in a `<script>` tag, which broke `/swagger` twice over — an air-gapped or egress-filtered install (e.g. `myseliasan`) rendered a blank page, and the inline `<script>` violated the apps' own shipped `script-src 'self'` Content-Security-Policy even when the CDN was reachable. The page still reads the local `/swagger/openapi.json` document; only the UI library and its bootstrap script moved on-box. The page's small inline `<style>` block is unaffected — the shipped CSP allows `style-src 'unsafe-inline'`.
