# Module: apps/myseliasan/apis/recording_stream.go

## Purpose

Range-capable recording playback over the control tunnel. The tunnel's control channel caps
each message at 16 MiB (`infra/control.maxFrameBytes`) and a recorded clip — especially an
encrypted or HEVC one, which the node must materialize as a seekable temp copy before it can
answer `Range` — can be far larger and isn't itself seekable end-to-end through the tunnel.
This endpoint lets the browser's `<video>` element seek a node's recording by walking it in
bounded chunks instead of fetching it whole.

## Route

```
GET /api/nodes/{id}/recording-stream/{segId}
```

Registered on its own `/nodes` subrouter, **before** `NewNodeProxyApi`'s catch-all proxy route
so this specific path wins.

## Constructor

`NewRecordingStreamApi(router, auth, sender, access, session)` — same collaborators as
`NewNodeProxyApi` (`ControlSender`, `INodeAccessService`, `*AccessSessionMidware`).

## Request Flow

1. Authorize exactly like the command proxy: resolve the caller's live role (session
   principal, falling back to the token's baked role) and check `INodeAccessService.Resolve`;
   no access → 403.
2. Parse the browser's `Range` header (`parseByteRange`); an absent/invalid range defaults to
   the first chunk. The requested end is capped so the span never exceeds
   `recordingStreamChunk` (8 MiB).
3. Build a proxied request to the node's own `GET /api/recording/segments/{segId}/download`
   (forwarding `?transcode=h264` when the browser asked for it) with the capped `Range` header,
   and send it over the control channel via `ControlSender.SendRequest`.
4. The node must answer `206 Partial Content` (its `downloadSegment` handler now serves Range
   — see `apps/mymatasan/apis/recording.go.md`). Any other status (an older node without Range
   support, or a clip that couldn't be made seekable) is surfaced as a `503`-class
   "recording not streamable over the link" error rather than a full-clip fallback.
5. Forward `Content-Type`, `Content-Range`, and `Accept-Ranges` from the node's response
   (`Accept-Ranges` defaults to `bytes` if the node didn't send it), set `Content-Length` from
   the actual body size, and write `206` with the chunk body.

The browser then issues successive `Range` requests as it plays/seeks, each capped and
proxied the same way — never exceeding the tunnel's per-message limit regardless of clip size.

## Notes

- `recordingStreamChunk = 8 << 20` (8 MiB) — comfortably under the 16 MiB control-channel frame
  cap, leaving headroom for control-message framing overhead.
- Depends on `apps/mymatasan/apis/control_dispatch.go` forwarding **all** response headers
  (not just `Content-Type`) so `Content-Range`/`Accept-Ranges`/`Content-Length` survive the
  round trip through `httptest.NewRecorder` → `control.Response`.
- Used by the embedded node Recordings tab (`nodecam/recording.js`), which detects the
  myseliasan proxy base and points playback at `.../recording-stream/{id}` instead of the
  node's own `.../download` URL.
