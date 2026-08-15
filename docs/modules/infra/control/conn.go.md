# Module: infra/control/conn.go

## Purpose

Wraps a WebSocket as a frame-oriented connection with keepalive deadline management for the control channel.

## Responsibilities

- Define `Conn`, wrapping a `*websocket.Conn` with an internal write mutex.
- `newConn`: apply the inbound frame size limit and read deadline, and install a pong handler that extends the read deadline on each keepalive.
- `ReadFrame` / `WriteFrame`: read and write one JSON `Frame`; a successful read also extends the read deadline so active traffic keeps the connection alive between pings.
- `Ping` / `Close`: send a WebSocket-level keepalive ping and tear down the socket. `Close` is
  safe on a nil or zero-valued `*Conn` (returns `nil`) — teardown paths close whatever they were
  handed, often in a `defer` and often more than once, and a `Close` that panicked on an unstarted
  connection would turn an ordinary cleanup into a lost goroutine.
- `RemoteAddr() string`: the peer's `host:port`, read off the underlying `*websocket.Conn`; empty when the socket has no addressable peer. Used by `apps/myseliasan/services/control_server.go` to record where a refused (stranded) node connection came from (see that module's doc, "Rejected (stranded) connection tracking") — purely diagnostic, not used for any auth decision.
- Expose keepalive timing (`PingPeriod`) and limits used by both server and client sides.

## Notes

- A single goroutine must own `ReadFrame`; writes are serialized internally, so any goroutine may call `WriteFrame`/`Ping` concurrently.
- `pongWait` (60s) bounds idle time before the connection is considered dead; `PingPeriod` is `9/10` of it so a pong arrives before the peer's read deadline elapses.
- `maxFrameBytes` (16 MiB) is generous because a tunneled response may carry a snapshot image, but bulk media is never sent over the control channel.
