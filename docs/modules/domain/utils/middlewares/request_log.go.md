# Module: domain/utils/middlewares/request_log.go

## Purpose

Request tracing and lightweight access logging.

## Behavior

- Reads incoming `X-Request-ID` if present.
- Generates UUID when missing.
- Returns `X-Request-ID` in response headers.
- Records request start time on the wrapped response writer for shared response helpers.
- Measures request duration and logs through the injected runtime logger when available:
  - request ID
  - method
  - path
  - status
  - duration (ms)
  - remote address

## Notes

- Logging falls back to the standard logger when no runtime logger is injected.
- Default status capture starts at `200` unless overridden.
- Exposes elapsed milliseconds through `RequestDurationMs()`.
