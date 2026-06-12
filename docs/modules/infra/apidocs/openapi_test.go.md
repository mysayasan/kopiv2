# Module: infra/apidocs/openapi_test.go

## Purpose

Validates OpenAPI document generation from runtime routes and app-provided endpoint descriptions.

## Coverage

- Confirms generated document uses OpenAPI 3.0.3.
- Confirms route discovery includes registered API endpoints.
- Confirms provider-supplied summary/description are applied.
- Confirms static catch-all route is excluded from docs output.
