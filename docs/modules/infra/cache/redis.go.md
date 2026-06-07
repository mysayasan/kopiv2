# Module: infra/cache/redis.go

## Purpose

Implements Redis-backed shared cache adapter for multi-instance deployments.

## Responsibilities

- Connects to Redis with configurable address/password/DB/TLS/timeouts.
- Serializes values as JSON payloads for typed cache reads.
- Supports prefix invalidation using Redis SCAN + DEL.
- Supports key listing using Redis SCAN with prefix filter and pagination.
- Supports atomic sliding-window rate-limit decisions using Redis sorted sets.
- Exposes ping and close lifecycle hooks for readiness and shutdown.

## Notes

- Key prefix is applied centrally by the adapter to avoid cross-app key collisions.
- Operation timeout is enforced per cache call to avoid hanging request paths.
