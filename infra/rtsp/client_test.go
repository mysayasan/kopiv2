package rtsp

import "testing"

func TestProbeRejectsNonRTSPURL(t *testing.T) {
	_, err := NewClient().Probe(t.Context(), "http://example.com/stream", OpenOptions{})
	if err == nil {
		t.Fatal("expected unsupported scheme error")
	}
}
