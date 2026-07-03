# Module: infra/talk/talk.go

## Purpose

Defines the `Session` contract and capability probe for one-way "talk-back" audio delivery from a browser microphone to a camera's built-in speaker (two-way audio) over the standard ONVIF audio backchannel.

## Responsibilities

- Declare `Session`: `WritePCMA(payload []byte, timestamp uint32) error` plus `Close() error` — one open audio path to a camera's speaker.
- `HasBackchannel(ctx, rtspURI)` — probes whether a camera's RTSP endpoint advertises an ONVIF audio backchannel with a G.711 format, by sending an RTSP `DESCRIBE` with `RequestBackChannels: true` (the `Require: www.onvif.org/ver20/backchannel` header) and checking the returned SDP for a backchannel media with a G.711 format. Returns `false` on any parse/connect/timeout error or when no backchannel is found — a probe never blocks the caller past `probeTimeout` (6s) or the passed `ctx`.

## Notes

- Sessions consume G.711 A-law (PCMA, 8 kHz) frames — the codec browsers encode natively in WebRTC; the concrete `onvifSession` (`onvif.go`) converts to µ-law internally when the camera's backchannel advertises PCMU.
- Targets Hikvision, Dahua, Axis, and most ONVIF Profile T cameras. TP-Link Tapo/VIGI cameras use a different, non-ONVIF speaker protocol and are not yet supported by this package (see `apps/mymatasan/entities/camera.go`'s `TalkTransport`/`TalkPassword` fields, reserved for a future non-ONVIF transport).
- Uses `gortsplib` for the RTSP/SDP transport, matching the rest of the codebase's RTSP stack.
