# Module: infra/mediarelay/frame.go

## Purpose

Defines the `mediarelay` package and the binary wire vocabulary for the node→parent media channel that carries camera RTP from an adopted `mymatasan` node to the `myseliasan` control plane for WebRTC re-broadcast to browsers (the "Option B" full relay).

The channel is a sibling of `infra/control`: a second node-dialed WebSocket-over-fleet-mTLS connection on its own port (`pairing.mediaPort`, default 49534), so high-rate camera media never competes with control traffic.

## Frame Format

Unlike the control channel (JSON throughout), the media channel is **throughput-first**: every WebSocket binary message is one `Frame` whose **9-byte header** (1 type byte + 8-byte big-endian `StreamID`) is followed by the raw payload — a marshaled RTP packet for media frames, or small JSON for the low-rate control frames (Hello / Start / Stop / Meta / Error). RTP is never base64-encoded.

## `FrameType` Values

| Value | Constant | Direction | Payload |
|---|---|---|---|
| 1 | `FrameHello` | node→parent | `HelloPayload` JSON (advisory identity + version) |
| 2 | `FrameStart` | parent→node | `StartPayload` JSON (camera to stream, `StreamID` = parent-allocated subscription id) |
| 3 | `FrameStop` | parent→node | empty (stop `StreamID`) |
| 4 | `FrameMeta` | node→parent | `MetaPayload` JSON (codec info before first media packet) |
| 5 | `FrameBacklog` | node→parent | marshaled `rtp.Packet` (GOP snapshot, sent right after Meta) |
| 6 | `FrameVideoRTP` | node→parent | marshaled `rtp.Packet` (live video) |
| 7 | `FrameAudioRTP` | node→parent | marshaled `rtp.Packet` (live audio, omitted if camera has no audio) |
| 8 | `FrameError` | node→parent | `ErrorPayload` JSON (stream start failure) |

## Key Payload Types

- `HelloPayload` — `{nodeId, version}`: identity is already proven by the fleet-CA mTLS client cert; this is advisory/diagnostic.
- `StartPayload` — `{cameraId, subStream}`: `StreamID` in the frame header is the parent-allocated per-subscription id; `cameraId` is the node-local camera. Two browsers watching the same camera get independent streams (independent `StreamID`s), each with its own keyframe backlog.
- `MetaPayload` — `{videoCodec, h264ProfileLevelId, audioCodec}`: mirrors `stream.Subscription` fields that affect SDP negotiation.
- `ErrorPayload` — `{message}`: the node could not start the requested stream.

## Notes

- The dial direction is node→parent (mirrors the control channel): nodes behind NAT/firewall dial out rather than accepting inbound connections from the parent.
- `Frame.Marshal()` encodes to a single binary WebSocket message; `parseFrame` decodes it. The `Body` slice aliases the input buffer for the frame's lifetime.
- `headerLen` is 9 bytes (1 type + 8 stream id).
