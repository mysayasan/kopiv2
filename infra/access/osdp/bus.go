package osdp

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Bus is the CP: it owns one RS-485 segment (or one TCP connection to a simulator/gateway) and
// polls every reader on it, forever.
//
// Three things about the shape here are not arbitrary, and each is a divergence from
// infra/iot/modbus that the OSDP plan §3.1 calls out:
//
//  1. The port is opened ONCE and held for the life of the Bus. Modbus's DialSerial opens, polls
//     and closes on every tick; doing that here would tear down the Secure Channel session on every
//     poll and add latency to every badge.
//  2. Card reads leave by CHANNEL, not by return value. You never ask a reader for a card; you
//     poll, and it hands one over when a human badges. Events() is the only way they surface.
//  3. Sequence numbers are per PD, not per bus — see pdState.seq.
type Bus struct {
	port Transport
	opts Options

	mu    sync.Mutex
	pds   map[uint8]*pdState
	order []uint8
	next  int // round-robin cursor into order

	reader *countingReader
	frames chan []byte
	events chan Event
	cmds   chan *cmdReq
	// portErr carries the first error that made the port unusable, so Run can return instead of
	// polling a dead wire forever. See failPort.
	portErr  chan error
	portOnce sync.Once

	dropped uint64 // events discarded because the consumer stalled
}

// Options tunes the poll loop. The zero value is usable; Defaults() documents what each becomes.
type Options struct {
	// SlotInterval is the minimum time between consecutive bus transactions. Per-PD cadence is
	// therefore SlotInterval × number of readers, which is the budget the OSDP plan §6.2 leaves
	// open: 50 ms across 3 readers gives each a 150 ms poll, comfortably inside the 100–200 ms
	// target. A 16-reader bus at 9600 baud will NOT sustain that, and needs a measured figure.
	SlotInterval time.Duration
	// ReplyTimeout is how long a PD gets to answer. A 20-byte frame at 9600 baud is about 21 ms,
	// so 200 ms is generous — deliberately, because a reader replying slowly is a diagnostic, not
	// a failure.
	ReplyTimeout time.Duration
	// OfflineAfter is how many consecutive failed transactions declare a reader offline. More than
	// one, because a single dropped frame on a noisy segment is normal and must not flap a door
	// out of service.
	OfflineAfter int
	// SupervisionTimeout declares a reader offline when it has not completed a USABLE transaction
	// within this window, however loudly it has been answering. It exists because OfflineAfter only
	// catches silence: a reader that NAKs every command, or replies BUSY forever, or is permanently
	// out of sequence, is alive and unusable — and without this it would sit in limbo indefinitely,
	// its door neither working nor alarmed. That is precisely the silent failure this suite
	// instruments against.
	SupervisionTimeout time.Duration
	// SecureRetryBackoff and SecureRetryMax bound how often a Secure Channel is re-established
	// after a failure, doubling from the first up to the second. They exist to damp a reader that
	// establishes a session and immediately loses it, which would otherwise flap a door in and out
	// of service several times a second and bury the audit log.
	SecureRetryBackoff time.Duration
	SecureRetryMax     time.Duration
	// StatusInterval is how often an online reader is asked for its LOCAL STATUS (tamper, mains
	// failure) and its INPUT STATUS (the door-position contact) instead of a bare poll.
	//
	// It has to exist at all because a PD volunteers neither. POLL is answered with ACK, or with a
	// queued card read or keypad entry — never with a status change — so a CP that only ever polls
	// learns nothing about the reader's enclosure being opened or the door being pushed. That was
	// the state of this driver until it was benched: `tamper`, `door-forced` and `door-held-open`
	// were declared, severity-mapped, titled and translated, and no condition on any site could
	// have reached any of them.
	//
	// One second is a compromise between two real costs. Too slow and a door forced at 03:00 is
	// reported a beat late; too fast and status frames crowd out card reads in the same slot
	// budget, which is the one thing a reader's poll cadence must never do. At the default 50 ms
	// slot on a three-reader bus each PD gets a slot roughly every 150 ms, so a 1 s status cadence
	// spends under a tenth of the bus on supervision.
	StatusInterval time.Duration
	// EventBuffer sizes the event channel.
	EventBuffer int
	// Logf, when set, receives protocol-level diagnostics.
	Logf func(format string, args ...any)
}

// Defaults fills any zero field with its default.
func (o Options) Defaults() Options {
	if o.SlotInterval <= 0 {
		o.SlotInterval = 50 * time.Millisecond
	}
	if o.ReplyTimeout <= 0 {
		o.ReplyTimeout = 200 * time.Millisecond
	}
	if o.OfflineAfter <= 0 {
		o.OfflineAfter = 3
	}
	if o.SupervisionTimeout <= 0 {
		o.SupervisionTimeout = 5 * time.Second
	}
	if o.SecureRetryBackoff <= 0 {
		o.SecureRetryBackoff = 500 * time.Millisecond
	}
	if o.SecureRetryMax <= 0 {
		o.SecureRetryMax = 30 * time.Second
	}
	if o.StatusInterval <= 0 {
		o.StatusInterval = time.Second
	}
	if o.EventBuffer <= 0 {
		o.EventBuffer = 256
	}
	return o
}

type cmdReq struct {
	addr  uint8
	code  Command
	data  []byte
	reply chan cmdResp
}

type cmdResp struct {
	frame *Frame
	err   error
}

var (
	// ErrUnknownPD is returned for a command addressed to a reader that was never added.
	ErrUnknownPD = errors.New("osdp: no such PD on this bus")
	// ErrTimeout means the reader did not answer within ReplyTimeout.
	ErrTimeout = errors.New("osdp: PD did not reply")
	// ErrBusClosed means Run has returned.
	ErrBusClosed = errors.New("osdp: bus closed")
)

// NewBus creates a CP over port. Add readers with Add, then call Run.
func NewBus(port Transport, opts Options) *Bus {
	opts = opts.Defaults()
	return &Bus{
		port:    port,
		opts:    opts,
		pds:     map[uint8]*pdState{},
		reader:  &countingReader{r: port},
		frames:  make(chan []byte, 16),
		events:  make(chan Event, opts.EventBuffer),
		cmds:    make(chan *cmdReq, 32),
		portErr: make(chan error, 1),
	}
}

// failPort records that the transport itself is unusable and wakes Run.
//
// This exists because a dead port is NOT the same as a dead reader, and treating it as one is a
// production outage waiting to happen. Without it, a CP whose USB-RS485 adapter is unplugged — or
// whose serial-to-Ethernet gateway reboots — keeps polling a wire nobody is listening to, marks
// every reader offline, and stays that way until somebody restarts the process. Every door on the
// segment is dead, silently, and the logs say only "reader offline".
//
// Returning instead lets the owner re-dial. It is deliberately one-shot: the first failure is the
// diagnosis, and the hundred that follow it are noise.
func (b *Bus) failPort(err error) {
	b.portOnce.Do(func() {
		select {
		case b.portErr <- err:
		default:
		}
	})
}

// PDConfig describes one reader on the bus.
type PDConfig struct {
	Address uint8
	// SCBK is the Secure Channel base key. Nil runs the reader in the clear, which is only
	// acceptable for `interior` doors. Use SCBKDefault for a factory-fresh reader you are about to
	// rekey — and note that a session on SCBK-D is cryptographically valid but worthless as a trust
	// signal, because that key is published in every vendor's manual.
	SCBK *SCBK
	// RequireSecureChannel makes the session mandatory. If it cannot be established, or drops
	// mid-session, the reader is taken OUT OF SERVICE and alarmed — never downgraded to cleartext.
	// The hardware plan §3.2 makes this a DOOR policy, not a reader property: the profile declares
	// what a reader can do, the door decides what it must do.
	RequireSecureChannel bool
}

// Add puts reader addresses into the polling rotation in the clear. It starts offline and is polled
// until it answers, so adding a reader that is not plugged in yet is harmless and self-healing.
func (b *Bus) Add(addrs ...uint8) error {
	for _, a := range addrs {
		if err := b.AddPD(PDConfig{Address: a}); err != nil {
			return err
		}
	}
	return nil
}

// AddPD puts a reader into the rotation with an explicit Secure Channel policy.
func (b *Bus) AddPD(cfg PDConfig) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if cfg.Address > MaxAddress {
		return fmt.Errorf("%w: %d", ErrBadAddress, cfg.Address)
	}
	if cfg.RequireSecureChannel && cfg.SCBK == nil {
		// Refusing here rather than at runtime: a door configured to require encryption with no key
		// to encrypt with would otherwise sit permanently out of service with a puzzling reason.
		return errors.New("osdp: RequireSecureChannel needs an SCBK")
	}
	if _, dup := b.pds[cfg.Address]; dup {
		return nil
	}
	pd := newPDState(cfg.Address, time.Now())
	pd.scbk = cfg.SCBK
	pd.requireSC = cfg.RequireSecureChannel
	b.pds[cfg.Address] = pd
	b.order = append(b.order, cfg.Address)
	return nil
}

// Secure reports whether a reader currently has an established Secure Channel session.
func (b *Bus) Secure(addr uint8) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if p, ok := b.pds[addr]; ok {
		return p.sc.established()
	}
	return false
}

// Events returns the channel every card read, keypad entry and status change arrives on. Drain it:
// a stalled consumer eventually causes events to be dropped, and a dropped event is a badge that
// never opened a door.
func (b *Bus) Events() <-chan Event { return b.events }

// Stats snapshots the per-reader counters.
func (b *Bus) Stats() map[uint8]PDStats {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make(map[uint8]PDStats, len(b.pds))
	for a, p := range b.pds {
		out[a] = p.stats
	}
	return out
}

// Status reports where a reader is in its lifecycle.
func (b *Bus) Status(addr uint8) PDStatus {
	b.mu.Lock()
	defer b.mu.Unlock()
	if p, ok := b.pds[addr]; ok {
		return p.status
	}
	return StatusOffline
}

// Dropped counts events discarded because Events() was not being drained.
func (b *Bus) Dropped() uint64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.dropped
}

// Send queues a command for a reader and waits for its reply. It is the low-level actuation
// primitive — Output wraps it for door strikes.
//
// The command jumps the round-robin queue: it is serviced in that reader's next slot rather than
// after a full rotation, because the latency that matters on this bus is badge-to-strike.
func (b *Bus) Send(ctx context.Context, addr uint8, code Command, data ...byte) (*Frame, error) {
	b.mu.Lock()
	_, known := b.pds[addr]
	b.mu.Unlock()
	if !known {
		return nil, fmt.Errorf("%w: address %d", ErrUnknownPD, addr)
	}

	req := &cmdReq{addr: addr, code: code, data: data, reply: make(chan cmdResp, 1)}
	select {
	case b.cmds <- req:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	select {
	case r := <-req.reply:
		return r.frame, r.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Output drives a reader output — the command that throws a door strike.
//
// Note this is the DRIVER-level primitive, not the application's actuation chokepoint. Every unlock
// in mypintusan (badge, plate, operator override, schedule, flow, API) must still funnel through
// one audited Issue-style call before reaching here, exactly as myiotsan funnels actuation through
// CommandService.Issue. That single path is the only reason "who opened door 3 at 02:14, and
// through what" is answerable. Do not call this from a second place because it is convenient.
func (b *Bus) Output(ctx context.Context, addr uint8, output byte, on bool, hold time.Duration) error {
	code := byte(1) // permanent off
	if on {
		code = 2 // permanent on
	}
	// Timer is in units of 100 ms; a timed unlock is safer than a permanent one because a CP that
	// dies mid-unlock leaves the strike to re-lock on the reader's own timer.
	ticks := uint16(hold / (100 * time.Millisecond))
	if on && ticks > 0 {
		code = 3 // on for the timer, then off
	}
	_, err := b.Send(ctx, addr, CmdOut, output, code, byte(ticks), byte(ticks>>8))
	return err
}

// Run owns the port and polls until ctx is cancelled. It is the only goroutine that writes to the
// port; everything else queues through Send.
func (b *Bus) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	go b.readLoop(ctx)

	// Closing the port is what actually stops readLoop: a goroutine blocked in Read does not
	// observe context cancellation, so waiting on it here would hang forever. Bus owns the port for
	// its lifetime, so Run closes it on the way out and lets the read unblock with an error.
	defer func() {
		_ = b.port.Close()
		close(b.events)
	}()

	for {
		if ctx.Err() != nil {
			b.failPending(ErrBusClosed)
			return ctx.Err()
		}
		// A dead transport ends the run so the owner can re-dial. Polling on is pointless: every
		// reader would go offline and stay offline until the process restarted.
		select {
		case err := <-b.portErr:
			b.failPending(err)
			return err
		default:
		}
		b.drainCommands()

		pd := b.nextPD()
		if pd == nil {
			// No readers configured yet; idle rather than spin.
			if !sleepCtx(ctx, b.opts.SlotInterval) {
				b.failPending(ErrBusClosed)
				return ctx.Err()
			}
			continue
		}

		start := time.Now()
		b.transact(ctx, pd)
		if !sleepCtx(ctx, b.opts.SlotInterval-time.Since(start)) {
			b.failPending(ErrBusClosed)
			return ctx.Err()
		}
	}
}

// readLoop is the only reader of the port. It runs continuously rather than being driven by the
// poll loop, so a late reply is still decoded (and attributed) instead of desynchronising the
// stream for every subsequent transaction.
func (b *Bus) readLoop(ctx context.Context) {
	sc := bufio.NewScanner(b.reader)
	sc.Buffer(make([]byte, 0, 4096), MaxFrameSize*4)
	sc.Split(ScanFrames)
	for sc.Scan() {
		raw := append([]byte(nil), sc.Bytes()...)
		select {
		case b.frames <- raw:
		case <-ctx.Done():
			return
		}
	}
	// A read error here is normal shutdown (Run closed the port) or a genuinely broken port. Only
	// the second is worth reporting, and the caller learns of it anyway when every reader goes
	// offline — so log it rather than trying to distinguish the two.
	if ctx.Err() != nil {
		return // ordinary shutdown
	}
	err := sc.Err()
	if err == nil {
		// A clean EOF is still the end of the port: the peer closed the connection.
		err = errors.New("osdp: port closed by the peer")
	}
	b.logf("port read ended: %v", err)
	b.failPort(err)
}

// drainCommands moves queued commands onto their target reader without blocking.
func (b *Bus) drainCommands() {
	for {
		select {
		case req := <-b.cmds:
			b.mu.Lock()
			pd, ok := b.pds[req.addr]
			if !ok {
				b.mu.Unlock()
				req.reply <- cmdResp{err: fmt.Errorf("%w: address %d", ErrUnknownPD, req.addr)}
				continue
			}
			if pd.pending != nil {
				// One outstanding command per reader. Queueing a second would let two unlocks
				// stack up behind a slow reader and fire back to back.
				prev := pd.pending
				pd.pending = req
				b.mu.Unlock()
				prev.reply <- cmdResp{err: errors.New("osdp: superseded by a newer command")}
				continue
			}
			pd.pending = req
			b.mu.Unlock()
		default:
			return
		}
	}
}

// nextPD picks the reader for this slot: one with a queued command first, else round-robin.
func (b *Bus) nextPD() *pdState {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.order) == 0 {
		return nil
	}
	for _, a := range b.order {
		if p := b.pds[a]; p.pending != nil {
			return p
		}
	}
	p := b.pds[b.order[b.next%len(b.order)]]
	b.next++
	return p
}

// transact runs one command/reply exchange with one reader.
func (b *Bus) transact(ctx context.Context, pd *pdState) {
	b.mu.Lock()
	seq := pd.nextSeq()
	operatorCmd := pd.pending != nil
	pd.stats.Transactions++

	// The Secure Channel handshake pre-empts everything else: until it completes there is nothing
	// this reader may usefully be asked, and on a RequireSecureChannel door there is nothing it is
	// ALLOWED to be asked.
	var frame *Frame
	var code Command
	handshake := false
	switch {
	case pd.scNext != nil:
		frame, pd.scNext = pd.scNext, nil
		code, handshake = Command(frame.Code), true
	case pd.wantsSecureChannel(time.Now()):
		pd.sc = newSecureChannel(*pd.scbk)
		pd.status = StatusSecuring
		f, err := pd.sc.challenge(pd.address)
		if err != nil {
			pd.sc.fail()
			b.mu.Unlock()
			b.secureChannelLost(pd, err.Error())
			b.completePending(pd, nil, err)
			return
		}
		frame, code, handshake = f, CmdChlng, true
	default:
		c, data := pd.nextCommand(time.Now(), b.opts.StatusInterval)
		frame, code = &Frame{Address: pd.address, Code: byte(c), Data: data}, c
	}
	frame.Sequence = seq
	frame.Address = pd.address

	// Seal ordinary traffic once a session exists. Handshake frames carry their own security block
	// and must go out unsealed — SCRYPT in particular is what CREATES the session it would need.
	var raw []byte
	var err error
	if !handshake && pd.sc.established() {
		raw, err = pd.sc.seal(frame, true)
	} else {
		raw, err = frame.Marshal()
	}
	b.mu.Unlock()

	b.checkSupervision(pd)

	if err != nil {
		b.completePending(pd, nil, err)
		return
	}

	before := b.reader.count()
	// Discard any stale frame still queued from a previous slot before transmitting, so a late
	// reply from the last reader is never mistaken for this one's answer.
	b.flushFrames()

	if _, err := b.port.Write(raw); err != nil {
		b.fail(pd, fmt.Sprintf("write: %v", err))
		b.failPort(fmt.Errorf("osdp: port write failed: %w", err))
		b.completePending(pd, nil, err)
		return
	}

	rawReply, reply, err := b.awaitReply(ctx, pd, before)
	if err != nil {
		b.completePending(pd, nil, err)
		return
	}

	// Unwrap an established session's reply before the state machine sees it. unseal also refuses a
	// CLEARTEXT reply on a secure session — the downgrade an RS-485 tap wants — and any integrity
	// failure lands in secureChannelLost rather than being retried through.
	if !handshake && pd.sc.established() {
		b.mu.Lock()
		plain, uerr := pd.sc.unseal(rawReply, reply, false)
		b.mu.Unlock()
		if uerr != nil {
			b.secureChannelLost(pd, uerr.Error())
			b.completePending(pd, nil, uerr)
			return
		}
		reply = plain
		// A sealed round-trip actually completed, so the session is not merely established but
		// WORKING. That — and only that — clears the re-establishment backoff. Clearing it when
		// the handshake completes instead would defeat the damping entirely, because completing a
		// handshake and then immediately losing the session is the exact loop being damped.
		b.mu.Lock()
		pd.scFailures = 0
		b.mu.Unlock()
	}

	b.handleReply(pd, code, seq, reply, operatorCmd)
}

// secureChannelLost handles a Secure Channel that could not be established or has broken.
//
// The posture is the security-critical rule from the hardware plan §3.2, and it is deliberately
// blunt: on a door that REQUIRES Secure Channel there is no cleartext fallback, ever. The reader
// goes out of service and alarms. A downgrade-to-plaintext fallback is precisely the attack an
// RS-485 tap is looking for, and "the door kept working" is how it goes unnoticed for a year.
//
// Where the door does NOT require it, the failure is still surfaced as a fault — it just does not
// take the reader out of service. The session is left in the terminal failed state either way, so
// the CP does not sit re-challenging a reader that has already said no on every single slot.
func (b *Bus) secureChannelLost(pd *pdState, reason string) {
	b.mu.Lock()
	pd.scNext = nil
	pd.stats.SecureFailures++
	pd.scFailures++
	pd.scRetryAt = time.Now().Add(secureRetryDelay(b.opts.SecureRetryBackoff, b.opts.SecureRetryMax, pd.scFailures))
	required := pd.requireSC
	first := !pd.announced

	var onlineEv Event
	if required {
		// Nothing to degrade to, so keep trying: clearing the session (rather than parking it in
		// the terminal failed state) means the next slot re-challenges, and the reader recovers by
		// itself once whatever was wrong is fixed. It stays offline until then.
		pd.sc = nil
		pd.status = StatusOffline
		if first {
			pd.announced = true
			pd.stats.Offlines++
		}
	} else {
		// Optional: give up permanently for this connection and run in the clear, rather than
		// re-challenging a reader that has already said no on every slot forever.
		if pd.sc != nil {
			pd.sc.fail()
		}
		// ANNOUNCE THE DOWNGRADE, whichever state the reader was in.
		//
		// This used to fire only from StatusSecuring — a reader that never got a session up. But
		// the harder case is the one this misses: a session that ESTABLISHES and then DROPS. That
		// reader is already StatusOnline, so no event was emitted, and every consumer kept the
		// `SecureSession: true` it was told at handshake — permanently. Measured against the
		// simulator's `sc-drop` scenario: a door configured to require an encrypted session went
		// on granting on a reader whose session had died, which is precisely the RS-485 tap this
		// whole mechanism exists to defeat, and the PD's own comment calls it "the harder half".
		//
		// Re-announcing from Online too means the downgrade is a fact the caller learns, rather
		// than one only the bus knows.
		if pd.status == StatusSecuring || pd.status == StatusOnline {
			pd.status = StatusOnline
			sc, dflt := pd.security()
			onlineEv = Event{At: time.Now(), Address: pd.address, Kind: EventOnline,
				Info: pd.info, Caps: pd.caps, SupportsSecureChannel: sc, DefaultKey: dflt}
		}
	}
	b.mu.Unlock()

	if required {
		if first {
			b.emit(Event{At: time.Now(), Address: pd.address, Kind: EventOffline,
				Reason: "Secure Channel required but not established (" + reason +
					") — reader out of service, no cleartext fallback"})
		}
		return
	}
	b.emit(Event{At: time.Now(), Address: pd.address, Kind: EventFault, Fault: FaultSecureChannel,
		Reason: "Secure Channel unavailable (" + reason + ") — continuing in the clear"})
	if onlineEv.Kind == EventOnline {
		b.emit(onlineEv)
	}
}

// checkSupervision declares a reader offline when it has answered recently but has not completed a
// usable transaction inside the supervision window.
//
// This is the rule that closes the limbo hole. OfflineAfter only ever fires on silence; a reader
// that NAKs everything, stays BUSY, or is permanently out of sequence keeps answering, so its
// failure counter keeps getting reset and it is never declared anything at all. Live-running the
// driver against the simulator's bad-sequence scenario is what surfaced it: the CP emitted the same
// fault fifteen times in seven seconds, never recovered, and never alarmed.
func (b *Bus) checkSupervision(pd *pdState) {
	b.mu.Lock()
	stale := time.Since(pd.lastGoodAt) > b.opts.SupervisionTimeout && !pd.announced
	if stale {
		pd.announced = true
		pd.status = StatusOffline
		pd.stats.Offlines++
		pd.resetSeq = true
		pd.info = PDInfo{}
		pd.caps = nil
		// Forget the last status reading with the identity. A reader that was tampered — or a door
		// that was opened — while the reader was away must be reported when it comes back, and an
		// edge-triggered emitter that still holds the pre-outage reading would swallow exactly
		// that. Re-reporting a condition that never cleared is the safe direction to err.
		pd.haveLocal, pd.haveInputs, pd.lastInputs = false, false, nil
		pd.statusAt, pd.pollsSinceStatus = time.Time{}, 0
	}
	b.mu.Unlock()
	if stale {
		b.emit(Event{At: time.Now(), Address: pd.address, Kind: EventOffline,
			Reason: fmt.Sprintf("answering but no usable transaction for %s — reader is present but unusable",
				b.opts.SupervisionTimeout)})
	}
}

// awaitReply waits for a decodable frame from this reader, discarding frames from other addresses.
//
// It returns the RAW bytes alongside the decoded frame because a Secure Channel MAC is computed
// over the wire form. Re-marshalling the decoded frame to check it would verify our own encoder
// rather than what the reader actually sent.
func (b *Bus) awaitReply(ctx context.Context, pd *pdState, bytesBefore uint64) ([]byte, *Frame, error) {
	deadline := time.NewTimer(b.opts.ReplyTimeout)
	defer deadline.Stop()

	badFrames := 0

	for {
		select {
		case <-ctx.Done():
			return nil, nil, ctx.Err()

		case <-deadline.C:
			unframed := b.reader.count() - bytesBefore
			b.mu.Lock()
			pd.stats.Timeouts++
			pd.stats.UnframedBytes += unframed
			b.mu.Unlock()

			// Three different faults look identical from a distance — nothing on the wire, wreckage
			// on the wire, and well-formed frames failing their CRC — and they send an installer to
			// three different places. Distinguishing them here is the difference between "check the
			// reader address" and "check your termination".
			var reason string
			switch {
			case badFrames > 0:
				// Frames arrived intact enough to parse a header, then failed the CRC. That is bus
				// integrity, not addressing: termination, grounding, or cable length.
				reason = fmt.Sprintf("%d frame(s) arrived but failed CRC — check 120Ω termination at both "+
					"ends, shield grounded at one end only, and cable length", badFrames)
			case unframed > 0:
				// Bytes arrived but never formed a frame at all. On a bus where readers ship at
				// address 0, the overwhelmingly likely cause is TWO readers answering at once and
				// corrupting each other — which at the frame layer is otherwise identical to an
				// empty address.
				reason = fmt.Sprintf("%d bytes received but no frame decoded — "+
					"likely two readers sharing address %d, or a badly terminated segment", unframed, pd.address)
			default:
				reason = "no reply"
			}
			b.fail(pd, reason)
			return nil, nil, ErrTimeout

		case raw := <-b.frames:
			f, err := Unmarshal(raw)
			if err != nil {
				badFrames++
				b.mu.Lock()
				pd.stats.CrcErrors++
				b.mu.Unlock()
				b.logf("addr=%d undecodable frame: %v", pd.address, err)
				continue // keep waiting; the real reply may still arrive inside the timeout
			}
			if !f.Reply || f.Address != pd.address {
				continue // another reader's traffic, or our own transmission on a half-duplex bus
			}
			return raw, f, nil
		}
	}
}

// flushFrames discards anything already decoded but not yet consumed.
func (b *Bus) flushFrames() {
	for {
		select {
		case <-b.frames:
		default:
			return
		}
	}
}

// handleReply applies one reply to the reader's state machine.
func (b *Bus) handleReply(pd *pdState, sent Command, seq uint8, f *Frame, operatorCmd bool) {
	// A reply carrying the wrong sequence number is the §3.1 fault. The reader is present and
	// talking, so the first move is a session reset rather than an offline declaration — but it
	// still counts as a FAILED transaction. Not counting it was the limbo bug: a permanently
	// skewed reader answered forever, so its failure counter never advanced, and it was never
	// declared anything while a door sat bound to it.
	if f.Sequence != seq {
		b.mu.Lock()
		pd.stats.SequenceErrs++
		pd.resetSeq = true
		first := pd.failures == 0
		b.mu.Unlock()
		if first {
			b.emit(Event{At: time.Now(), Address: pd.address, Kind: EventFault,
				Reason: fmt.Sprintf("reply sequence %d, expected %d — resetting the session", f.Sequence, seq)})
		}
		b.fail(pd, fmt.Sprintf("reply sequence %d, expected %d — reader is present but unusable", f.Sequence, seq))
		b.completePending(pd, nil, fmt.Errorf("osdp: reply out of sequence"))
		return
	}

	b.mu.Lock()
	pd.failures = 0
	b.mu.Unlock()

	switch Reply(f.Code) {
	case RplBusy:
		// "Ask me again", not "I am broken". It costs a slot and nothing else.
		b.mu.Lock()
		pd.stats.Busy++
		b.mu.Unlock()
		b.completePending(pd, nil, errors.New("osdp: PD busy"))
		return

	case RplNak:
		b.mu.Lock()
		pd.stats.Naks++
		nak := NakCode(0)
		if len(f.Data) > 0 {
			nak = NakCode(f.Data[0])
		}
		if nak == NakBadSequence {
			pd.stats.SequenceErrs++
			pd.resetSeq = true
		}
		first := pd.failures == 0
		b.mu.Unlock()

		// A NAK to CHLNG or SCRYPT is a REFUSED HANDSHAKE, not an ordinary command failure. It is
		// the security-critical case: the reader will not encrypt, so a door that requires it must
		// go out of service rather than quietly carry on in the clear.
		if sent == CmdChlng || sent == CmdSCrypt {
			b.secureChannelLost(pd, fmt.Sprintf("%s refused: %s", sent, nak))
			b.completePending(pd, f, fmt.Errorf("%w: %s (%s)", ErrSecureChannelRefused, sent, nak))
			return
		}

		if first {
			b.emit(Event{At: time.Now(), Address: pd.address, Kind: EventFault,
				Reason: fmt.Sprintf("%s refused: %s", sent, nak)})
		}
		// A refused OPERATOR command is that command's problem — the caller gets the error and the
		// reader stays healthy, which is what keeps a CHLNG refusal (expected until securechannel.go
		// lands) from knocking a working reader offline. A refused POLL or identify command is
		// different: the reader cannot be driven at all, so it counts against supervision.
		if !operatorCmd {
			b.fail(pd, fmt.Sprintf("%s refused: %s", sent, nak))
		}
		b.completePending(pd, f, fmt.Errorf("osdp: %s NAKed (%s)", sent, nak))
		return

	case RplPDID:
		info, err := parsePDID(f.Data)
		if err != nil {
			b.emit(Event{At: time.Now(), Address: pd.address, Kind: EventFault, Reason: err.Error()})
			break
		}
		b.mu.Lock()
		pd.info = info
		pd.status = StatusIdentifying
		b.mu.Unlock()

	case RplPDCap:
		caps, err := parsePDCap(f.Data)
		if err != nil {
			b.emit(Event{At: time.Now(), Address: pd.address, Kind: EventFault, Reason: err.Error()})
			break
		}
		b.mu.Lock()
		pd.caps = caps
		secure := pd.scbk != nil
		if secure {
			// Identified, but not yet trusted. The reader is not announced online until the session
			// is up, so nothing binds a door to it in the window where it is reachable but
			// unencrypted.
			pd.status = StatusSecuring
			b.mu.Unlock()
			break
		}
		pd.status = StatusOnline
		sc, dflt := pd.security()
		ev := Event{At: time.Now(), Address: pd.address, Kind: EventOnline,
			Info: pd.info, Caps: caps, SupportsSecureChannel: sc, DefaultKey: dflt}
		b.mu.Unlock()
		b.emit(ev)

	case RplCCrypt:
		b.mu.Lock()
		next, err := pd.sc.acceptCCRYPT(pd.address, f.Data)
		if err == nil {
			pd.scNext = next
		}
		b.mu.Unlock()
		if err != nil {
			b.secureChannelLost(pd, err.Error())
			b.completePending(pd, nil, err)
			return
		}

	case RplRMacI:
		b.mu.Lock()
		err := pd.sc.acceptRMACI(f.Data)
		var ev Event
		if err == nil {
			pd.status = StatusOnline
			sc, dflt := pd.security()
			ev = Event{At: time.Now(), Address: pd.address, Kind: EventOnline,
				Info: pd.info, Caps: pd.caps, SupportsSecureChannel: sc, DefaultKey: dflt,
				SecureSession: true, DefaultKeySession: pd.sc.usingDefault}
		}
		b.mu.Unlock()
		if err != nil {
			b.secureChannelLost(pd, err.Error())
			b.completePending(pd, nil, err)
			return
		}
		b.emit(ev)

	case RplRaw:
		card, err := parseCardRead(f.Data)
		if err != nil {
			b.emit(Event{At: time.Now(), Address: pd.address, Kind: EventFault, Reason: err.Error()})
			break
		}
		// THE fail-closed boundary. A reader whose door requires Secure Channel and does not have
		// one must not deliver credentials, and this check is what guarantees it.
		//
		// It is not redundant with refusing to poll: an offline reader is still polled so it can
		// recover, a card queued before the session broke can still arrive, and any future code
		// path that reaches a reader in the clear passes through here too. Dropping the credential
		// at the point of delivery is the one place that cannot be bypassed by accident — and a
		// badge silently served over a channel we required to be encrypted is precisely the
		// downgrade that makes a tapped RS-485 segment invisible.
		if b.credentialsBlocked(pd) {
			b.emit(Event{At: time.Now(), Address: pd.address, Kind: EventFault, Fault: FaultSecureChannel,
				Reason: "credential presented but Secure Channel is required and not established — denied"})
			break
		}
		b.emit(Event{At: time.Now(), Address: pd.address, Kind: EventCard, Card: card})

	case RplKeypad:
		_, digits, err := parseKeypad(f.Data)
		if err != nil {
			b.emit(Event{At: time.Now(), Address: pd.address, Kind: EventFault, Reason: err.Error()})
			break
		}
		if b.credentialsBlocked(pd) {
			b.emit(Event{At: time.Now(), Address: pd.address, Kind: EventFault, Fault: FaultSecureChannel,
				Reason: "PIN entered but Secure Channel is required and not established — denied"})
			break
		}
		b.emit(Event{At: time.Now(), Address: pd.address, Kind: EventKeypad, Digits: digits})

	case RplLStatR:
		// Local status: the reader's own enclosure and its mains feed. Emitted on the EDGE, so a
		// case that stays open reports once rather than once per status interval forever.
		if len(f.Data) >= 2 {
			tamper, power := f.Data[0] != 0, f.Data[1] != 0
			b.mu.Lock()
			changed := !pd.haveLocal || tamper != pd.lastTamper || power != pd.lastPowerFail
			pd.haveLocal, pd.lastTamper, pd.lastPowerFail = true, tamper, power
			b.mu.Unlock()
			if changed {
				b.emit(Event{At: time.Now(), Address: pd.address, Kind: EventStatus,
					Tamper: tamper, PowerFail: power})
			}
		}

	case RplIStatR:
		// Input status: the door-position contact, and whatever else the installer supervised.
		//
		// The FIRST reading is emitted even though nothing has changed yet, and that is
		// deliberate. Suppressing it would mean a door already standing open when the controller
		// starts is never reported at all — the held-open timer only runs on a door the machine
		// believes is open — so the one case where a propped door survives a restart unnoticed is
		// exactly the case the alarm exists for.
		inputs := make([]bool, len(f.Data))
		for i, v := range f.Data {
			inputs[i] = v != 0
		}
		b.mu.Lock()
		changed := !pd.haveInputs || !sameInputs(pd.lastInputs, inputs)
		pd.haveInputs, pd.lastInputs = true, inputs
		b.mu.Unlock()
		if changed {
			b.emit(Event{At: time.Now(), Address: pd.address, Kind: EventInput, Inputs: inputs})
		}
	}

	// Reaching here means a USABLE reply — not a NAK, not a BUSY, not out of sequence. That is what
	// supervision measures, and it is also what ends a failure episode, so a reader that dies a
	// second time is reported a second time.
	b.mu.Lock()
	pd.lastGoodAt = time.Now()
	pd.announced = false
	if sent == CmdKeySet && Reply(f.Code) == RplAck {
		// The reader has just adopted a new base key, so our session — built on the old one — is
		// dead the moment this reply lands. Drop it and re-handshake with whatever key is now
		// configured. If the caller forgot to update that, the handshake fails and the fail-closed
		// path takes over, which is the right outcome: a half-rekeyed reader is not trustworthy.
		pd.sc, pd.scNext = nil, nil
		if pd.requireSC {
			pd.status = StatusSecuring
		}
	}
	b.mu.Unlock()

	b.completePending(pd, f, nil)
}

// sameInputs reports whether two input readings are identical. A reader that gains or loses an
// input between readings has changed by definition, so a length difference counts as a change.
func sameInputs(a, b []bool) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// credentialsBlocked reports that this reader must not deliver credentials because its door
// requires a Secure Channel it does not currently have.
func (b *Bus) credentialsBlocked(pd *pdState) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return pd.secureChannelUnusable()
}

// fail records a failed transaction and takes the reader offline once the failures pass the
// threshold. One dropped frame on a noisy segment is normal; OfflineAfter of them is not.
//
// The announcement is gated on `announced`, NOT on the previous status, and that distinction is
// load-bearing. Gating on "was online" means a reader that NEVER came online never reports anything
// at all — so an address where two readers are fighting, the single most useful thing onboarding
// can be told, stays completely silent. Announcing once per failure episode covers both: a reader
// that died, and one that was never reachable to begin with.
func (b *Bus) fail(pd *pdState, reason string) {
	b.mu.Lock()
	pd.failures++
	crossed := pd.failures >= b.opts.OfflineAfter && !pd.announced
	if crossed {
		pd.announced = true
		pd.status = StatusOffline
		pd.stats.Offlines++
		// Force a session reset and re-identification on recovery: a reader that vanished may have
		// been power-cycled, and its own sequence expectation went with it.
		pd.resetSeq = true
		pd.info = PDInfo{}
		pd.caps = nil
		// Drop the session with the identity. A reader that vanished may have been power-cycled or
		// swapped for another unit entirely, and resuming an old session against a new device is
		// precisely the substitution a secure channel exists to prevent. Clearing it also lets a
		// previously FAILED handshake be retried on recovery rather than staying failed forever.
		//
		// If the session was ESTABLISHED when it went quiet, this is a session drop rather than an
		// absent reader, so it must be damped like one. Without this the flap backoff leaks: a
		// reader that dies SILENTLY (rather than answering in cleartext) never reaches
		// secureChannelLost, so it re-handshakes on the very next slot and flaps as fast as the bus
		// will carry it — measured at 67 transitions in 12 s against the simulator, with the
		// backoff apparently in place. The distinction matters the other way too: a reader that was
		// merely absent must NOT be delayed by up to the cap before it can re-enrol.
		if pd.sc.established() {
			pd.scFailures++
			pd.scRetryAt = time.Now().Add(
				secureRetryDelay(b.opts.SecureRetryBackoff, b.opts.SecureRetryMax, pd.scFailures))
		}
		pd.sc, pd.scNext = nil, nil
	}
	b.mu.Unlock()

	if crossed {
		b.emit(Event{At: time.Now(), Address: pd.address, Kind: EventOffline, Reason: reason})
	}
	b.logf("addr=%d transaction failed: %s", pd.address, reason)
}

// completePending answers a queued Send, if this slot was serving one.
func (b *Bus) completePending(pd *pdState, f *Frame, err error) {
	b.mu.Lock()
	req := pd.pending
	pd.pending = nil
	b.mu.Unlock()
	if req != nil {
		req.reply <- cmdResp{frame: f, err: err}
	}
}

// failPending answers every outstanding command when the bus shuts down, so no caller blocks
// forever on a Send that will never be serviced.
func (b *Bus) failPending(err error) {
	b.mu.Lock()
	var reqs []*cmdReq
	for _, pd := range b.pds {
		if pd.pending != nil {
			reqs = append(reqs, pd.pending)
			pd.pending = nil
		}
	}
	b.mu.Unlock()
	for _, r := range reqs {
		r.reply <- cmdResp{err: err}
	}
	for {
		select {
		case r := <-b.cmds:
			r.reply <- cmdResp{err: err}
		default:
			return
		}
	}
}

// emit publishes an event, preferring to drop rather than to stall the bus.
//
// The trade-off is deliberate and uncomfortable either way: blocking here would stop polling — and
// with it supervision, tamper detection and every other reader on the segment — because one
// consumer is slow. Dropping loses a badge. Bounded blocking first, then a counted drop, keeps the
// bus alive while making the loss visible via Dropped() rather than silent.
func (b *Bus) emit(ev Event) {
	select {
	case b.events <- ev:
		return
	default:
	}
	t := time.NewTimer(b.opts.ReplyTimeout)
	defer t.Stop()
	select {
	case b.events <- ev:
	case <-t.C:
		b.mu.Lock()
		b.dropped++
		b.mu.Unlock()
		b.logf("EVENT DROPPED (consumer not draining): %s", ev)
	}
}

func (b *Bus) logf(format string, args ...any) {
	if b.opts.Logf != nil {
		b.opts.Logf(format, args...)
	}
}
