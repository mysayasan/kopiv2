# Module: infra/apidocs/openapi_test.go

## Purpose

Validates OpenAPI document generation from runtime routes and app-provided endpoint descriptions.

## Coverage

- Confirms generated document uses OpenAPI 3.0.3.
- Confirms route discovery includes registered API endpoints.
- Confirms provider-supplied summary/description are applied.
- Confirms static catch-all route is excluded from docs output.
- Confirms a mux path parameter's regex constraint (`{id:[0-9]+}`) is stripped from the emitted OpenAPI path to bare `{id}`, and that the operation still declares a matching `path` parameter named `id` (`TestBuildSpecStripsMuxRegexFromPathParameters`).
