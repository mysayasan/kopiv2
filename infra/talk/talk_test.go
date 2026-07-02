package talk

import "testing"

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
