# Module: infra/talk/talk.go

## Purpose

Defines the `Session` contract and capability probes for two-way "talk-back" audio delivery from a browser microphone to a camera's built-in speaker: the standard ONVIF RTSP audio backchannel, and the TP-Link Tapo/VIGI proprietary port-8800 protocol (see `tapo.go`/`mpegts.go`).

## Responsibilities

- Declare `Session`: `WritePCMA(payload []byte, timestamp uint32) error` plus `Close() error` — one open audio path to a camera's speaker (implemented by `onvifSession` in `onvif.go` and `tapoSession` in `tapo.go`).
- `HasBackchannel(ctx, rtspURI)` — probes whether a camera's RTSP endpoint advertises an ONVIF audio backchannel with a G.711 format, by sending an RTSP `DESCRIBE` with `RequestBackChannels: true` (the `Require: www.onvif.org/ver20/backchannel` header) and checking the returned SDP for a backchannel media with a G.711 format. Returns `false` on any parse/connect/timeout error or when no backchannel is found — a probe never blocks the caller past `probeTimeout` (6s) or the passed `ctx`.
- `Probe8800(ctx, host)` — TCP-dials `host:8800` (`:8800` assumed if no port given) and POSTs an unauthenticated multipart `/stream` request. Reports `Probe8800Result{Supported, EncryptType, NoneAuth}` **only** when the response is a `401` digest challenge that fingerprints as TP-Link's "Streamd" talk daemon (`isTPLinkStreamd` — `Server: Streamd` or a `TP-Link`/`IP-Camera` realm in the `WWW-Authenticate` header); any other device that merely has port 8800 open returns `Supported: false`. `EncryptType == 3` means the cloud password must be SHA-256-hashed (else MD5); `NoneAuth` means the camera still offers the legacy `username="none"` exchange (CVE-2022-37255) needing no password at all.
- `isTPLinkStreamd(server, auth)` — the fingerprint check shared by `Probe8800` and `tapoAuthenticatedConn` (`tapo.go`) that gates both the capability probe and the actual dial to genuine TP-Link devices.

## Notes

- Sessions consume G.711 A-law (PCMA, 8 kHz) frames — the codec browsers encode natively in WebRTC; `onvifSession` converts to µ-law internally when the camera's backchannel advertises PCMU, and `tapoSession` always sends PCMA (TP-Link's private stream type 0x90).
- ONVIF targets Hikvision, Dahua, Axis, and most ONVIF Profile T cameras (their RTSP backchannel reuses the already-stored camera credentials, so no extra password). TP-Link Tapo/VIGI consumer cameras expose no RTSP backchannel and use the port-8800 transport instead (`tapo.go`), which needs a separately stored password — see `apps/mymatasan/services/talk.go`'s `NeedsPassword`/`SaveTalkPassword`.
- Uses `gortsplib` for the RTSP/SDP transport, matching the rest of the codebase's RTSP stack; the TP-Link path uses the standard library `net`/`net/http` directly (no RTSP involved).
