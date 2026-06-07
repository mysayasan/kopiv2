# Module: domain/shared/services/cache_service.go

## Purpose

Provides shared cache management business operations consumed by the cache admin API.

## Responsibilities

- List cache keys from active cache provider.
- Wipe cache by key.
- Wipe cache by prefix.
- Check cache health status via provider ping.
