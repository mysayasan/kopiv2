# Module: infra/stream/webrtc.go

## Purpose

Creates browser WebRTC answers from camera RTP subscriptions, forwarding both video and audio when available. Also provides a shared pion engine and a raw RTP subscription helper for the node media relay path.

## Key Types

### `Manager`

Wraps a `Connector` (RTSP session pool) and an optional shared `*webrtc.API` (the engine). Created via:

- `NewManagerWithConnector(connector)` — default pion per-peer ephemeral ports (same-LAN / local dev).
- `NewManagerWithConnectorEngine(connector, engine)` — same, but uses the shared engine's `SettingEngine` (NAT 1:1 public-IP advertisement + fixed UDP mux) when `engine` is non-nil.

### `WebRTCEngine`

A shared pion `API` built once at startup for the parent media relay. Created via `NewWebRTCEngine(publicIPs []string, udpPort int)`:

- Returns `(nil, nil)` when both args are zero/empty — callers then use per-peer default behavior (host candidates, ephemeral ports), which is correct for same-LAN/local dev.
- When `publicIPs` are provided, configures `SetNAT1To1IPs` (host-candidate type) so browsers on other networks receive the parent's external IP.
- When `udpPort > 0`, binds a shared `ICEUDPMux` on that port (one firewall rule for all browser peers).

## Responsibilities

- `Manager.Subscribe(source)` — opens (or shares) a camera RTP subscription without building a browser peer. Used by the node media channel (`services/media_channel.go`) to relay RTP to the control plane; the caller owns and must `Close` the returned `*Subscription`.
- `Manager.CreateWebRTCAnswerWithOptions(ctx, source, offer, opts)` — full path: subscribe, create a peer connection via the manager's configured API, add H264 video and optional G.711/Opus audio tracks, answer the browser offer after ICE gathering.
- Add an H264 RTP track for video (always present).
- Add a PCMA or PCMU RTP track for audio when the subscription includes audio packets.
- Forward camera video RTP packets into the browser video track (`pumpRTP`).
- Forward camera audio RTP packets into the browser audio track (goroutine inside `pumpRTP`).
- Drain RTCP packets from each sender and close subscriptions when the peer disconnects.

## Audio Track Creation

`audioCodecCapability` maps `CodecPCMA` → `audio/PCMA` (PT=8, 8 kHz) and `CodecPCMU` → `audio/PCMU` (PT=0, 8 kHz).
The audio track and sender are created only when `sub.AudioPackets != nil && sub.AudioCodec != ""`.
Audio packets are pumped in a goroutine that exits when the channel closes; it does not call `closePeer` so an audio-only error does not terminate the video stream.
When the subscription has no audio, the browser's `a=inactive` SDP answer section is negotiated automatically by Pion; `ontrack` does not fire for audio on the browser side.

## Notes

- The HTTP request context is used for setup only; media continues until the WebRTC peer closes.
- The video track uses H264 packetization mode 1 for common browser compatibility.
- PCMA and PCMU are RFC 3551 static payload types supported natively by all major browsers — no transcoding required for G.711 camera audio.
- `newPeerConnection` dispatches to the manager's shared API when present, or to pion's package default — so the same `CreateWebRTCAnswerWithOptions` code path serves both local cameras (default API) and relayed node cameras (shared engine API).
- Smoke coverage negotiates an in-process WebRTC offer/answer and verifies an H264 RTP packet reaches the receiving peer.
