package osdp

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// harness wires a real Bus to real PDs over an in-memory connection, so the CP is exercised against
// the same PD implementation the simulator ships.
type harness struct {
	t      *testing.T
	bus    *Bus
	mu     sync.Mutex
	pds    []*PD
	cancel context.CancelFunc
	done   chan struct{}
}

// fastOpts keeps the tests quick without changing the logic under test: the same slot/timeout
// relationship as production, two orders of magnitude smaller.
func fastOpts() Options {
	return Options{
		SlotInterval:   2 * time.Millisecond,
		ReplyTimeout:   40 * time.Millisecond,
		OfflineAfter:   3,
		StatusInterval: 10 * time.Millisecond,
	}
}

func newHarness(t *testing.T, opts Options, pds ...*PD) *harness {
	t.Helper()
	cfgs := make([]PDConfig, len(pds))
	for i, pd := range pds {
		cfgs[i] = PDConfig{Address: pd.Address}
	}
	return buildHarness(t, opts, cfgs, pds)
}

// newSecureHarness is the same wiring with an explicit Secure Channel policy per reader.
func newSecureHarness(t *testing.T, opts Options, cfg PDConfig, pds ...*PD) *harness {
	t.Helper()
	return buildHarness(t, opts, []PDConfig{cfg}, pds)
}

func buildHarness(t *testing.T, opts Options, cfgs []PDConfig, pds []*PD) *harness {
	t.Helper()
	cpEnd, pdEnd := net.Pipe()

	h := &harness{t: t, pds: pds, done: make(chan struct{})}
	h.bus = NewBus(cpEnd, opts)
	for _, cfg := range cfgs {
		if err := h.bus.AddPD(cfg); err != nil {
			t.Fatalf("AddPD(%d): %v", cfg.Address, err)
		}
	}

	// The PD side of the wire: decode a frame, offer it to every reader, transmit whatever answers.
	go func() {
		sc := bufio.NewScanner(pdEnd)
		sc.Buffer(make([]byte, 0, 4096), MaxFrameSize*4)
		sc.Split(ScanFrames)
		for sc.Scan() {
			req := append([]byte(nil), sc.Bytes()...)
			h.mu.Lock()
			var out []byte
			var delay time.Duration
			for _, pd := range h.pds {
				if o := pd.Handle(req); o != nil {
					out, delay = o, pd.Faults.ReplyDelay
					break
				}
			}
			h.mu.Unlock()
			if out == nil {
				continue
			}
			if delay > 0 {
				time.Sleep(delay)
			}
			if _, err := pdEnd.Write(out); err != nil {
				return
			}
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	h.cancel = cancel
	go func() {
		defer close(h.done)
		_ = h.bus.Run(ctx)
	}()

	t.Cleanup(func() {
		cancel()
		cpEnd.Close()
		pdEnd.Close()
		select {
		case <-h.done:
		case <-time.After(2 * time.Second):
			t.Error("Bus.Run did not return after cancellation")
		}
	})
	return h
}

// with mutates a PD under the same lock the serve goroutine holds.
func (h *harness) with(fn func(pds []*PD)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	fn(h.pds)
}

// await waits for the first event matching pred.
func (h *harness) await(timeout time.Duration, what string, pred func(Event) bool) Event {
	h.t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case ev, ok := <-h.bus.Events():
			if !ok {
				h.t.Fatalf("waiting for %s: event channel closed", what)
			}
			if pred(ev) {
				return ev
			}
		case <-deadline:
			h.t.Fatalf("timed out waiting for %s", what)
		}
	}
}

func (h *harness) awaitKind(timeout time.Duration, addr uint8, kind EventKind) Event {
	h.t.Helper()
	return h.await(timeout, kind.String(), func(e Event) bool {
		return e.Kind == kind && e.Address == addr
	})
}

// TestBusBringsReaderOnline covers the identification path: a reader is not usable until PDID and
// PDCAP have both been read, because the trust rules are enforced against its capabilities.
func TestBusBringsReaderOnline(t *testing.T) {
	pd := NewPD(1)
	h := newHarness(t, fastOpts(), pd)

	ev := h.awaitKind(2*time.Second, 1, EventOnline)
	if ev.Info.Serial != pd.Info.Serial {
		t.Errorf("online serial = %#08x, want %#08x", ev.Info.Serial, pd.Info.Serial)
	}
	if len(ev.Caps) == 0 {
		t.Error("online event carried no capabilities")
	}
	if !ev.SupportsSecureChannel {
		t.Error("a healthy reader should report Secure Channel support")
	}
	if ev.DefaultKey {
		t.Error("a rekeyed reader must not report the default base key")
	}
	if got := h.bus.Status(1); got != StatusOnline {
		t.Errorf("status = %s, want online", got)
	}
}

// TestBusReportsDefaultKey is the trust-cap input: a reader still on SCBK-D must be discoverable
// from PDCAP alone, with no crypto, so the hardware plan's `interior`-only cap can be applied
// before any Secure Channel is attempted.
func TestBusReportsDefaultKey(t *testing.T) {
	pd := NewPD(1)
	pd.Faults.DefaultSCBK = true
	h := newHarness(t, fastOpts(), pd)

	ev := h.awaitKind(2*time.Second, 1, EventOnline)
	if !ev.DefaultKey {
		t.Error("reader on SCBK-D did not report DefaultKey — it would be treated as secure")
	}
	if !ev.SupportsSecureChannel {
		t.Error("SCBK-D reader still supports SC; only the KEY is wrong")
	}
}

// TestBusDeliversCard proves a badge reaches the application, and that it arrives on the event
// channel rather than as the return value of anything.
func TestBusDeliversCard(t *testing.T) {
	pd := NewPD(1)
	h := newHarness(t, fastOpts(), pd)
	h.awaitKind(2*time.Second, 1, EventOnline)

	want := []byte{0xDE, 0xAD, 0xBE, 0xEF}
	h.with(func(pds []*PD) {
		pds[0].PresentCard(CardRead{Format: 1, BitCount: 26, Data: want})
	})

	ev := h.awaitKind(2*time.Second, 1, EventCard)
	if !bytes.Equal(ev.Card.Data, want) {
		t.Errorf("card = % x, want % x", ev.Card.Data, want)
	}
	if ev.Card.BitCount != 26 {
		t.Errorf("bit count = %d, want 26", ev.Card.BitCount)
	}
}

// TestBusPerPDSequenceNumbers is the regression test for the bug this driver already produced once.
//
// Sequence numbers are per PD, not per bus. A CP with one shared counter reuses a number a reader
// has already seen; the reader correctly treats that as a retransmission and REPLAYS its previous
// reply, silently swallowing the queued card read. On site that presents as a reader intermittently
// missing badges, with nothing wrong on the bus. Three readers, a card at each, all three must
// arrive.
func TestBusPerPDSequenceNumbers(t *testing.T) {
	pds := []*PD{NewPD(1), NewPD(2), NewPD(3)}
	h := newHarness(t, fastOpts(), pds...)
	for _, pd := range pds {
		h.awaitKind(3*time.Second, pd.Address, EventOnline)
	}

	cards := map[uint8][]byte{1: {0x11, 0x11}, 2: {0x22, 0x22}, 3: {0x33, 0x33}}
	h.with(func(list []*PD) {
		for _, pd := range list {
			pd.PresentCard(CardRead{Format: 1, BitCount: 26, Data: cards[pd.Address]})
		}
	})

	seen := map[uint8][]byte{}
	for len(seen) < 3 {
		ev := h.await(3*time.Second, "card reads from all three readers", func(e Event) bool {
			return e.Kind == EventCard
		})
		seen[ev.Address] = ev.Card.Data
	}
	for addr, want := range cards {
		if got := seen[addr]; !bytes.Equal(got, want) {
			t.Errorf("reader %d delivered % x, want % x", addr, got, want)
		}
	}
}

// TestBusDetectsOfflineAndRecovers covers supervision in both directions. Going offline must take
// more than one dropped frame — a noisy segment drops frames routinely and must not flap a door
// out of service — and coming back must re-identify rather than assume the reader is unchanged.
func TestBusDetectsOfflineAndRecovers(t *testing.T) {
	pd := NewPD(1)
	h := newHarness(t, fastOpts(), pd)
	h.awaitKind(2*time.Second, 1, EventOnline)

	h.with(func(pds []*PD) { pds[0].Faults.Silent = true })
	ev := h.awaitKind(3*time.Second, 1, EventOffline)
	if ev.Reason == "" {
		t.Error("offline event carried no reason")
	}
	if got := h.bus.Status(1); got != StatusOffline {
		t.Errorf("status = %s, want offline", got)
	}

	h.with(func(pds []*PD) { pds[0].Faults.Silent = false })
	h.awaitKind(3*time.Second, 1, EventOnline)
	if got := h.bus.Status(1); got != StatusOnline {
		t.Errorf("status after recovery = %s, want online", got)
	}

	if st := h.bus.Stats()[1]; st.Offlines != 1 {
		t.Errorf("Offlines = %d, want 1", st.Offlines)
	}
}

// TestBusOneReaderDownDoesNotTakeTheBus is the multi-drop supervision case: a dead reader must not
// stop its neighbours being polled, or one failed door takes a building with it.
func TestBusOneReaderDownDoesNotTakeTheBus(t *testing.T) {
	pds := []*PD{NewPD(1), NewPD(2)}
	h := newHarness(t, fastOpts(), pds...)
	h.awaitKind(3*time.Second, 1, EventOnline)
	h.awaitKind(3*time.Second, 2, EventOnline)

	h.with(func(list []*PD) { list[0].Faults.Silent = true })
	h.awaitKind(3*time.Second, 1, EventOffline)

	// Reader 2 must still be serving badges.
	h.with(func(list []*PD) {
		list[1].PresentCard(CardRead{Format: 1, BitCount: 26, Data: []byte{0xAB}})
	})
	ev := h.awaitKind(3*time.Second, 2, EventCard)
	if !bytes.Equal(ev.Card.Data, []byte{0xAB}) {
		t.Errorf("healthy reader delivered % x", ev.Card.Data)
	}
	if got := h.bus.Status(2); got != StatusOnline {
		t.Errorf("healthy reader status = %s, want online", got)
	}
}

// TestBusBusyIsRetriedNotFatal pins the BUSY semantics: "ask me again", not "I am broken". A CP
// that counts BUSY as a failure declares healthy readers dead under load.
func TestBusBusyIsRetriedNotFatal(t *testing.T) {
	pd := NewPD(1)
	pd.Faults.Busy = 2
	h := newHarness(t, fastOpts(), pd)

	h.awaitKind(3*time.Second, 1, EventOnline)
	if st := h.bus.Stats()[1]; st.Busy == 0 {
		t.Error("BUSY replies were not counted")
	}
	if st := h.bus.Stats()[1]; st.Offlines != 0 {
		t.Errorf("a BUSY reader was declared offline %d time(s)", st.Offlines)
	}
}

// TestBusRecoversFromSequenceSkew covers the §3.1 trap from the CP side: a reader replying with the
// wrong sequence number is present and talking, so the recovery is a session reset, not an offline
// declaration.
func TestBusRecoversFromSequenceSkew(t *testing.T) {
	pd := NewPD(1)
	pd.Faults.SequenceSkew = 1
	h := newHarness(t, fastOpts(), pd)

	ev := h.await(3*time.Second, "a sequence fault", func(e Event) bool {
		return e.Kind == EventFault && strings.Contains(e.Reason, "sequence")
	})
	if !strings.Contains(ev.Reason, "resetting") {
		t.Errorf("fault reason %q does not mention the recovery action", ev.Reason)
	}
	// A sequence skew is a CABLING or firmware fault, and it must be classified as one. Before
	// Event.Fault existed, mypintusan had no way to tell it from a failed handshake and titled it
	// "Reader secure channel fault" — the right alarm under a headline that sends the installer
	// looking for a bus tap.
	if ev.Fault != FaultProtocol {
		t.Errorf("sequence fault classified as %s, want protocol", ev.Fault)
	}
	if st := h.bus.Stats()[1]; st.SequenceErrs == 0 {
		t.Error("sequence errors were not counted")
	}

	// Clear the fault; the reader must come online without operator intervention.
	h.with(func(pds []*PD) { pds[0].Faults.SequenceSkew = 0 })
	h.awaitKind(3*time.Second, 1, EventOnline)
}

// TestBusPermanentlyFaultyReaderGoesOffline closes the limbo hole that live-running the driver
// against the simulator exposed and that every in-process test had missed.
//
// A reader that answers every poll with the wrong sequence number is ALIVE. The failure counter
// only ever tracked silence, so it kept being reset, and the reader was never declared offline —
// it just emitted the same fault forever while a door sat bound to it, unusable and unalarmed.
// Both halves are asserted here: it must go offline, and it must not flood the event stream.
func TestBusPermanentlyFaultyReaderGoesOffline(t *testing.T) {
	opts := fastOpts()
	opts.SupervisionTimeout = 300 * time.Millisecond

	pd := NewPD(1)
	pd.Faults.SequenceSkew = 1
	h := newHarness(t, opts, pd)

	faults := 0
	deadline := time.After(3 * time.Second)
	for {
		var ev Event
		select {
		case e, ok := <-h.bus.Events():
			if !ok {
				t.Fatal("event channel closed before the reader was declared offline")
			}
			ev = e
		case <-deadline:
			t.Fatalf("a permanently out-of-sequence reader was never declared offline "+
				"(%d faults emitted, still %s)", faults, h.bus.Status(1))
		}

		if ev.Kind == EventFault {
			faults++
			// One fault per failure episode. An unbounded stream of identical faults would fill an
			// audit log and a UI with thousands of rows a minute and bury everything else.
			if faults > 4 {
				t.Fatalf("event flood: %d identical faults before any offline declaration", faults)
			}
			continue
		}
		if ev.Kind == EventOffline && ev.Address == 1 {
			if !strings.Contains(ev.Reason, "unusable") {
				t.Errorf("offline reason %q does not say the reader is present but unusable", ev.Reason)
			}
			return
		}
	}
}

// TestBusNakedOperatorCommandDoesNotOfflineReader is the other half of that fix. A refused operator
// command must fail the CALLER without knocking a perfectly healthy reader out of service.
//
// KEYSET on a cleartext session is the example used here because it is refused for a good reason
// rather than an incidental one: installing a base key outside an established session would put the
// site key on the wire in plaintext.
func TestBusNakedOperatorCommandDoesNotOfflineReader(t *testing.T) {
	pd := NewPD(1)
	h := newHarness(t, fastOpts(), pd)
	h.awaitKind(2*time.Second, 1, EventOnline)

	keyset := append([]byte{0x01, 0x10}, bytes.Repeat([]byte{0x5A}, 16)...)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for range 5 {
		if _, err := h.bus.Send(ctx, 1, CmdKeySet, keyset...); err == nil {
			t.Fatal("KEYSET succeeded on a cleartext session — the site key would go out in plaintext")
		}
	}
	if got := h.bus.Status(1); got != StatusOnline {
		t.Errorf("status = %s after refused operator commands, want online", got)
	}
	if st := h.bus.Stats()[1]; st.Offlines != 0 {
		t.Errorf("refused operator commands took the reader offline %d time(s)", st.Offlines)
	}
}

// TestBusDiagnosesUnframedBytes covers the address-collision signal. Two readers at one address
// corrupt each other, and the wreckage almost never survives framing — which at the frame layer is
// identical to an empty address. Counting bytes that arrived but never framed is the only thing
// that tells "two readers fighting" from "no reader here", and onboarding must distinguish them.
func TestBusDiagnosesUnframedBytes(t *testing.T) {
	pd := NewPD(1)
	pd.Faults.Garbage = true
	h := newHarness(t, fastOpts(), pd)

	ev := h.awaitKind(3*time.Second, 1, EventOffline)
	if !strings.Contains(ev.Reason, "no frame decoded") && !strings.Contains(ev.Reason, "failed CRC") {
		t.Errorf("offline reason %q reports neither unframed bytes nor CRC failures — an operator "+
			"cannot tell a colliding pair from an empty address", ev.Reason)
	}
	if st := h.bus.Stats()[1]; st.UnframedBytes == 0 {
		t.Error("unframed bytes were not counted")
	}
}

// TestBusDistinguishesCorruptFromAbsent pins the three-way diagnosis. "Nothing on the wire",
// "wreckage on the wire" and "well-formed frames failing CRC" look identical from a distance and
// send an installer to three different places; conflating them costs a site visit.
func TestBusDistinguishesCorruptFromAbsent(t *testing.T) {
	cases := []struct {
		name    string
		faults  Faults
		wantSub string
	}{
		{"absent reader", Faults{Silent: true}, "no reply"},
		{"frames failing CRC", Faults{BadCRC: true}, "failed CRC"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pd := NewPD(1)
			pd.Faults = c.faults
			h := newHarness(t, fastOpts(), pd)

			ev := h.awaitKind(3*time.Second, 1, EventOffline)
			if !strings.Contains(ev.Reason, c.wantSub) {
				t.Errorf("offline reason = %q, want it to mention %q", ev.Reason, c.wantSub)
			}
		})
	}
}

// TestBusOutputFiresStrike covers the actuation primitive — the call that energises a door strike.
func TestBusOutputFiresStrike(t *testing.T) {
	pd := NewPD(1)
	h := newHarness(t, fastOpts(), pd)
	h.awaitKind(2*time.Second, 1, EventOnline)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := h.bus.Output(ctx, 1, 0, true, 5*time.Second); err != nil {
		t.Fatalf("Output: %v", err)
	}
	h.with(func(pds []*PD) {
		if !pds[0].Outputs[0] {
			t.Error("strike was not energised")
		}
	})

	if err := h.bus.Output(ctx, 1, 0, false, 0); err != nil {
		t.Fatalf("Output off: %v", err)
	}
	h.with(func(pds []*PD) {
		if pds[0].Outputs[0] {
			t.Error("strike was not de-energised")
		}
	})
}

// TestBusSendRejectsUnknownPD guards the operator-data path: a door bound to an address that is not
// on this bus must fail loudly rather than block.
func TestBusSendRejectsUnknownPD(t *testing.T) {
	h := newHarness(t, fastOpts(), NewPD(1))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if _, err := h.bus.Send(ctx, 9, CmdPoll); err == nil {
		t.Fatal("Send to an unknown PD succeeded")
	}
}

// TestBusTamperSurfaces covers the alarm path: tamper is read from LSTATR and published.
func TestBusTamperSurfaces(t *testing.T) {
	pd := NewPD(1)
	h := newHarness(t, fastOpts(), pd)
	h.awaitKind(2*time.Second, 1, EventOnline)

	h.with(func(pds []*PD) { pds[0].Faults.Tamper = true })
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := h.bus.Send(ctx, 1, CmdLStat); err != nil {
		t.Fatalf("LSTAT: %v", err)
	}
	ev := h.awaitKind(2*time.Second, 1, EventStatus)
	if !ev.Tamper {
		t.Error("tamper was not reported")
	}
}

// TestBusPollsStatusUnasked is the check TestBusTamperSurfaces above could not make.
//
// That test asserts the DECODE path — hand the bus an LSTAT and a tamper flag comes back out — and
// it passed for the entire life of this driver while nothing in the poll loop ever sent one. A PD
// answers POLL with an ACK or a queued card read and never volunteers a status change, so the CP
// was structurally blind to tamper: the alarm existed, was severity-mapped, titled and translated
// in four languages, and no site could have raised it. The difference between the two tests is the
// missing `bus.Send` here, and that absence is the whole point.
func TestBusPollsStatusUnasked(t *testing.T) {
	pd := NewPD(1)
	h := newHarness(t, fastOpts(), pd)
	h.awaitKind(2*time.Second, 1, EventOnline)

	h.with(func(pds []*PD) { pds[0].Faults.Tamper = true })
	ev := h.awaitKind(2*time.Second, 1, EventStatus)
	if !ev.Tamper {
		t.Error("tamper was not reported by the poll loop")
	}
}

// TestBusReportsDoorContactUnasked is the same claim for the door-position contact, which is what
// makes door-forced and door-held-open possible at all.
func TestBusReportsDoorContactUnasked(t *testing.T) {
	pd := NewPD(1)
	h := newHarness(t, fastOpts(), pd)
	h.awaitKind(2*time.Second, 1, EventOnline)

	h.with(func(pds []*PD) { pds[0].Inputs[0] = true })
	ev := h.await(2*time.Second, "the door contact opening", func(e Event) bool {
		return e.Kind == EventInput && e.Address == 1 && len(e.Inputs) > 0 && e.Inputs[0]
	})
	if len(ev.Inputs) != 1 {
		t.Errorf("expected one supervised input, got %d", len(ev.Inputs))
	}
}

// TestBusStatusIsEdgeTriggered pins the property that stops supervision becoming a flood.
//
// A reader whose case is open answers every LSTAT with the tamper bit set, so a level-triggered
// emitter would republish the alarm on every status interval for as long as the case stays open —
// hundreds an hour, all identical. alarm.go argues against paging on a flaky segment because "the
// failure mode worth avoiding is an alarm nobody believes"; this is the same argument with a much
// larger number.
func TestBusStatusIsEdgeTriggered(t *testing.T) {
	pd := NewPD(1)
	h := newHarness(t, fastOpts(), pd)
	h.awaitKind(2*time.Second, 1, EventOnline)

	h.with(func(pds []*PD) { pds[0].Faults.Tamper = true })
	h.awaitKind(2*time.Second, 1, EventStatus)

	// Well over a hundred status intervals. A level-triggered emitter would have produced dozens
	// of repeats by now.
	deadline := time.After(300 * time.Millisecond)
	repeats := 0
	for done := false; !done; {
		select {
		case ev, ok := <-h.bus.Events():
			if !ok {
				done = true
				break
			}
			if ev.Kind == EventStatus {
				repeats++
			}
		case <-deadline:
			done = true
		}
	}
	if repeats > 0 {
		t.Errorf("an unchanged tamper was republished %d times", repeats)
	}
}

// TestBusStatusNeverStarvesCardDelivery pins the bound that stops supervision eating the bus.
//
// A card read is queued at the PD and handed over on a POLL, so every slot spent asking for status
// is a slot the card waits. That is a rounding error on a healthy bus and a catastrophe on a sick
// one: each dead reader costs a full reply timeout, so a degraded segment can take longer to go
// round than the status interval — and a scheduler gated only on elapsed time then sends a status
// command EVERY time, forever, and no badge ever reaches the controller. The bus stays "up",
// supervision stays "healthy", and nobody can get into the building.
//
// The status interval here is deliberately shorter than a single round can possibly be.
func TestBusStatusNeverStarvesCardDelivery(t *testing.T) {
	opts := fastOpts()
	opts.StatusInterval = time.Nanosecond // always due, on every slot
	dead, healthy := NewPD(1), NewPD(2)
	dead.Faults.Silent = true
	h := newHarness(t, opts, dead, healthy)
	h.awaitKind(3*time.Second, 2, EventOnline)

	h.with(func(list []*PD) {
		list[1].PresentCard(CardRead{Format: 1, BitCount: 26, Data: []byte{0xAB}})
	})
	ev := h.awaitKind(3*time.Second, 2, EventCard)
	if !bytes.Equal(ev.Card.Data, []byte{0xAB}) {
		t.Errorf("healthy reader delivered % x", ev.Card.Data)
	}
}

// TestBusSecureChannelRefusalIsSurfaced is the fail-closed check available before securechannel.go
// exists: a refused handshake must reach the application as a fault, never be silently ignored on
// the way to a cleartext session.
func TestBusSecureChannelRefusalIsSurfaced(t *testing.T) {
	pd := NewPD(1)
	pd.Faults.RefuseSecureChannel = true
	h := newHarness(t, fastOpts(), pd)
	h.awaitKind(2*time.Second, 1, EventOnline)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := h.bus.Send(ctx, 1, CmdChlng, bytes.Repeat([]byte{0xA5}, 8)...)
	if err == nil {
		t.Fatal("a refused Secure Channel handshake reported success")
	}
	h.await(2*time.Second, "a secure-channel fault", func(e Event) bool {
		return e.Kind == EventFault && strings.Contains(e.Reason, "CHLNG")
	})
}

// TestBusRunReturnsOnCancel proves the loop is cancellable and does not leak the reader goroutine.
func TestBusRunReturnsOnCancel(t *testing.T) {
	cpEnd, pdEnd := net.Pipe()
	defer cpEnd.Close()
	defer pdEnd.Close()
	go io.Copy(io.Discard, pdEnd) // absorb whatever the CP transmits

	bus := NewBus(cpEnd, fastOpts())
	_ = bus.Add(1)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- bus.Run(ctx) }()

	time.Sleep(30 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Error("Run returned nil after cancellation, want a context error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after cancellation")
	}
}

// TestBusRunReturnsWhenThePortDies is the regression test for a production outage found by booting
// the app rather than by any unit test.
//
// A dead transport is NOT a dead reader. Before this, a CP whose port went away — a USB-RS485
// adapter unplugged, a serial-to-Ethernet gateway rebooting — kept polling a wire nobody was
// listening to, marked every reader offline, and stayed that way until the process was restarted.
// Every door on the segment was dead, silently, and the only clue was "reader offline".
//
// Run must RETURN so its owner can re-dial. The tests all used an in-memory pipe that was never
// torn down mid-run, which is exactly why none of them saw it.
func TestBusRunReturnsWhenThePortDies(t *testing.T) {
	t.Run("peer closes the connection", func(t *testing.T) {
		cpEnd, pdEnd := net.Pipe()
		bus := NewBus(cpEnd, fastOpts())
		_ = bus.Add(1)

		go io.Copy(io.Discard, pdEnd) // absorb polls, answer nothing

		done := make(chan error, 1)
		go func() { done <- bus.Run(context.Background()) }()

		time.Sleep(20 * time.Millisecond)
		pdEnd.Close() // the far end goes away

		select {
		case err := <-done:
			if err == nil {
				t.Error("Run returned nil after the port died; the owner cannot tell it must re-dial")
			}
		case <-time.After(2 * time.Second):
			t.Fatal("Run kept polling a dead port — every door on this bus would stay dead until restart")
		}
	})

	t.Run("writes start failing", func(t *testing.T) {
		bus := NewBus(deadTransport{}, fastOpts())
		_ = bus.Add(1)

		done := make(chan error, 1)
		go func() { done <- bus.Run(context.Background()) }()

		select {
		case err := <-done:
			if err == nil {
				t.Error("Run returned nil despite a port that cannot be written to")
			}
		case <-time.After(2 * time.Second):
			t.Fatal("Run ignored a permanently failing port")
		}
	})
}

// deadTransport fails every write, like a serial handle whose device has been removed.
type deadTransport struct{}

func (deadTransport) Read(p []byte) (int, error)  { select {} }
func (deadTransport) Write(p []byte) (int, error) { return 0, errors.New("device not configured") }
func (deadTransport) Close() error                { return nil }

// TestBusAddValidatesAddress keeps operator data from reaching the wire as a malformed frame.
func TestBusAddValidatesAddress(t *testing.T) {
	bus := NewBus(nopTransport{}, fastOpts())
	if err := bus.Add(0x80); err == nil {
		t.Error("Add accepted an address above 0x7F")
	}
	if err := bus.Add(1, 1); err != nil {
		t.Errorf("Add rejected a duplicate instead of ignoring it: %v", err)
	}
}

type nopTransport struct{}

func (nopTransport) Read(p []byte) (int, error)  { select {} }
func (nopTransport) Write(p []byte) (int, error) { return len(p), nil }
func (nopTransport) Close() error                { return nil }
