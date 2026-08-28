package osdp

import (
	"fmt"
	"time"
)

// The CP-side per-PD state machine: offline → identify → online, plus the supervision that decides
// which of those a reader is in. bus.go owns the port and the loop; this file owns what happens to
// one reader.

// PDStatus is where a reader sits in its lifecycle.
type PDStatus int

const (
	// StatusOffline means nothing has answered recently. A door bound to an offline reader is out
	// of service — it does not fall back to anything.
	StatusOffline PDStatus = iota
	// StatusIdentifying means the reader answered and we are asking who it is (ID, then CAP). A
	// reader is deliberately NOT usable in this state: binding a door before PDCAP is known would
	// mean enforcing the trust rules against unknown capabilities.
	StatusIdentifying
	// StatusSecuring means identified, and the Secure Channel handshake is in flight. A door is
	// NOT served in this state: serving it would mean answering badges over a channel we have just
	// decided must be encrypted, before it is.
	StatusSecuring
	// StatusOnline means identified and polling normally.
	StatusOnline
)

func (s PDStatus) String() string {
	switch s {
	case StatusOffline:
		return "offline"
	case StatusIdentifying:
		return "identifying"
	case StatusSecuring:
		return "securing"
	case StatusOnline:
		return "online"
	}
	return "unknown"
}

// EventKind discriminates a Bus event.
type EventKind int

const (
	EventCard    EventKind = iota // a credential was presented
	EventKeypad                   // PIN digits were entered
	EventOnline                   // a reader completed identification
	EventOffline                  // a reader stopped answering
	EventStatus                   // tamper / mains failure changed
	EventInput                    // a supervised input changed — the door-position contact
	EventFault                    // a protocol fault worth surfacing, reader still present
)

func (k EventKind) String() string {
	switch k {
	case EventCard:
		return "card"
	case EventKeypad:
		return "keypad"
	case EventOnline:
		return "online"
	case EventOffline:
		return "offline"
	case EventStatus:
		return "status"
	case EventInput:
		return "input"
	case EventFault:
		return "fault"
	}
	return "unknown"
}

// Event is something that happened on the bus. Card reads arrive here and nowhere else: a PD hands
// one over on a poll reply, unprompted, so there is no call that returns a card and never will be.
type Event struct {
	At      time.Time
	Address uint8
	Kind    EventKind

	Card   CardRead // EventCard
	Digits []byte   // EventKeypad

	// Info/Caps are populated on EventOnline — the identity the door binding is checked against.
	Info PDInfo
	Caps []Capability
	// SupportsSecureChannel and DefaultKey are lifted out of PDCAP capability 9 because both drive
	// policy rather than diagnostics. DefaultKey means the reader is still on the well-known base
	// key SCBK-D and is therefore NOT secure, whatever else it claims — the hardware plan caps such
	// a reader at `interior` doors until a KEYSET lands.
	SupportsSecureChannel bool
	DefaultKey            bool
	// SecureSession reports that this reader came online behind an ESTABLISHED Secure Channel, as
	// opposed to merely claiming it could. SupportsSecureChannel is the reader's advertisement;
	// this is the observed fact, and only the second one is worth binding a `critical` door to.
	SecureSession bool
	// DefaultKeySession means that established session was built on SCBK-D. Cryptographically it is
	// a real session; as a trust signal it is worth nothing, because the key is published in every
	// vendor's manual. Such a reader stays capped at `interior` until a KEYSET replaces the key.
	DefaultKeySession bool

	// Tamper/PowerFail carry the LSTATR flags on EventStatus.
	Tamper, PowerFail bool
	// Inputs carries the ISTATR contact states on EventInput, one entry per supervised input.
	//
	// INPUT 0 IS THE DOOR-POSITION CONTACT and true means the door is OPEN. That is a convention,
	// not something the protocol states: OSDP reports an input's electrical state and leaves the
	// meaning to the installer, so a normally-closed contact wired the other way round reports the
	// inverse. Per-input polarity is a reader-profile setting this driver does not yet carry;
	// until it does, a site must wire the contact so that "door open" asserts the input.
	Inputs []bool

	// Reason explains EventOffline and EventFault in operator language.
	Reason string
}

func (e Event) String() string {
	switch e.Kind {
	case EventCard:
		return fmt.Sprintf("addr=%d card % x (%d bits)", e.Address, e.Card.Data, e.Card.BitCount)
	case EventOnline:
		sc := "cleartext"
		if e.SecureSession {
			sc = "SECURE"
		} else if e.SupportsSecureChannel {
			sc = "cleartext(sc-capable)"
		}
		if e.DefaultKey || e.DefaultKeySession {
			sc += " DEFAULT-KEY"
		}
		return fmt.Sprintf("addr=%d online serial=%#08x fw=%d.%d.%d %s",
			e.Address, e.Info.Serial, e.Info.FirmwareMajor, e.Info.FirmwareMinor, e.Info.FirmwareBuild, sc)
	case EventOffline, EventFault:
		return fmt.Sprintf("addr=%d %s: %s", e.Address, e.Kind, e.Reason)
	case EventStatus:
		return fmt.Sprintf("addr=%d status tamper=%t mains-fail=%t", e.Address, e.Tamper, e.PowerFail)
	case EventInput:
		return fmt.Sprintf("addr=%d inputs %v", e.Address, e.Inputs)
	default:
		return fmt.Sprintf("addr=%d %s", e.Address, e.Kind)
	}
}

// PDStats is the per-reader counter set. These are the numbers that make a sick RS-485 segment
// visible before it becomes an outage: a bus with failing termination or a ground loop shows a
// rising CrcErrors long before any reader actually drops offline.
type PDStats struct {
	Transactions uint64
	Timeouts     uint64
	CrcErrors    uint64 // undecodable frames attributed to this PD's slot
	Naks         uint64
	Busy         uint64
	SequenceErrs uint64
	Offlines     uint64
	// SecureFailures counts Secure Channel handshakes that were refused and sessions that broke.
	// On a bus that is supposed to be encrypted, a rising count here is the difference between "a
	// reader is flaky" and "someone is on the pair".
	SecureFailures uint64
	// UnframedBytes counts bytes that arrived in this PD's slot but never formed a frame. A
	// non-zero value against a reader that never answers is the signature of two PDs sharing one
	// address (see countingReader in transport.go).
	UnframedBytes uint64
}

// pdState is everything the CP knows about one reader.
type pdState struct {
	address uint8
	status  PDStatus

	// seq is THIS PD's sequence number. Keyed per address, never shared across the bus: reusing a
	// number a PD has already seen makes it replay its previous reply, which silently swallows a
	// queued card read and presents as a reader that intermittently misses badges.
	seq uint8
	// resetSeq forces the next transaction onto sequence 0, the session-start value. It is the
	// only way to recover a PD that has NAKed us out of step.
	resetSeq bool

	info PDInfo
	caps []Capability

	failures int // consecutive failed transactions
	// announced records that this failure episode has already been reported, so a permanently
	// absent reader produces one event rather than one per slot forever. Cleared when it comes
	// online, so a reader that dies twice is reported twice.
	announced bool
	// lastGoodAt is when this reader last completed a USABLE transaction — not merely answered.
	// A NAK, a BUSY or an out-of-sequence reply all prove the reader is alive while proving it is
	// not working, and a reader stuck in any of those states would otherwise sit in limbo forever:
	// never online, never declared offline, so a door bound to it is unusable AND unalarmed. The
	// counter-based rule catches sudden silence; this timer catches every slow-burn variant with
	// one concept instead of a special case per failure mode.
	lastGoodAt time.Time
	stats      PDStats

	// scbk is the base key for this reader; nil means run in the clear. requireSC records the
	// door-level policy: when the session cannot be established or drops, the reader is taken OUT
	// OF SERVICE and alarmed rather than continuing unencrypted. That posture is the whole point —
	// a downgrade-to-plaintext fallback is exactly what an RS-485 tap wants, and "the door kept
	// working" is how it goes unnoticed for a year.
	scbk      *SCBK
	requireSC bool
	sc        *secureChannel
	// scNext is the next handshake frame to transmit. The handshake is reply-driven — each step
	// produces the following one — so the frame is parked here by handleReply and picked up by the
	// next slot, rather than the poll loop trying to reconstruct where the exchange had got to.
	scNext *Frame
	// scFailures and scRetryAt back off repeated re-establishment.
	//
	// Without them a reader that establishes a session and immediately loses it — a failing
	// transceiver, or someone on the pair — re-handshakes every slot and flaps online/offline
	// about ten times a second. That floods the audit log and thrashes the door in and out of
	// service, which is the same event-flood failure as an unbounded fault stream. scFailures is
	// cleared only by a successfully SEALED transaction, not merely by a completed handshake:
	// completing a handshake and then dropping it is exactly the loop being damped.
	scFailures int
	scRetryAt  time.Time

	// pending is an operator/door command waiting for this PD's slot. A queued command jumps the
	// round-robin, because the latency that matters here is badge-to-strike.
	pending *cmdReq

	// statusAt is when this PD is next due a status command instead of a bare poll, and
	// statusPhase alternates LSTAT and ISTAT so neither starves the other. See
	// Options.StatusInterval for why a CP that only polls is blind to both.
	statusAt    time.Time
	statusPhase int
	// pollsSinceStatus caps supervision as a FRACTION of this PD's slots, not merely as a rate.
	//
	// The time gate alone is not enough and the failure is nasty. A card read is queued at the PD
	// and handed over on a POLL, so a slot spent on LSTAT is a slot the card waits. On a healthy
	// bus a round takes milliseconds and a one-second status interval costs one slot in twenty.
	// On a DEGRADED bus it does not: every dead reader costs a full reply timeout, so a segment
	// with several dead readers can take longer to go round than the status interval — at which
	// point every single transaction is due a status command and NO CARD IS EVER DELIVERED. The
	// bus looks alive, supervision looks healthy, and nobody can badge in. Requiring polls between
	// status commands bounds supervision at one slot in four however slow the round becomes.
	pollsSinceStatus int
	// haveLocal/lastTamper/lastPowerFail and haveInputs/lastInputs hold the last reading, so a
	// status event is emitted on a CHANGE rather than on every poll.
	//
	// The edge is not an optimisation. A reader whose case is open answers LSTAT with the tamper
	// bit set for as long as it stays open, so a level-triggered alarm would republish once a
	// second forever — and alarm.go's own argument against paging on a flaky segment applies with
	// far more force to a thousand identical alarms an hour: "the failure mode worth avoiding
	// is an alarm nobody believes".
	haveLocal                 bool
	lastTamper, lastPowerFail bool
	haveInputs                bool
	lastInputs                []bool
}

func newPDState(addr uint8, now time.Time) *pdState {
	return &pdState{address: addr, status: StatusOffline, resetSeq: true, lastGoodAt: now}
}

// nextSeq advances this PD's sequence number, honouring a pending session reset.
//
// An OFFLINE reader always gets sequence 0. You cannot be mid-session with a device that is not
// answering: while it is away the CP's counter keeps advancing and the reader's does not, so
// resuming with an ordinary number earns a NAK on the very first frame after recovery. Sending the
// session-start value until it answers makes reconnection deterministic instead of costing a
// wasted round-trip every time a reader is power-cycled.
func (p *pdState) nextSeq() uint8 {
	if p.resetSeq || p.status == StatusOffline {
		p.resetSeq = false
		p.seq = 0
		return 0
	}
	p.seq = NextSequence(p.seq)
	return p.seq
}

// nextCommand decides what to send in this PD's slot: a queued command, the next identification
// step, a due status command, or a poll.
func (p *pdState) nextCommand(now time.Time, statusEvery time.Duration) (Command, []byte) {
	if p.pending != nil {
		return p.pending.code, p.pending.data
	}
	switch p.status {
	case StatusOffline, StatusIdentifying:
		if p.info.Serial == 0 {
			return CmdID, []byte{0x00}
		}
		if p.caps == nil {
			return CmdCap, []byte{0x00}
		}
		return CmdPoll, nil
	default:
		if c, ok := p.dueStatus(now, statusEvery); ok {
			p.pollsSinceStatus = 0
			return c, nil
		}
		p.pollsSinceStatus++
		return CmdPoll, nil
	}
}

// minPollsPerStatus is how many polls must pass between two status commands for one reader. Four
// transactions per status caps supervision at a quarter of a reader's slots in the worst case.
const minPollsPerStatus = 3

// dueStatus returns the supervision command this PD owes, if its interval has elapsed.
//
// LSTAT and ISTAT alternate rather than both going out each round: they answer different
// questions (is somebody at the reader / is somebody at the door) and interleaving them costs one
// slot per interval instead of two. ISTAT is skipped entirely on a reader whose PDCAP says it
// supervises no inputs — asking a keypad-only reader for a door contact earns a NAK every second,
// and a NAK is a failed transaction, which is what declares a healthy reader offline.
func (p *pdState) dueStatus(now time.Time, every time.Duration) (Command, bool) {
	if p.statusAt.IsZero() {
		// Do not fire on the very first slot after coming online: the identification exchange has
		// only just finished and the first thing a door wants from a fresh reader is a poll.
		p.statusAt = now.Add(every)
		return 0, false
	}

	if now.Before(p.statusAt) || p.pollsSinceStatus < minPollsPerStatus {
		return 0, false
	}
	p.statusAt = now.Add(every)
	if p.inputCount() > 0 {
		p.statusPhase++
		if p.statusPhase%2 == 0 {
			return CmdIStat, true
		}
	}
	return CmdLStat, true
}

// inputCount reports how many supervised inputs this reader claims in PDCAP capability
// CapContactStatus. Zero means it has none, so it is never asked for input status.
func (p *pdState) inputCount() int {
	for _, c := range p.caps {
		if c.Function == CapContactStatus {
			return int(c.NumItems)
		}
	}
	return 0
}

// wantsSecureChannel reports whether this reader should be running a session but is not.
//
// Identification comes FIRST — before the handshake — for a concrete reason: DiversifySCBK needs
// the reader's serial, and PDCAP capability 9 tells us whether the reader can do Secure Channel at
// all. Challenging a reader we cannot yet name means guessing at both.
func (p *pdState) wantsSecureChannel(now time.Time) bool {
	if p.scbk == nil || p.caps == nil || p.sc.established() || p.sc.failed() {
		return false
	}
	return now.After(p.scRetryAt) || now.Equal(p.scRetryAt)
}

// secureRetryDelay is the backoff after n consecutive failures to hold a session: exponential from
// base, capped, so a chronically broken reader settles into a quiet retry loop instead of a flap.
func secureRetryDelay(base, max time.Duration, n int) time.Duration {
	d := base
	for i := 1; i < n && d < max; i++ {
		d *= 2
	}
	return min(d, max)
}

// secureChannelUnusable reports that this reader requires Secure Channel and does not have it. A
// door bound to such a reader must be out of service.
func (p *pdState) secureChannelUnusable() bool {
	return p.requireSC && !p.sc.established()
}

// capability returns a PDCAP triple by function code.
func (p *pdState) capability(fn byte) (Capability, bool) {
	for _, c := range p.caps {
		if c.Function == fn {
			return c, true
		}
	}
	return Capability{}, false
}

// security reports what capability 9 says: whether the reader can do Secure Channel at all, and
// whether it is still on the default base key.
func (p *pdState) security() (supported, defaultKey bool) {
	c, ok := p.capability(CapCommSecurity)
	if !ok {
		return false, false
	}
	return c.Compliance&CommSecAES128 != 0, c.NumItems&CommSecDefaultKey != 0
}

// parsePDID decodes a PDID reply payload.
func parsePDID(data []byte) (PDInfo, error) {
	if len(data) < 12 {
		return PDInfo{}, fmt.Errorf("PDID payload is %d bytes, want 12", len(data))
	}
	return PDInfo{
		VendorCode: [3]byte{data[0], data[1], data[2]},
		Model:      data[3],
		Version:    data[4],
		// Serial is little-endian on the wire; reading it big-endian gives every reader a plausible
		// but wrong identity, which is worse than failing outright.
		Serial:        uint32(data[5]) | uint32(data[6])<<8 | uint32(data[7])<<16 | uint32(data[8])<<24,
		FirmwareMajor: data[9], FirmwareMinor: data[10], FirmwareBuild: data[11],
	}, nil
}

// parsePDCap decodes a PDCAP reply payload into capability triples.
func parsePDCap(data []byte) ([]Capability, error) {
	if len(data) == 0 || len(data)%3 != 0 {
		return nil, fmt.Errorf("PDCAP payload is %d bytes, want a non-empty multiple of 3", len(data))
	}
	caps := make([]Capability, 0, len(data)/3)
	for i := 0; i < len(data); i += 3 {
		caps = append(caps, Capability{Function: data[i], Compliance: data[i+1], NumItems: data[i+2]})
	}
	return caps, nil
}

// parseCardRead decodes a RAW reply payload.
func parseCardRead(data []byte) (CardRead, error) {
	if len(data) < 4 {
		return CardRead{}, fmt.Errorf("RAW payload is %d bytes, want at least 4", len(data))
	}
	return CardRead{
		Reader:   data[0],
		Format:   data[1],
		BitCount: uint16(data[2]) | uint16(data[3])<<8,
		Data:     append([]byte(nil), data[4:]...),
	}, nil
}

// parseKeypad decodes a KEYPAD reply payload.
func parseKeypad(data []byte) (byte, []byte, error) {
	if len(data) < 2 {
		return 0, nil, fmt.Errorf("KEYPAD payload is %d bytes, want at least 2", len(data))
	}
	n := int(data[1])
	if n > len(data)-2 {
		n = len(data) - 2
	}
	return data[0], append([]byte(nil), data[2:2+n]...), nil
}
