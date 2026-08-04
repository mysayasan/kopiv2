package main

import (
	"bufio"
	"io"
	"log"
	"sync"
	"time"

	"github.com/mysayasan/kopiv2/infra/access/osdp"
)

// Bus is a simulated RS-485 multi-drop segment: several PDs that all hear every byte, one of which
// (usually) answers.
//
// Unlike tools/sunspec-sim, this tool DOES import infra — infra/access/osdp. That is deliberate and
// is the point of the simulator: it shares pd.go with the driver, so the simulator and the
// production PD-side decoder cannot drift apart. A simulator with its own private frame parser is
// one that agrees with itself and disagrees with the wire.
type Bus struct {
	mu      sync.Mutex
	pds     []*osdp.PD
	verbose bool
	log     *log.Logger
}

func NewBus(lg *log.Logger, verbose bool, pds ...*osdp.PD) *Bus {
	return &Bus{pds: pds, verbose: verbose, log: lg}
}

// With runs fn holding the bus lock, so a scripted fault or a card presentation cannot race the
// serve loop. Every mutation of a PD from outside Serve must go through this.
func (b *Bus) With(fn func(pds []*osdp.PD)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	fn(b.pds)
}

// Serve reads frames from conn, hands each to every PD, and writes back whatever answers.
//
// It returns when the connection closes. A malformed or undecodable token is handed to the PDs
// anyway — they are required to ignore what they cannot decode, and letting the junk through is how
// that requirement stays tested rather than assumed.
func (b *Bus) Serve(conn io.ReadWriter) error {
	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 0, 4096), osdp.MaxFrameSize*4)
	sc.Split(osdp.ScanFrames)

	for sc.Scan() {
		// sc.Bytes() is only valid until the next Scan; the PDs keep no reference, but the verbose
		// log and the collision model both outlive the call.
		req := append([]byte(nil), sc.Bytes()...)
		b.logFrame("CP->PD", req)

		var replies [][]byte
		var delay time.Duration
		b.mu.Lock()
		for _, pd := range b.pds {
			if out := pd.Handle(req); out != nil {
				replies = append(replies, out)
				if pd.Faults.ReplyDelay > delay {
					delay = pd.Faults.ReplyDelay
				}
			}
		}
		b.mu.Unlock()

		if len(replies) == 0 {
			continue
		}
		if delay > 0 {
			// A slow PD must not starve the rest of the bus. Sleeping here rather than inside PD
			// keeps PD I/O-free and pure.
			time.Sleep(delay)
		}

		out := replies[0]
		if len(replies) > 1 {
			out = collide(replies)
			b.logf("!! %d PDs answered at once — bus collision", len(replies))
		}
		b.logFrame("PD->CP", out)
		if _, err := conn.Write(out); err != nil {
			return err
		}
	}
	return sc.Err()
}

// collide models what happens when two PDs transmit simultaneously on one pair — the out-of-box
// "two readers both at address 0" case, which is a real onboarding step and not a hypothetical.
//
// Physically, two differential drivers fighting leave the receiver seeing an indeterminate level.
// The subtlety, and the reason this is not a plain byte-wise AND: two brand-new readers are
// IDENTICAL, so they answer with identical bytes, and AND-ing identical frames returns that frame
// unchanged — the CP would decode a perfectly clean reply and happily bind a door to one of two
// readers it cannot tell apart. That is the exact case the scenario exists to catch, and a naive
// model silently passes it.
//
// Real transmitters are never bit-synchronised: they start microseconds apart and drift. Applying a
// one-byte skew to every frame after the first reproduces that, and corrupts identical frames as
// readily as differing ones. The leading SOM survives (the first byte has nothing to collide with
// yet), so the CP still detects a frame start and must fail on the CRC — the harder and more
// realistic path than a framing miss.
func collide(frames [][]byte) []byte {
	n := 0
	for _, f := range frames {
		if len(f) > n {
			n = len(f)
		}
	}
	out := make([]byte, n)
	for i := range out {
		b := byte(0xFF)
		for j, f := range frames {
			// Frame j starts j bytes late, so at wire position i it is emitting its byte i-j.
			k := i - j
			if k >= 0 && k < len(f) {
				b &= f[k]
			}
		}
		out[i] = b
	}
	return out
}

func (b *Bus) logf(format string, args ...any) {
	if b.verbose && b.log != nil {
		b.log.Printf(format, args...)
	}
}

func (b *Bus) logFrame(dir string, raw []byte) {
	if !b.verbose || b.log == nil {
		return
	}
	f, err := osdp.Unmarshal(raw)
	if err != nil {
		b.log.Printf("%s  [undecodable: %v] % x", dir, err, raw)
		return
	}
	code := osdp.Command(f.Code).String()
	if f.Reply {
		code = osdp.Reply(f.Code).String()
	}
	b.log.Printf("%s  addr=%-3d seq=%d %-7s data=% x", dir, f.Address, f.Sequence, code, f.Data)
}
