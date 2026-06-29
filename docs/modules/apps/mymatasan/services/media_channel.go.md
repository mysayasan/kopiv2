# Module: apps/mymatasan/services/media_channel.go

## Purpose

Implements the node side of the camera media relay: once paired and enrolled, dials the control plane's media listener (`pairing.mediaPort`, default 49534) over fleet mTLS, and on the parent's request subscribes a local camera's RTSP stream and pumps its RTP up the channel so `myseliasan` can re-broadcast it to browsers over WebRTC.

## Type: `MediaChannelManager`

### Constructor

`NewMediaChannelManager(svc IPairingService, mediaPort int, version string, subscriber MediaSubscriber, resolve MediaSourceResolver, logf)` — `mediaPort <= 0` defaults to `49534`. A nil `subscriber`/`resolve` still establishes the channel for liveness but answers `FrameStart` with an error frame.

### Interfaces Used

| Interface | Implemented By | Role |
|---|---|---|
| `IPairingService` | `PairingService` | Read pairing state and enrollment to gate the channel and obtain the mTLS material. |
| `MediaSubscriber` | `stream.Manager` | Open a shared camera RTP subscription (`Subscribe(source)`) reusing the RTSP session pool. |
| `MediaSourceResolver` | closure in `app.go` | Map a node-local camera ID to its `stream.Source` (RTSP URI + creds). |

### Run Loop

`Run(ctx)` mirrors `ControlChannelManager`: gate on paired + enrolled, call `connectAndServe`, reconnect with exponential backoff (initial 1 s, cap 30 s) on any error. Stops when `ctx` is cancelled.

### Media Protocol (node side)

1. Dial `wss://parentHost:mediaPort/media` with fleet mTLS.
2. Send `FrameHello` (advisory `nodeId` + `version`).
3. Enter read loop: handle `FrameStart` / `FrameStop` from the parent.
4. **`handleStart(streamID, cameraID)`**: subscribe the camera, send `FrameMeta` (codecs), replay the GOP backlog as `FrameBacklog` packets, then pump live RTP as `FrameVideoRTP` (and `FrameAudioRTP` on a goroutine). Each subscription is keyed by `streamID` (parent-allocated, per-browser) so two browsers watching the same camera each get an independent stream and their own keyframe.
5. **`handleStop(streamID)`**: cancel the egress context and close the subscription.
6. A `pingLoop` goroutine keeps the connection alive between media bursts.
7. On disconnect or context cancellation: `teardown` drops the active connection and cancels all active egresses; the parent re-sends `FrameStart` when the node reconnects.

## Notes

- Run as a goroutine alongside `controlChannel.Run` in `app.go`, sharing the monitor lifecycle context.
- `mediaWSURL` derives the dial target by replacing the port in the stored `ParentBaseURL` with `mediaPort` (scheme forced to `wss`). The same host the node uses for the control channel.
- Audio is only pumped when `sub.AudioPackets != nil && sub.AudioCodec != ""`.
- RTP pump errors that occur with an active context are logged; the connection is marked dropped and reconnect runs without backoff (mid-session drops).
