# Module: infra/cache/memory.go

## Purpose

Implements process-local cache adapter using go-cache.

## Responsibilities

- Stores values as JSON bytes for backend parity with Redis adapter.
- Supports key reads/writes with TTL.
- Supports prefix invalidation by scanning local keys.
- Supports key listing with prefix filter and pagination.
- Supports process-local sliding-window rate-limit counters for local development and single-instance use.
- Serves as local fallback provider for development/testing.
