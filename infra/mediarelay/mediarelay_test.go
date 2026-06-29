package mediarelay

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/pion/rtp"
)

func TestFrameMarshalRoundTrip(t *testing.T) {
	cases := []*Frame{
		{Type: FrameHello, StreamID: 0, Body: []byte(`{"nodeId":"n1","version":"1.2.3"}`)},
		{Type: FrameStart, StreamID: 42, Body: []byte(`{"subStream":1}`)},
		{Type: FrameStop, StreamID: 42, Body: nil},
		{Type: FrameVideoRTP, StreamID: 7, Body: []byte{0x80, 0x60, 0x00, 0x01, 0xde, 0xad, 0xbe, 0xef}},
	}
	for _, in := range cases {
		out, err := parseFrame(in.Marshal())
		if err != nil {
			t.Fatalf("parseFrame(%d): %v", in.Type, err)
		}
		if out.Type != in.Type || out.StreamID != in.StreamID || !bytes.Equal(out.Body, in.Body) {
			t.Fatalf("roundtrip mismatch: got %+v want %+v", out, in)
		}
	}
}

func TestParseFrameRejectsShort(t *testing.T) {
	if _, err := parseFrame([]byte{0x01, 0x02}); err == nil {
		t.Fatal("expected error for short frame")
	}
}

// TestConnLoopback verifies a real WebSocket carries binary frames intact, including
// a marshaled RTP packet, through ReadFrame/WriteFrame.
func TestConnLoopback(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		conn := newConn(ws)
		// Echo every frame back until the client closes.
		for {
			f, err := conn.ReadFrame()
			if err != nil {
				return
			}
			if err := conn.WriteFrame(f); err != nil {
				return
			}
		}
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	client := newConn(ws)
	defer client.Close()

	pkt := &rtp.Packet{
		Header:  rtp.Header{Version: 2, PayloadType: 96, SequenceNumber: 5, Timestamp: 9000, SSRC: 0xdeadbeef},
		Payload: []byte{0x67, 0x42, 0x00, 0x1f},
	}
	raw, err := pkt.Marshal()
	if err != nil {
		t.Fatalf("marshal rtp: %v", err)
	}
	if err := client.WriteFrame(&Frame{Type: FrameVideoRTP, StreamID: 11, Body: raw}); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := client.ReadFrame()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got.Type != FrameVideoRTP || got.StreamID != 11 {
		t.Fatalf("header mismatch: %+v", got)
	}
	var back rtp.Packet
	if err := back.Unmarshal(got.Body); err != nil {
		t.Fatalf("unmarshal rtp: %v", err)
	}
	if back.SequenceNumber != 5 || back.SSRC != 0xdeadbeef || !bytes.Equal(back.Payload, pkt.Payload) {
		t.Fatalf("rtp roundtrip mismatch: %+v", back)
	}
}
