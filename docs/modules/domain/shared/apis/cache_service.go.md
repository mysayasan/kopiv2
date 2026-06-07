# Module: domain/shared/apis/cache_service.go

## Purpose

Exposes protected shared cache administration endpoints for operational control.

## Responsibilities

- List cache keys with optional prefix and pagination.
- Expose cache provider health check for admin usage.
- Wipe cache by key or prefix.
- Require explicit confirmation (`wipeAll=true`) to wipe all entries.
- Write successful wipe actions to API logs for admin audit trail.

## Routes

- `GET /api/cache-service`
- `GET /api/cache-service/health`
- `DELETE /api/cache-service`
- `POST /api/cache-service/wipe`
