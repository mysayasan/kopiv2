# Module: infra/stream/webrtc.go

## Purpose

Creates browser WebRTC answers from camera RTP subscriptions.

## Responsibilities

- Subscribe to a camera stream source.
- Create a Pion peer connection with configured STUN/TURN ICE servers and a local H264 RTP track.
- Answer a browser offer after ICE gathering.
- Forward camera RTP packets into the browser track.
- Drain RTCP packets and close subscriptions when the peer disconnects.

## Notes

- The HTTP request context is used for setup only; media continues until the WebRTC peer closes.
- The track uses H264 packetization mode 1 for common browser compatibility.
- Smoke coverage negotiates an in-process WebRTC offer/answer and verifies an H264 RTP packet reaches the receiving peer.
