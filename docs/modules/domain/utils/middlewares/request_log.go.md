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

## `statusWriter.Unwrap()` — FIXED: was silently blocking `http.ResponseController`

`statusWriter` (this middleware's `http.ResponseWriter` wrapper) now implements `Unwrap()
http.ResponseWriter { return w.ResponseWriter }`. Without it, `http.ResponseController`
cannot see past this wrapper to the real connection and every method returns "not
supported" — which is what silently defeated `infra/notification/sse_channel.go.md`'s
`SetWriteDeadline(time.Time{})` fix for the SSE stream's 30-second death: the deadline call
returned an error (swallowed as non-fatal, by design, for exactly this kind of older/wrapped
writer), the deadline was never actually cleared, and the notification stream kept dying
every 30 seconds with a browser's `EventSource` silently reconnecting and losing anything
that landed in the reconnect gap.

This is the same class of bug as `statusWriter.Flush()` above (a comment in the source notes
it bit the same feature once before): a `ResponseWriter` wrapper that adds a method the
standard library's helper interfaces expect (`http.Flusher`, `http.ResponseController`'s
`Unwrap` chain) but doesn't forward it breaks any *later* middleware or handler that relies
on that capability, invisibly — the request still succeeds, just without the behavior the
caller asked for. **Any future `ResponseWriter` wrapper added to this middleware chain needs
both `Flush` and `Unwrap`** for this reason.

## Notes

- Logging falls back to the standard logger when no runtime logger is injected.
- Default status capture starts at `200` unless overridden.
- Exposes elapsed milliseconds through `RequestDurationMs()`.
- Covered by `request_log_unwrap_test.go`: asserts `statusWriter` satisfies an
  `interface{ Unwrap() http.ResponseWriter }` and returns the wrapped writer unchanged.
