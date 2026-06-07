# Module: domain/utils/controllers/controller.go

## Purpose

Shared JSON response helpers for API handlers.

## Response Types

- `DefaultResponse` is used by `SendResult`.
- `PagingResponse` is used by `SendPagingResult`.
- `ErrResponse` is used by `SendError`.

## Timing Contract

- `SendResult`, `SendPagingResult`, and `SendError` include top-level `durationMs`.
- `durationMs` is measured from the request start time recorded by middleware when available.
- When helpers are used without a timing-aware response writer, `durationMs` is `0`.
