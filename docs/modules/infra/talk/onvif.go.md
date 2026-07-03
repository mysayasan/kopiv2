# Module: infra/talk/onvif.go

## Purpose

Implements the ONVIF RTSP audio backchannel `Session`: dials a camera, opens its backchannel media, and pushes G.711 RTP frames into it.

## Responsibilities

- `DialONVIF(rtspURI)` — opens an RTSP client with `RequestBackChannels: true`, runs `DESCRIBE`, locates the first backchannel media carrying a G.711 format (`findG711Backchannel`), `SETUP`s and `PLAY`s it, and returns a ready `onvifSession`. Errors when the camera exposes no G.711 backchannel.
- `onvifSession.WritePCMA(payload, timestamp)` — wraps the payload in an RTP packet (converting A-law → µ-law first via `alawToUlaw` when the backchannel format is PCMU), increments the sequence number, and writes it over the RTSP session with `client.WritePacketRTP`.
- `onvifSession.Close()` — idempotent; closes the underlying RTSP client.
- `findG711Backchannel(desc)` — scans the SDP session description for a `media.IsBackChannel` entry whose format is `*format.G711`.

## Notes

- The RTP SSRC is randomized per session (`randUint32`, crypto/rand with a time-based fallback); the sequence number starts at zero and increments per frame — no RTP timestamp arithmetic is done here, the caller (webrtc.go's `pumpTrack`) forwards the browser track's own RTP timestamps unchanged.
- `WritePCMA` is guarded by a mutex and returns `errSessionClosed` after `Close`.
- Uses TCP (interleaved) RTSP transport, matching the read-side RTSP clients elsewhere in the codebase.
