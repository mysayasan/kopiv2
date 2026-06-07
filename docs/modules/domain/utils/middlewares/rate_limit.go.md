# Module: domain/utils/middlewares/rate_limit.go

## Purpose

Applies config-driven sliding-window rate limits to `/api` requests by API access tier.

## Strategy

1. Load active `api_endpoint` rows from cache, with read-through fallback to the endpoint service.
2. Match request host/path using the same wildcard-host and segment-boundary behavior as RBAC.
3. Use the longest matching endpoint path so specific public routes can override broader protected bases.
4. Select tier limits from config (`DevOnly`, `AuthOnly`, `Public`).
5. Use the shared cache provider's atomic sliding-window operation to allow or reject the request.

## Notes

- Rate limiting runs after API activity logging so `429` responses are still persisted in `api_log`.
- `DevOnly` does not bypass authorization. Dev-only routes still require auth/RBAC when mounted behind protected handlers.
- Redis should be used in multi-instance production so counters are shared across app instances.
