# Module: infra/notification/sse_channel.go

## Purpose

`SSEChannel` delivers notifications to connected browsers over Server-Sent Events. It is
both a `Channel` (the hub sends to it on every publish) and an `http.Handler` (each browser
connects to it, one goroutine per connection, via `GET /api/notifications/stream`).
Broadcasts are non-blocking: a client whose buffer is full is skipped for that message
rather than stalling the publisher.

## Behavior

- `NewSSEChannel(opts SSEOptions) *SSEChannel` — `opts.ClientBuffer` (per-connection queue
  depth) defaults to 16 when `<= 0`.
- `Send(ctx, n)` — broadcasts to every connected client's channel without blocking; a full
  channel drops that message for that client only (a slow reader loses updates, not the
  stream).
- `ServeHTTP` streams one client's subscription until the request context is cancelled:
  sets SSE headers, extends the write deadline (below), sends a `": connected"` priming
  comment, then loops on the client's channel, a heartbeat ticker, and `ctx.Done()`,
  extending the deadline again before every write.

## FIXED: the stream used to die 30 seconds after it opened

`http.Server.WriteTimeout` (30s by default in this codebase) is an ABSOLUTE deadline from
the start of the request and does **not** reset as more is written, so every SSE stream —
finite response or not — was cut 30 seconds after it opened, regardless of how much traffic
it carried. The symptom was close to invisible: a browser's `EventSource` reconnects
silently, so the bell kept appearing to work while every notification that happened to land
in a reconnect gap was lost, forever, with nothing logged. This affects **every install**,
including a plain standalone one — it has nothing to do with clustering.

Fixed in `ServeHTTP`, right after the SSE headers are set and before `WriteHeader`, and
again before every subsequent write:

```go
rc := http.NewResponseController(w)
extendDeadline := func() bool {
    _ = rc.SetWriteDeadline(time.Now().Add(sseWriteTimeout))
    return true
}
extendDeadline()
```

**A ROLLING deadline, not a cleared one.** An earlier version of this fix cleared the
deadline outright (`rc.SetWriteDeadline(time.Time{})`), which solved the 30-second death but
traded it for a worse failure: a peer that vanished without closing (a laptop lid, a dropped
VPN) leaves a write blocked forever once the socket buffer fills, holding the goroutine and
its subscriber slot for the life of the process. Pushing the deadline `sseWriteTimeout`
(40s = `2× sseHeartbeatInterval`) forward before each write instead means the stream lives as
long as it is being consumed, and a peer that stopped consuming fails a write — the loop
returns on that error — and is reaped within `sseWriteTimeout` instead of never.
`SetWriteDeadline` failing is not fatal (an older or wrapped `ResponseWriter` may not support
it), in which case the stream still works, just with the server's own `WriteTimeout` as its
ceiling — the pre-fix behavior.

**This alone did nothing** until a second, independent bug (below) was fixed — see
`domain/utils/middlewares/request_log.go.md`.

### The heartbeat

A `": ping\n\n"` comment line on a `sseHeartbeatInterval` (20s) ticker, added alongside the
deadline fix, extending the write deadline before it writes just like a real notification
does. Nothing consumes the comment itself; its only job is to make a dead peer FAIL a write
(and to keep the rolling deadline moving on an otherwise-quiet stream), so:

- a client that vanished without closing (a laptop lid, a dropped VPN) is reaped instead of
  holding a subscriber slot forever, and
- an intermediary (proxy/load balancer) that idles out a quiet connection sees traffic and
  keeps it open.

20s is comfortably under the idle timeouts a proxy or load balancer typically applies (60s
is common) while staying rare enough to be free. `sseWriteTimeout` (40s) must exceed the
heartbeat interval — otherwise a healthy but quiet stream would expire between beats — while
staying short enough that a peer which stopped reading is reaped in seconds, not held for
the life of the process.

### Live verification

Benched against the fix: 14 heartbeats observed and delivery confirmed after 290 seconds of
idle, on a test that failed twice (once per half of the bug — see the middleware doc) before
both were fixed.

## Notes

- `subscribe`/`unsubscribe` manage the per-client channel map under a mutex; `Close`
  disconnects every client and rejects new subscriptions (used on shutdown).
- `ClientCount()` exposes the live connection count for diagnostics.
- The deadline fix is a per-response opt-out via `http.ResponseController`, not a change to
  the server's global `WriteTimeout` — every other (finite) response on the same server keeps
  the 30s ceiling.
- See `domain/utils/middlewares/request_log.go.md` for the reason the deadline fix initially
  had no effect, and `domain/notification/service.go.md`'s `RelayToStream` for how a
  multi-instance deployment feeds this same channel with notifications relayed from another
  instance.
