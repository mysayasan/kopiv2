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

## Notes

- `Delete(key)` also clears that key's sliding-window state from the separate `rateWindows`
  map, not just the value in `cache`. On Redis the window IS the key, so `Delete` already meant
  "forget everything about this key" there; on `MemoryStore` the two lived in different maps,
  so `Delete` used to mean something different depending on the provider — a primitive that
  means two different things on two providers is a bug waiting for whichever one is not under
  test. Found because `domain/shared/apis.LoginGuard`'s shared-store half
  (`login_guard_shared.go.md`) clears a key exactly this way on a successful sign-in, and on
  `MemoryStore` the rate-limit history used to survive it.
