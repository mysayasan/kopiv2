# Module: apps/mymatasan/services/ifaces.go

## Purpose

Declares service contracts for app-specific domain.

## Interfaces

- `IHomeService`
  - `GetLatest(ctx, limit, offset)`
- `ICameraStreamService`
  - CRUD on camera stream entities
  - `StartAllMjpegStream()`
  - `ReadMjpeg(ctx, id)`
  - `Shutdown(ctx)`

## Why It Matters

- Keeps handlers and service implementations loosely coupled.
- Allows swapping/testing concrete implementations.
