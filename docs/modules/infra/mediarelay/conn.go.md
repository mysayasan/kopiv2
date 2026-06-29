# Module: infra/mediarelay/conn.go

## Purpose

Wraps a WebSocket as a frame-oriented binary connection with keepalive deadline management for the media channel. Mirrors `infra/control/conn.go` in structure but optimized for binary throughput rather than JSON request/response.

## Responsibilities

- Define `Conn`, wrapping a `*websocket.Conn` with an internal write mutex so any goroutine may `WriteFrame`/`Ping` concurrently while a single goroutine owns `ReadFrame`.
- `newConn`: set `maxFrameBytes` (4 MiB, generous headroom for fragmented NALs / large audio frames), install a pong handler extending the read deadline on each keepalive.
- `ReadFrame`: block for the next binary message, extend the read deadline, reject non-binary messages, and delegate to `parseFrame`.
- `WriteFrame`: marshal and write one frame as a binary message under the write mutex.
- `Ping` / `Close`: WebSocket-level keepalive and teardown.

## Keepalive Constants

| Constant | Value | Meaning |
|---|---|---|
| `writeWait` | 10 s | Deadline for a single frame/control write. |
| `pongWait` | 60 s | How long a read may go without traffic before the connection is considered dead. |
| `PingPeriod` | 54 s (9/10 of `pongWait`) | How often each side sends a ping; exported so both `MediaChannelManager` and `MediaRelayHub` use the same period. |
| `maxFrameBytes` | 4 MiB | Inbound frame size limit. |

## Notes

- Write serialization via `writeMu` means the RTP pump goroutine and the ping loop can coexist safely on the same connection.
- `pongWait` and `PingPeriod` match the control channel convention; the media channel is more latency-sensitive but uses the same 60 s/54 s pairing.
