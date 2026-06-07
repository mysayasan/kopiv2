# Module: infra/cache/store.go

## Purpose

Defines the cache abstraction used by middleware/services without binding domain code to a specific backend.

## Responsibilities

- Defines read/write/delete contract for cache operations.
- Provides prefix invalidation contract for grouped cache keys.
- Provides key listing contract with prefix and pagination support for admin APIs.
- Provides an atomic sliding-window rate-limit contract used by API rate limiting.
- Provides ping/close lifecycle methods for runtime health and shutdown.
