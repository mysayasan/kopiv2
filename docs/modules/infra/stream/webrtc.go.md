# Module: infra/stream/webrtc.go

## Purpose

Creates browser WebRTC answers from camera RTP subscriptions, forwarding both video and audio when available.

## Responsibilities

- Subscribe to a camera stream source.
- Create a Pion peer connection with configured STUN/TURN ICE servers.
- Add an H264 RTP track for video (always present).
- Add a PCMA or PCMU RTP track for audio when the subscription includes audio packets.
- Answer a browser offer after ICE gathering.
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
- Smoke coverage negotiates an in-process WebRTC offer/answer and verifies an H264 RTP packet reaches the receiving peer.
