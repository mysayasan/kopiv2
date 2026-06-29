package services

import (
	"sync"
	"testing"

	"github.com/pion/rtp"
)

func TestParseRelayCameraID(t *testing.T) {
	cases := map[string]uint64{
		"camera-7":  7,
		"camera-42": 42,
		"camera-0":  0,
		"bogus":     0,
		"":          0,
	}
	for in, want := range cases {
		if got := parseRelayCameraID(in); got != want {
			t.Fatalf("parseRelayCameraID(%q) = %d, want %d", in, got, want)
		}
	}
}

// TestRelaySubCloseIsSafe verifies close is idempotent and that feed never writes after
// close (no panic on a closed channel) even under concurrent access — the node read
// loop and a Close() can race.
func TestRelaySubCloseIsSafe(t *testing.T) {
	s := &relaySub{
		packets: make(chan *rtp.Packet, 4),
		audio:   make(chan *rtp.Packet, 4),
		done:    make(chan struct{}),
	}
	raw, _ := (&rtp.Packet{Header: rtp.Header{Version: 2}}).Marshal()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			s.feed(s.packets, raw)
		}
	}()
	s.close()
	s.close() // idempotent
	wg.Wait()

	// Channels are closed: a drain terminates rather than blocking.
	for range s.packets { //nolint:revive
	}
	for range s.audio { //nolint:revive
	}
}
