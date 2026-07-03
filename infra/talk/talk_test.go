package talk

import "testing"

// TestMuxerHeaderAlignment verifies the PAT+PMT header is exactly two 188-byte
// TS packets (what Tapo firmware expects before audio).
func TestMuxerHeaderAlignment(t *testing.T) {
	var m tsMuxer
	h := m.header()
	if len(h) != 2*tsPacketSize {
		t.Fatalf("header length = %d, want %d", len(h), 2*tsPacketSize)
	}
	if h[0] != tsSyncByte || h[tsPacketSize] != tsSyncByte {
		t.Fatalf("TS packets must start with sync byte 0x47")
	}
}

// TestMuxerPayloadAlignment verifies every emitted audio payload is a whole
// number of 188-byte TS packets and starts on a sync byte.
func TestMuxerPayloadAlignment(t *testing.T) {
	var m tsMuxer
	for i, size := range []int{160, 320, 1024} {
		out := m.payload(uint32(i*160), make([]byte, size))
		if len(out) == 0 || len(out)%tsPacketSize != 0 {
			t.Fatalf("payload(size=%d) length %d not a multiple of %d", size, len(out), tsPacketSize)
		}
		if out[0] != tsSyncByte {
			t.Fatalf("payload(size=%d) does not start with sync byte", size)
		}
	}
}

// TestMuxerPTSAdvances checks PTS accumulates the timestamp deltas (first frame
// PTS 0, then + delta each frame).
func TestMuxerPTSAdvances(t *testing.T) {
	var m tsMuxer
	m.payload(1000, make([]byte, 160))
	if m.pts != 0 {
		t.Fatalf("first frame PTS = %d, want 0", m.pts)
	}
	m.payload(1160, make([]byte, 160))
	if m.pts != 160 {
		t.Fatalf("second frame PTS = %d, want 160", m.pts)
	}
}

// TestAlawToUlaw checks A-law → µ-law conversion used when a camera's ONVIF
// backchannel advertises PCMU while the browser sends PCMA.
func TestAlawToUlaw(t *testing.T) {
	in := []byte{0xD5, 0x55, 0x00, 0xFF} // includes A-law silence (0xD5)
	out := alawToUlaw(in)
	if len(out) != len(in) {
		t.Fatalf("length changed: %d -> %d", len(in), len(out))
	}
	// A-law silence (0xD5) decodes to near-zero PCM, which re-encodes to a µ-law
	// near-silence byte (0xFE/0xFF — the largest-magnitude, quietest codes).
	if out[0] < 0xFE {
		t.Errorf("A-law silence should map to µ-law near-silence (>=0xFE), got 0x%02X", out[0])
	}
}

func TestIsTPLinkStreamd(t *testing.T) {
	cases := []struct {
		server, auth string
		want         bool
	}{
		{"Streamd", `Digest realm="TP-Link IP-Camera", encrypt_type="3"`, true},
		{"", `Digest realm="TP-Link IP-Camera", encrypt_type="3"`, true},
		{"streamd", `Digest realm="whatever"`, true},
		{"", `Digest realm="Some IP-Camera"`, true},
		{"nginx", `Digest realm="Acme Router", qop="auth"`, false},
		{"", ``, false},
	}
	for _, c := range cases {
		if got := isTPLinkStreamd(c.server, c.auth); got != c.want {
			t.Errorf("isTPLinkStreamd(%q, %q) = %v, want %v", c.server, c.auth, got, c.want)
		}
	}
}

func TestNormalizeTapoHost(t *testing.T) {
	cases := map[string]string{
		"192.168.1.188":      "192.168.1.188:8800",
		"192.168.1.188:8800": "192.168.1.188:8800",
		"192.168.1.188:9999": "192.168.1.188:9999",
	}
	for in, want := range cases {
		if got := normalizeTapoHost(in); got != want {
			t.Errorf("normalizeTapoHost(%q) = %q, want %q", in, got, want)
		}
	}
}
