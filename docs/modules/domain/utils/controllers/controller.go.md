# Module: domain/utils/controllers/controller.go

## Purpose

Shared JSON response helpers for API handlers.

## Response Types

- `DefaultResponse` is used by `SendResult`.
- `PagingResponse` is used by `SendPagingResult`.
- `ErrResponse` is used by `SendError`.

## Paging Contract

- `PagingResponse.data` uses offset-window metadata: `limit`, `offset`, `resCnt`, `totalCnt`, `hasNext`, and `nextOffset`.
- `resCnt` is the number of items in the current response.
- `totalCnt` is the total number of rows matching the query.

## Timing Contract

- `SendResult`, `SendPagingResult`, and `SendError` include top-level `durationMs`.
- `durationMs` is measured from the request start time recorded by middleware when available.
- When helpers are used without a timing-aware response writer, `durationMs` is `0`.

## Error Message Disclosure

- `SendError` resolves the response status via `NewErrorUtils().GetHttpStatusCode(err)` and picks the response message: the caller-supplied `message` argument replaces the generic error text whenever it is safe to reveal.
- "Safe to reveal" is either environment (`ENVIRONMENT=dev`, unconditionally) or any client-error status (`4xx`, i.e. `http.StatusBadRequest` ≤ status < `http.StatusInternalServerError`), in every environment. A 4xx message describes the caller's own bad input (validation, conflict, parse failure) and is meant to guide them.
- Server-error (`5xx`) detail messages stay hidden outside dev so internal failure details never leak to clients in production.
- This is a repo-wide behavior: every handler across every app that calls `controllers.SendError` with a detail message on a 4xx now returns that message in production, not just in dev.
