// Package mediarelay implements the node→parent media channel that carries camera
// RTP from an adopted mymatasan node to the myseliasan control plane, which then
// re-broadcasts it to browsers over WebRTC (the "Option B" full relay). It is a
// sibling of infra/control: a second node-dialed connection over the same fleet-CA
// mTLS trust, on its own port, so high-rate media never competes with the control
// channel.
//
// Unlike control (JSON frames, request/response), this channel is throughput-first:
// each WebSocket BINARY message is one frame whose 9-byte header is followed by the
// raw payload — a marshaled RTP packet for media frames, or small JSON for the
// low-rate control frames (Hello/Start/Stop/Meta/Error). RTP is never base64'd.
package mediarelay

import (
	"encoding/binary"
	"fmt"
)

// FrameType discriminates the binary messages on the media channel.
type FrameType byte

const (
	// FrameHello is the node's first frame after connecting (identity + version).
	FrameHello FrameType = 1
	// FrameStart is a parent→node request to begin streaming the camera identified by
	// the frame's StreamID. Payload: StartPayload JSON.
	FrameStart FrameType = 2
	// FrameStop is a parent→node request to stop streaming StreamID. Empty payload.
	FrameStop FrameType = 3
	// FrameMeta is a node→parent description of a stream (codecs, H264 profile) sent
	// before media so the parent can build its WebRTC tracks. Payload: MetaPayload JSON.
	FrameMeta FrameType = 4
	// FrameBacklog is a node→parent video RTP packet from the current GOP snapshot,
	// sent right after Meta so a late-joining browser starts on a keyframe. Payload:
	// a marshaled RTP packet.
	FrameBacklog FrameType = 5
	// FrameVideoRTP is a node→parent live video RTP packet. Payload: marshaled RTP.
	FrameVideoRTP FrameType = 6
	// FrameAudioRTP is a node→parent live audio RTP packet. Payload: marshaled RTP.
	FrameAudioRTP FrameType = 7
	// FrameError is a node→parent failure notice for StreamID. Payload: ErrorPayload JSON.
	FrameError FrameType = 8
)

// headerLen is the fixed binary header: 1 type byte + 8-byte big-endian StreamID.
const headerLen = 1 + 8

// Frame is one message on the media channel. StreamID is the node-local camera id
// the frame pertains to (0 for connection-level frames like Hello). Body is the raw
// payload — a marshaled RTP packet for media frames, JSON for control frames.
type Frame struct {
	Type     FrameType
	StreamID uint64
	Body     []byte
}

// Marshal encodes the frame to a single binary WebSocket message.
func (f *Frame) Marshal() []byte {
	buf := make([]byte, headerLen+len(f.Body))
	buf[0] = byte(f.Type)
	binary.BigEndian.PutUint64(buf[1:headerLen], f.StreamID)
	copy(buf[headerLen:], f.Body)
	return buf
}

// parseFrame decodes a binary WebSocket message into a Frame. The returned Body
// aliases the input slice (the caller owns the message buffer for the frame's life).
func parseFrame(msg []byte) (*Frame, error) {
	if len(msg) < headerLen {
		return nil, fmt.Errorf("mediarelay: short frame (%d bytes)", len(msg))
	}
	return &Frame{
		Type:     FrameType(msg[0]),
		StreamID: binary.BigEndian.Uint64(msg[1:headerLen]),
		Body:     msg[headerLen:],
	}, nil
}

// HelloPayload announces the node on connect (the nodeID is also proven by the mTLS
// client cert CN; this is advisory/diagnostic).
type HelloPayload struct {
	NodeID  string `json:"nodeId"`
	Version string `json:"version"`
}

// StartPayload carries a parent→node start request. The frame's StreamID is a
// parent-allocated, per-subscription id (so two browsers watching the same camera get
// independent streams, each with its own keyframe backlog); CameraID names the actual
// node-local camera to pull. SubStream selects a profile (0 = the configured stream).
type StartPayload struct {
	CameraID  uint64 `json:"cameraId"`
	SubStream int    `json:"subStream,omitempty"`
}

// MetaPayload describes a node stream so the parent's WebRTC layer can negotiate the
// matching tracks. Mirrors the fields of stream.Subscription that affect SDP.
type MetaPayload struct {
	VideoCodec         string `json:"videoCodec"`
	H264ProfileLevelID string `json:"h264ProfileLevelId,omitempty"`
	AudioCodec         string `json:"audioCodec,omitempty"`
}

// ErrorPayload reports that a requested stream could not be produced.
type ErrorPayload struct {
	Message string `json:"message"`
}
