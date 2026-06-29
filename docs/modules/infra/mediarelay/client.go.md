# Module: infra/mediarelay/client.go

## Purpose

Node-side dial helper for the media channel. Opens a fleet-mTLS WebSocket connection to the parent's media endpoint.

## Responsibilities

- `Dial(ctx, url, tlsCfg)` — connects to `wss://host:port/media` presenting the node's fleet client cert via `tlsCfg`, with a 15 s handshake timeout and 64 KiB read/write buffers. Returns a `*Conn` ready for `ReadFrame`/`WriteFrame`.

## Notes

- Called by `MediaChannelManager.connectAndServe` in `apps/mymatasan/services/media_channel.go`.
- The URL is derived by `mediaWSURL` from the stored `ParentBaseURL` with the media port substituted; scheme is always `wss` (the TLS config enforces fleet-CA verification).
- A `context.Context` deadline applies to the WebSocket handshake only; in-flight reads/writes use the `Conn` keepalive deadlines after that.
