package stream

import (
	"testing"

	"github.com/pion/rtp"
)

func TestH264IsKeyframeStart(t *testing.T) {
	cases := []struct {
		name    string
		payload []byte
		want    bool
	}{
		{"single IDR", []byte{0x65, 0x88, 0x84}, true},
		{"single SPS", []byte{0x67, 0x42, 0xe0, 0x1f}, true},
		{"single PPS", []byte{0x68, 0xce, 0x3c}, false},
		{"single P-slice", []byte{0x61, 0x9a, 0x00}, false},
		{"STAP-A with SPS", []byte{0x78, 0x00, 0x04, 0x67, 0x42, 0xe0, 0x1f}, true},
		{"STAP-A without keyframe", []byte{0x78, 0x00, 0x03, 0x68, 0xce, 0x3c}, false},
		{"FU-A start of IDR", []byte{0x7c, 0x85, 0x88}, true},
		{"FU-A middle of IDR", []byte{0x7c, 0x05, 0x88}, false},
		{"FU-A start of P-slice", []byte{0x7c, 0x81, 0x9a}, false},
		{"empty", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := h264IsKeyframeStart(tc.payload); got != tc.want {
				t.Fatalf("h264IsKeyframeStart(%v) = %v, want %v", tc.payload, got, tc.want)
			}
		})
	}
}

func TestSessionPrimesLateSubscriberFromKeyframe(t *testing.T) {
	s := newRTSPSession("cam", "rtsp://camera/live", nil)
	s.markReady(CodecH264, "", "42e01f")

	// A P-frame arriving before any keyframe must not be cached.
	s.broadcast(&rtp.Packet{Header: rtp.Header{SequenceNumber: 1}, Payload: []byte{0x61, 0x00}})
	// Keyframe opens a new GOP, followed by a dependent P-frame.
	s.broadcast(&rtp.Packet{Header: rtp.Header{SequenceNumber: 10}, Payload: []byte{0x65, 0x88}})
	s.broadcast(&rtp.Packet{Header: rtp.Header{SequenceNumber: 11}, Payload: []byte{0x61, 0x01}})

	sub, err := s.subscribe()
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer sub.Close()

	if len(sub.Backlog) != 2 {
		t.Fatalf("expected backlog of 2 (keyframe + 1 P-frame), got %d", len(sub.Backlog))
	}
	if !h264IsKeyframeStart(sub.Backlog[0].Payload) {
		t.Fatalf("backlog must start on a keyframe")
	}
	if sub.Backlog[0].Header.SequenceNumber != 10 {
		t.Fatalf("backlog should start at the keyframe (seq 10), got seq %d", sub.Backlog[0].Header.SequenceNumber)
	}
}

func TestSessionKeyframeResetsGOP(t *testing.T) {
	s := newRTSPSession("cam", "rtsp://camera/live", nil)
	s.markReady(CodecH264, "", "42e01f")

	s.broadcast(&rtp.Packet{Header: rtp.Header{SequenceNumber: 10}, Payload: []byte{0x65, 0x88}})
	s.broadcast(&rtp.Packet{Header: rtp.Header{SequenceNumber: 11}, Payload: []byte{0x61, 0x01}})
	// Second keyframe must discard the prior GOP.
	s.broadcast(&rtp.Packet{Header: rtp.Header{SequenceNumber: 20}, Payload: []byte{0x67, 0x42, 0xe0, 0x1f}})

	sub, err := s.subscribe()
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer sub.Close()

	if len(sub.Backlog) != 1 {
		t.Fatalf("expected backlog reset to the latest keyframe, got %d packets", len(sub.Backlog))
	}
	if sub.Backlog[0].Header.SequenceNumber != 20 {
		t.Fatalf("backlog should start at the latest keyframe (seq 20), got seq %d", sub.Backlog[0].Header.SequenceNumber)
	}
}

func TestSessionNoBacklogWithoutKeyframe(t *testing.T) {
	s := newRTSPSession("cam", "rtsp://camera/live", nil)
	s.markReady(CodecH264, "", "42e01f")

	s.broadcast(&rtp.Packet{Header: rtp.Header{SequenceNumber: 1}, Payload: []byte{0x61, 0x00}})
	s.broadcast(&rtp.Packet{Header: rtp.Header{SequenceNumber: 2}, Payload: []byte{0x61, 0x01}})

	sub, err := s.subscribe()
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer sub.Close()

	if len(sub.Backlog) != 0 {
		t.Fatalf("expected no backlog before a keyframe, got %d packets", len(sub.Backlog))
	}
}
