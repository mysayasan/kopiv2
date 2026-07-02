# Module: infra/talk/webrtc.go

## Purpose

Negotiates the browser→server leg of a talk-back session: answers the browser's WebRTC microphone offer and pumps the received audio into an already-open camera `Session`.

## Responsibilities

- `AnswerBrowserTalk(offer, session, iceServers)` — builds a `webrtc.MediaEngine` registering **only** PCMA (payload type 8, 8 kHz mono) so the negotiated codec is exactly what the ONVIF backchannel consumes (no server-side transcode), creates a peer connection configured with the caller's ICE servers, adds a `recvonly` audio transceiver, sets the browser's offer as the remote description, creates and sets the local answer, waits (up to 15s) for ICE gathering to complete, and returns the resulting `SessionDescription`.
- `pc.OnTrack` starts `pumpTrack` in a goroutine once the browser's audio track arrives; `pc.OnConnectionStateChange` closes the session and peer connection (`closeAll`, via `sync.Once`) on `Closed`/`Failed`/`Disconnected`.
- `pumpTrack(track, session, done)` — reads RTP packets off the browser's audio track in a loop and forwards each non-empty payload to `session.WritePCMA`, using the browser's own RTP timestamp; returns (calling `done`, which closes the session) on read error, write error, or track end.
- `SessionDescription` — minimal `{Type, SDP}` offer/answer pair mirroring `infra/stream`'s type so callers don't import `pion/webrtc` directly.
- `ICEServer` — mirrors `infra/stream`'s ICE server config (`URLs`, `Username`, `Credential`); `webRTCICEServers` converts the slice to `pion/webrtc` types, skipping entries with no URLs.

## Notes

- The browser side must offer a `sendonly` or `sendrecv` audio track; the server always answers `recvonly` — this package only receives from the browser and forwards to the camera, it never sends audio back to the browser.
- A missing/empty offer SDP is rejected immediately with an error before any peer connection work happens.
- ICE gathering has a 15s timeout (`webRTCSetupTimeout`); timing out closes the session/peer connection and returns an error rather than answering with an incomplete candidate set.
