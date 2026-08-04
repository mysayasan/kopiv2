package main

import (
	"bufio"
	"bytes"
	"io"
	"net"
	"testing"
	"time"

	"github.com/mysayasan/kopiv2/infra/access/osdp"
)

// cpConn is the CP half of a simulated bus: it writes commands and reads framed replies, which is
// exactly what infra/access/osdp's cp.go will do in build-order step 3. Writing it here first means
// the simulator is exercised over a real io.ReadWriter — not just unit-tested in memory — before
// anything depends on it.
type cpConn struct {
	t  *testing.T
	rw io.ReadWriter
	sc *bufio.Scanner
	// seq is PER PD ADDRESS, not per bus. This is the §3.1 trap and it is not theoretical: the
	// first version of this harness kept one counter for the whole bus, so polling PD 2 after PD 1
	// reused a number PD 2 had already seen. The PD correctly read that as a retransmission and
	// replayed its previous reply — swallowing a queued card read. On real hardware that presents
	// as a reader that intermittently "misses" badges, with nothing wrong on the bus.
	seq map[uint8]uint8
}

func newCP(t *testing.T, rw io.ReadWriter) *cpConn {
	t.Helper()
	sc := bufio.NewScanner(rw)
	sc.Buffer(make([]byte, 0, 4096), osdp.MaxFrameSize*4)
	sc.Split(osdp.ScanFrames)
	return &cpConn{t: t, rw: rw, sc: sc, seq: map[uint8]uint8{}}
}

// send transmits a command on that PD's next sequence number and returns the raw reply, or nil if
// the bus stayed silent for the deadline.
func (c *cpConn) send(addr uint8, code osdp.Command, data ...byte) []byte {
	c.t.Helper()
	c.seq[addr] = osdp.NextSequence(c.seq[addr])
	raw, err := (&osdp.Frame{Address: addr, Sequence: c.seq[addr], Code: byte(code), Data: data}).Marshal()
	if err != nil {
		c.t.Fatalf("marshal %s: %v", code, err)
	}
	if _, err := c.rw.Write(raw); err != nil {
		c.t.Fatalf("write %s: %v", code, err)
	}
	if conn, ok := c.rw.(net.Conn); ok {
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	}
	if !c.sc.Scan() {
		return nil
	}
	return append([]byte(nil), c.sc.Bytes()...)
}

// expect sends a command and decodes the reply, failing if there is none.
func (c *cpConn) expect(addr uint8, code osdp.Command, data ...byte) *osdp.Frame {
	c.t.Helper()
	raw := c.send(addr, code, data...)
	if raw == nil {
		c.t.Fatalf("%s: no reply from the bus", code)
	}
	f, err := osdp.Unmarshal(raw)
	if err != nil {
		c.t.Fatalf("%s: undecodable reply % x: %v", code, raw, err)
	}
	return f
}

// serveBus runs a Bus over an in-process socket pair and returns the CP's end.
func serveBus(t *testing.T, b *Bus) *cpConn {
	t.Helper()
	cp, pd := net.Pipe()
	t.Cleanup(func() { cp.Close(); pd.Close() })
	go func() {
		defer pd.Close()
		_ = b.Serve(pd)
	}()
	return newCP(t, cp)
}

// TestBusServesHealthyReader is the step-2 acceptance test: a PD on a real connection answering
// POLL, ID and CAP, and handing over a card on a poll.
func TestBusServesHealthyReader(t *testing.T) {
	pd := osdp.NewPD(1)
	b := NewBus(nil, false, pd)
	cp := serveBus(t, b)

	if f := cp.expect(1, osdp.CmdPoll); osdp.Reply(f.Code) != osdp.RplAck {
		t.Errorf("POLL = %s, want ACK", f.ReplyCode())
	}
	if f := cp.expect(1, osdp.CmdID, 0x00); osdp.Reply(f.Code) != osdp.RplPDID {
		t.Errorf("ID = %s, want PDID", f.ReplyCode())
	}
	if f := cp.expect(1, osdp.CmdCap, 0x00); osdp.Reply(f.Code) != osdp.RplPDCap {
		t.Errorf("CAP = %s, want PDCAP", f.ReplyCode())
	}

	card := []byte{0xDE, 0xAD, 0xBE, 0xEF}
	b.With(func(pds []*osdp.PD) {
		pds[0].PresentCard(osdp.CardRead{Format: 1, BitCount: 26, Data: card})
	})
	f := cp.expect(1, osdp.CmdPoll)
	if osdp.Reply(f.Code) != osdp.RplRaw {
		t.Fatalf("poll after a badge = %s, want RAW", f.ReplyCode())
	}
	if !bytes.Equal(f.Data[4:], card) {
		t.Errorf("card data = % x, want % x", f.Data[4:], card)
	}
}

// TestBusMultidrop proves per-PD state on a shared segment: three readers, each answering only for
// itself, and a card at one not appearing at another.
func TestBusMultidrop(t *testing.T) {
	b := NewBus(nil, false, osdp.NewPD(1), osdp.NewPD(2), osdp.NewPD(3))
	cp := serveBus(t, b)

	for _, addr := range []uint8{1, 2, 3} {
		if f := cp.expect(addr, osdp.CmdPoll); f.Address != addr {
			t.Errorf("poll to %d answered by %d", addr, f.Address)
		}
	}

	b.With(func(pds []*osdp.PD) {
		pds[1].PresentCard(osdp.CardRead{Format: 1, BitCount: 26, Data: []byte{0xAA}})
	})
	if f := cp.expect(1, osdp.CmdPoll); osdp.Reply(f.Code) != osdp.RplAck {
		t.Errorf("PD 1 delivered PD 2's card: %s", f.ReplyCode())
	}
	if f := cp.expect(2, osdp.CmdPoll); osdp.Reply(f.Code) != osdp.RplRaw {
		t.Errorf("PD 2 = %s, want RAW", f.ReplyCode())
	}
}

// TestBusSilentPDTimesOut covers the offline-supervision input: the CP hears nothing at all, and
// must not block forever waiting.
func TestBusSilentPDTimesOut(t *testing.T) {
	pd := osdp.NewPD(1)
	pd.Faults.Silent = true
	cp := serveBus(t, NewBus(nil, false, pd))

	done := make(chan []byte, 1)
	go func() { done <- cp.send(1, osdp.CmdPoll) }()
	select {
	case raw := <-done:
		if raw != nil {
			t.Errorf("silent PD answered with % x", raw)
		}
	case <-time.After(500 * time.Millisecond):
		// Expected: nothing comes back. The CP's own supervision timer is what recovers this, and
		// that is step 3's job.
	}
}

// TestBusAddressCollision is the out-of-box onboarding case: two brand-new readers, both at the
// factory address 0, answering the same poll. The CP must see corruption rather than silently
// accepting one of them — accepting one would bind a door to whichever reader won the race.
// The two PDs here are deliberately IDENTICAL, because that is what two boxes off the same pallet
// actually are. An earlier version of this test gave them different serials, which let a byte-wise
// collision model look correct while the real scenario — identical readers — produced a cleanly
// decodable reply. Booting the binary caught what the differentiated test could not.
func TestBusAddressCollision(t *testing.T) {
	cp := serveBus(t, NewBus(nil, false, osdp.NewPD(0), osdp.NewPD(0)))

	// The requirement is narrow and absolute: the CP must never cleanly decode a reply when two
	// PDs answer, because doing so binds a door to whichever reader won a race the operator cannot
	// see. HOW it fails is modelling taste — a byte-skewed collision usually mangles the length
	// field too, so the frame layer yields nothing at all rather than a CRC failure. Both outcomes
	// satisfy the requirement, so both are accepted here.
	//
	// Worth carrying into cp.go (step 3): at the frame layer this is indistinguishable from an
	// EMPTY address, yet onboarding needs to tell "two readers fighting" from "no reader here".
	// That needs a bytes-arrived-but-never-framed signal, which only the transport can see.
	raw := cp.send(0, osdp.CmdID, 0x00)
	if raw == nil {
		return // collision destroyed framing outright; the CP sees a timeout
	}
	if f, err := osdp.Unmarshal(raw); err == nil {
		t.Errorf("two PDs at address 0 produced a cleanly decodable %s — the collision was not modelled",
			f.ReplyCode())
	}
}

// TestCollideCorrupts pins the collision model itself. The identical-frames case is listed first
// because it is both the realistic one (two readers off the same pallet) and the one a byte-wise
// AND silently passes through unchanged.
func TestCollideCorrupts(t *testing.T) {
	ack, _ := (&osdp.Frame{Address: 0, Reply: true, Sequence: 1, Code: byte(osdp.RplAck)}).Marshal()
	busy, _ := (&osdp.Frame{Address: 0, Reply: true, Sequence: 1, Code: byte(osdp.RplBusy)}).Marshal()
	long, _ := (&osdp.Frame{Address: 0, Reply: true, Sequence: 1, Code: byte(osdp.RplPDID),
		Data: bytes.Repeat([]byte{0x11}, 12)}).Marshal()

	cases := []struct {
		name   string
		frames [][]byte
	}{
		{"two identical readers", [][]byte{ack, append([]byte(nil), ack...)}},
		{"two different replies", [][]byte{ack, busy}},
		{"different lengths", [][]byte{ack, long}},
		{"three at once", [][]byte{ack, busy, long}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := collide(c.frames)
			// The leading SOM survives, so the CP detects a frame start and must fail on the CRC —
			// the harder, more realistic path than a framing miss.
			if out[0] != osdp.SOM {
				t.Errorf("collision destroyed the SOM (%#02x); the CRC path would not be exercised", out[0])
			}
			if f, err := osdp.Unmarshal(out); err == nil {
				t.Errorf("collided frames decoded cleanly as %s", f.ReplyCode())
			}
		})
	}
}

// TestBusIgnoresNoise feeds the bus raw junk on the wire. No PD may answer it, and the connection
// must survive so the next real command still works — one noise burst cannot take a segment down.
func TestBusIgnoresNoise(t *testing.T) {
	cp := serveBus(t, NewBus(nil, false, osdp.NewPD(1)))

	if _, err := cp.rw.Write(bytes.Repeat([]byte{0x53, 0x00, 0xFF, 0xAA}, 40)); err != nil {
		t.Fatalf("write noise: %v", err)
	}
	if f := cp.expect(1, osdp.CmdPoll); osdp.Reply(f.Code) != osdp.RplAck {
		t.Errorf("after a noise burst, POLL = %s, want ACK", f.ReplyCode())
	}
}

// TestBusSlowPDDoesNotStarveOthers covers the last row of the §4.1 table: a PD replying near the
// timeout must not stop its neighbour being polled.
func TestBusSlowPDDoesNotStarveOthers(t *testing.T) {
	slow := osdp.NewPD(1)
	slow.Faults.ReplyDelay = 150 * time.Millisecond
	cp := serveBus(t, NewBus(nil, false, slow, osdp.NewPD(2)))

	start := time.Now()
	cp.expect(1, osdp.CmdPoll)
	slowElapsed := time.Since(start)

	start = time.Now()
	cp.expect(2, osdp.CmdPoll)
	fastElapsed := time.Since(start)

	if slowElapsed < 150*time.Millisecond {
		t.Errorf("slow PD replied in %v, want at least its 150ms delay", slowElapsed)
	}
	if fastElapsed > 100*time.Millisecond {
		t.Errorf("healthy PD took %v — the slow PD starved it", fastElapsed)
	}
}
