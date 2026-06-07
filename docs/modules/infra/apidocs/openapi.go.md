# Module: infra/apidocs/openapi.go

## Purpose

Provides a shared runtime OpenAPI/Swagger implementation for all app modules.

## Responsibilities

- Exposes Swagger UI at `/swagger`.
- Exposes generated OpenAPI JSON at `/swagger/openapi.json`.
- Walks Gorilla Mux routes to auto-discover endpoint paths and methods.
- Converts discovered routes into OpenAPI 3.0 path operations.
- Adds reusable request/response schema components for key endpoints under `components.schemas`.
- Documents top-level `durationMs` on default, paging, and error JSON response wrappers.
- Maps key endpoints to endpoint-specific response wrapper schemas (typed `Default*Response` / `Paging*Response`).
- Models non-JSON routes with explicit status/content contracts (e.g. OAuth redirect status codes and MJPEG streaming media type).
- Includes cache-service admin endpoint contracts (`GET/DELETE /api/cache-service`, `POST /api/cache-service/wipe`, `GET /api/cache-service/health`).
- Includes API log endpoint contracts (`GET /api/log`, `DELETE /api/log`).
- Includes runtime log endpoint contracts (`GET /api/log-service`, `DELETE /api/log-service`).
- Includes runtime version endpoint contract (`GET /api/version`) without cookie auth.
- Marks protected `/api/*` routes with cookie session auth security requirements.
- Adds `X-CSRF-Token` header parameters for unsafe protected methods.
- Adds path parameters to OpenAPI operation parameters.
- Supports app-provided metadata and endpoint descriptions through `apidocs.Provider`.

## Notes

- Route discovery happens from runtime registration, so shared APIs and app-specific APIs are documented together.
- Request bodies are attached for key write endpoints (for example user group, endpoint RBAC, camera stream, file upload).
- Request bodies include local auth endpoints: `POST /api/login/default` and `POST /api/login/default/register`.
- App modules can improve endpoint summaries/descriptions by implementing `APIDocs()`.
- The Swagger UI is loaded from CDN assets and reads the local `/swagger/openapi.json` document.
