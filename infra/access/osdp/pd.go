package osdp

import "time"

// The PD (Peripheral Device) side of OSDP — the reader's half of the conversation.
//
// mypintusan is a CP and will never ship a PD. This exists so the simulator and the tests have a
// reader that answers correctly, and — the point of putting it here rather than in tools/ — so the
// simulator and the CP are decoded by the same code. A simulator with its own private frame parser
// is a simulator that agrees with itself and disagrees with the wire.
//
// PD does no I/O and never sleeps. It is a pure function from an inbound byte slice to an outbound
// one (or to nil, meaning "say nothing"), which is what lets the whole fault matrix in
// MYPINTUSAN_OSDP_PLAN.md §4.1 be exercised in a unit test instead of on a bench.

// PDInfo is the nameplate returned in a PDID reply.
type PDInfo struct {
	VendorCode                                  [3]byte // IEEE OUI
	Model                                       byte
	Version                                     byte
	Serial                                      uint32
	FirmwareMajor, FirmwareMinor, FirmwareBuild byte
}

// Capability is one PDCAP triple: what the reader can do, how well, and how many of them it has.
type Capability struct {
	Function   byte // CapContactStatus, CapCommSecurity, …
	Compliance byte
	NumItems   byte
}

// PDCAP function codes. Only the ones the CP acts on are named — the rest pass through as numbers,
// which is enough for a diagnostic dump.
const (
	CapContactStatus  byte = 1
	CapOutputControl  byte = 2
	CapCardDataFormat byte = 3
	CapLEDControl     byte = 4
	CapAudibleOutput  byte = 5
	CapTextOutput     byte = 6
	CapTimeKeeping    byte = 7
	CapCheckCharacter byte = 8
	// CapCommSecurity is the one that matters for the trust model. Compliance bit 0 means the
	// reader supports AES-128 Secure Channel; NumItems bit 0 means it is STILL ON THE DEFAULT
	// BASE KEY (SCBK-D). That second bit is how a CP discovers, without any crypto, that a reader
	// is not actually secure — which the hardware plan §3.1 turns into a hard cap at `interior`
	// doors until a KEYSET lands.
	CapCommSecurity  byte = 9
	CapReceiveBuffer byte = 10
	CapCombinedMsg   byte = 11
	CapSmartCard     byte = 12
	CapReaders       byte = 13
	CapBiometrics    byte = 14
)

// Secure-channel capability bits, for readability at the call sites above.
const (
	CommSecAES128     byte = 0x01 // compliance: supports Secure Channel
	CommSecDefaultKey byte = 0x01 // num items: still on SCBK-D
)

// Faults scripts a misbehaving reader. Every field here corresponds to a row of the fault table in
// MYPINTUSAN_OSDP_PLAN.md §4.1, and the reason they exist is blunt: you cannot make a real reader
// misbehave on demand. A card that arrives late, a PD that NAKs the sequence, a reader that goes
// silent mid-session — each is a failure the CP must survive, and none can be provoked from a
// vendor's firmware.
type Faults struct {
	// Silent stops the PD replying at all: a cut bus, a dead reader, a PSU brownout. Exercises
	// offline supervision and the degraded-mode alert.
	Silent bool
	// Busy replies BUSY to the next N commands. BUSY means "ask me again", not "I am broken"; a CP
	// that counts it as a failure declares healthy readers dead under load.
	Busy int
	// BadCRC corrupts the CRC of every reply, and Garbage replaces the reply with junk. Both prove
	// the decoder does not panic and that framing resynchronises.
	BadCRC  bool
	Garbage bool
	// SequenceSkew adds to the reply's sequence number, exercising the CP's own sequence
	// validation. The §3.1 trap seen from the other side.
	SequenceSkew int
	// RefuseSecureChannel makes the PD NAK the Secure Channel handshake. This is THE
	// security-critical fault: a door with RequireSecureChannel must go out of service and alarm,
	// never quietly fall back to cleartext.
	RefuseSecureChannel bool
	// NoSecureChannel drops the AES-128 bit from PDCAP: a reader that cannot do SC at all, as
	// opposed to one that can but won't.
	NoSecureChannel bool
	// DropSecureChannel tears down an ALREADY ESTABLISHED session on the next secure frame. This is
	// the harder half of the security-critical pair: refusing a handshake is caught at enrolment,
	// but losing a session mid-conversation happens to a reader that is already trusted and
	// already bound to a live door. A real reader will not do this on request.
	DropSecureChannel bool
	// DefaultSCBK reports SCBK-D still in use via PDCAP, so the trust cap and the UI nag can be
	// tested without owning an un-rekeyed reader.
	DefaultSCBK bool
	// Tamper asserts the tamper input, driving LSTATR/RSTATR down the alarm path.
	Tamper bool
	// PowerFail asserts the power-failure input in LSTATR.
	PowerFail bool
	// ReplyDelay is applied by the TRANSPORT, not by PD — PD never sleeps. The simulator reads it
	// and waits before writing, to prove a slow PD does not starve the others on a multi-drop bus.
	ReplyDelay time.Duration
}

// CardRead is a credential presented at the reader, delivered to the CP on the next poll.
type CardRead struct {
	Reader   byte   // reader number on this PD, almost always 0
	Format   byte   // 0 = raw/unspecified, 1 = P/data/P Wiegand
	BitCount uint16 // 26 for standard Wiegand, 34 for the extended format, 32+ for raw UIDs
	Data     []byte // MSB first, left-aligned in the final byte
}

// PD is a simulated reader. Construct with NewPD; mutate Faults at any time to inject a failure
// mid-session (the "establish Secure Channel then drop it" case needs exactly that).
type PD struct {
	Address uint8
	Info    PDInfo
	Caps    []Capability
	Faults  Faults

	// Inputs/Outputs back the ISTAT/OSTAT replies; a door contact is an input and a strike is an
	// output, so these are what a bound door reads and writes.
	Inputs  []bool
	Outputs []bool

	// SCBK is the base key this reader will accept. Defaults to SCBK-D, which is what a
	// factory-fresh reader actually ships with.
	SCBK SCBK

	pending  []*Frame // queued card reads / keypad entries, drained one per poll
	lastSeq  uint8
	lastRply []byte // replayed when the CP retransmits the same sequence number
	haveSeq  bool
	junk     uint32 // LCG state, so Garbage is reproducible across runs
	sc       *secureChannel
	// scTeardown drops the session AFTER the current reply has been sealed and sent. KEYSET needs
	// it: the ACK is the final message of the session being replaced.
	scTeardown bool
}

// NewPD returns a well-behaved reader at addr: card format capable, LED/buzzer capable, tamper
// capable, and AES-128 capable with a site key already installed. Every deviation from that is an
// explicit Faults field, so a test's intent is visible in the test rather than in a constructor.
func NewPD(addr uint8) *PD {
	return &PD{
		Address: addr,
		Info: PDInfo{
			VendorCode:    [3]byte{0x00, 0x1B, 0x1B},
			Model:         1,
			Version:       1,
			Serial:        0x53494D00 | uint32(addr), // "SIM" + address, so logs identify the sim
			FirmwareMajor: 1, FirmwareMinor: 0, FirmwareBuild: 0,
		},
		Inputs:  []bool{false}, // one door contact
		Outputs: []bool{false}, // one strike relay
		junk:    uint32(addr)*2654435761 + 1,
		// A factory-fresh reader is on the well-known default key. Tests that want a rekeyed
		// reader set SCBK explicitly, which mirrors what a KEYSET does on real hardware.
		SCBK: SCBKDefault,
	}
}

// Secure reports whether this reader currently has an established Secure Channel session.
func (p *PD) Secure() bool { return p.sc.established() }

// cUID is the client identity sent in CCRYPT: the reader's nameplate packed into 8 bytes, so a CP
// can tell which physical reader answered a challenge even before PDID is read.
func (p *PD) cUID() [8]byte {
	i := p.Info
	return [8]byte{
		i.VendorCode[0], i.VendorCode[1], i.VendorCode[2], i.Model,
		byte(i.Serial), byte(i.Serial >> 8), byte(i.Serial >> 16), byte(i.Serial >> 24),
	}
}

// PresentCard queues a credential to be handed over on the next poll.
//
// Note what this signature implies: presenting a card is a PD-side event with no CP involvement.
// The CP cannot ask for it and cannot predict it — which is the whole reason the driver's API has
// to be a channel of events rather than a ReadCard call.
func (p *PD) PresentCard(c CardRead) {
	data := make([]byte, 0, 4+len(c.Data))
	data = append(data, c.Reader, c.Format, byte(c.BitCount), byte(c.BitCount>>8))
	data = append(data, c.Data...)
	p.pending = append(p.pending, &Frame{Code: byte(RplRaw), Data: data})
}

// PresentKeypad queues PIN digits, delivered as a KEYPAD reply on the next poll.
func (p *PD) PresentKeypad(reader byte, digits []byte) {
	data := append([]byte{reader, byte(len(digits))}, digits...)
	p.pending = append(p.pending, &Frame{Code: byte(RplKeypad), Data: data})
}

// Pending reports how many events are queued, so a scenario can wait for the CP to drain them.
func (p *PD) Pending() int { return len(p.pending) }

// Handle decodes one inbound frame and returns the bytes to transmit, or nil for silence.
//
// Silence — rather than an error reply — is the correct response to anything undecodable or
// addressed elsewhere. A PD that answered noise would turn one corrupted frame on a multi-drop bus
// into a storm of collisions from every PD on it.
func (p *PD) Handle(raw []byte) []byte {
	if p.Faults.Silent {
		return nil
	}
	f, err := Unmarshal(raw)
	if err != nil {
		return nil // not a frame we can act on; do not add to the noise
	}
	if f.Reply {
		return nil // another PD's reply, or our own transmission heard on a half-duplex bus
	}
	if f.Address != p.Address && f.Address != BroadcastAddress {
		return nil // addressed to a different PD on the bus
	}

	// A repeated sequence number means the CP did not hear us and is retransmitting. Re-send the
	// EXACT previous bytes rather than re-running the command: replaying an OUT would fire a door
	// strike twice, and replaying a POLL would consume a queued card read that the CP never saw.
	if f.Sequence != 0 && p.haveSeq && f.Sequence == p.lastSeq && p.lastRply != nil {
		return p.lastRply
	}

	// Secure Channel: unwrap established-session traffic before dispatch. Handshake blocks
	// (SCS_11–14) are exempt because they are what establishes the session in the first place.
	plain := f
	if p.sc.established() && !isHandshake(scsType(f)) {
		if p.Faults.DropSecureChannel {
			// A reader that loses its session mid-conversation. The CP must treat this as a
			// security event, not as a reason to carry on in the clear.
			p.sc = nil
			return nil
		}
		if plain, err = p.sc.unseal(raw, f, true); err != nil {
			// Integrity failure on an established session means a fault or a tap on the pair.
			// Tear it down; the CP has to re-handshake, and a door that requires Secure Channel
			// stays out of service until it does.
			p.sc = nil
			return nil
		}
	} else if !p.sc.established() && f.Secure() && !isHandshake(scsType(f)) {
		// Session traffic with no session. Refuse rather than guess.
		plain = nil
	}

	var reply *Frame
	if plain == nil {
		reply = &Frame{Code: byte(RplNak), Data: []byte{byte(NakBadSCB)}}
		p.lastSeq, p.haveSeq = f.Sequence, true
	} else {
		reply = p.dispatch(plain)
	}
	if reply == nil {
		return nil
	}
	handshakeReply := reply.Secure() && isHandshake(scsType(reply))
	reply.Address = p.Address
	reply.Reply = true
	reply.Sequence = uint8((int(f.Sequence) + p.Faults.SequenceSkew) & 0x03)

	var out []byte
	switch {
	case handshakeReply:
		// CCRYPT and RMAC_I carry their own handshake SCB and must go out unsealed — RMAC_I in
		// particular IS the message that establishes the session, so sealing it would require the
		// very session it creates.
		out, err = reply.Marshal()
	case p.sc.established():
		out, err = p.sc.seal(reply, false)
	default:
		out, err = reply.Marshal()
	}
	if err != nil {
		return nil
	}
	if p.scTeardown {
		p.sc, p.scTeardown = nil, false
	}
	if p.Faults.Garbage {
		out = p.garble(out)
	} else if p.Faults.BadCRC {
		out[len(out)-1] ^= 0xFF
	}
	p.lastRply = out
	return out
}

// dispatch applies sequence rules, then answers the command.
func (p *PD) dispatch(f *Frame) *Frame {
	// Sequence 0 means "start of session": the CP is resetting, so forget what we expected. This is
	// the only legitimate 0 on the wire and it is how a CP recovers a wedged PD.
	if f.Sequence == 0 {
		p.lastSeq, p.haveSeq = 0, true
		return p.command(f)
	}
	// Anything other than the next number in the 1→2→3→1 cycle is a protocol violation. NAKing it
	// is what a real PD does, and it is exactly the failure that looks like a wiring fault on site:
	// the bus falls silent, every command is refused, and nothing in the cabling is wrong.
	if p.haveSeq && f.Sequence != NextSequence(p.lastSeq) {
		p.lastSeq = f.Sequence
		return &Frame{Code: byte(RplNak), Data: []byte{byte(NakBadSequence)}}
	}
	p.lastSeq, p.haveSeq = f.Sequence, true

	// BUSY is checked after sequencing so a busy PD still tracks the conversation.
	if p.Faults.Busy > 0 {
		p.Faults.Busy--
		return &Frame{Code: byte(RplBusy)}
	}
	return p.command(f)
}

func (p *PD) command(f *Frame) *Frame {
	switch Command(f.Code) {
	case CmdPoll:
		if len(p.pending) > 0 {
			ev := p.pending[0]
			p.pending = p.pending[1:]
			return ev
		}
		return &Frame{Code: byte(RplAck)}

	case CmdID:
		i := p.Info
		return &Frame{Code: byte(RplPDID), Data: []byte{
			i.VendorCode[0], i.VendorCode[1], i.VendorCode[2],
			i.Model, i.Version,
			byte(i.Serial), byte(i.Serial >> 8), byte(i.Serial >> 16), byte(i.Serial >> 24),
			i.FirmwareMajor, i.FirmwareMinor, i.FirmwareBuild,
		}}

	case CmdCap:
		var data []byte
		for _, c := range p.capabilities() {
			data = append(data, c.Function, c.Compliance, c.NumItems)
		}
		return &Frame{Code: byte(RplPDCap), Data: data}

	case CmdLStat:
		return &Frame{Code: byte(RplLStatR), Data: []byte{b2b(p.Faults.Tamper), b2b(p.Faults.PowerFail)}}

	case CmdIStat:
		data := make([]byte, len(p.Inputs))
		for i, v := range p.Inputs {
			data[i] = b2b(v)
		}
		return &Frame{Code: byte(RplIStatR), Data: data}

	case CmdOStat:
		return &Frame{Code: byte(RplOStatR), Data: p.outputBytes()}

	case CmdRStat:
		// 0 = connected, 1 = not connected, 2 = tamper.
		st := byte(0)
		if p.Faults.Tamper {
			st = 2
		}
		return &Frame{Code: byte(RplRStatR), Data: []byte{st}}

	case CmdOut:
		// [output#, control code, timer LSB, timer MSB]. Control codes 1/2 are permanent off/on,
		// 3/4 are timed. This is the command that throws a door strike.
		if len(f.Data) >= 2 {
			n := int(f.Data[0])
			if n < len(p.Outputs) {
				switch f.Data[1] {
				case 1:
					p.Outputs[n] = false
				case 2, 3, 4:
					p.Outputs[n] = true
				}
			}
		}
		return &Frame{Code: byte(RplOStatR), Data: p.outputBytes()}

	case CmdLED, CmdBuz, CmdText:
		return &Frame{Code: byte(RplAck)}

	case CmdComSet:
		// [new address, baud LSB..MSB]. The PD adopts the new address IMMEDIATELY, so the CP's
		// very next poll must already use it — the out-of-box "two readers at address 0" fix, and
		// a step that is easy to get wrong by continuing to poll the old address.
		if len(f.Data) >= 5 {
			p.Address = f.Data[0] &^ replyBit
			out := &Frame{Code: byte(RplCom), Data: append([]byte(nil), f.Data[:5]...)}
			return out
		}
		return &Frame{Code: byte(RplNak), Data: []byte{byte(NakBadCommand)}}

	case CmdChlng:
		if p.Faults.RefuseSecureChannel || p.Faults.NoSecureChannel {
			return &Frame{Code: byte(RplNak), Data: []byte{byte(NakBadSCB)}}
		}
		p.sc = newSecureChannel(p.SCBK)
		rep, err := p.sc.answerCHLNG(p.Address, p.cUID(), f.Data)
		if err != nil {
			p.sc = nil
			return &Frame{Code: byte(RplNak), Data: []byte{byte(NakBadSCB)}}
		}
		return rep

	case CmdSCrypt:
		if p.sc == nil || p.Faults.RefuseSecureChannel {
			return &Frame{Code: byte(RplNak), Data: []byte{byte(NakBadSCB)}}
		}
		rep, err := p.sc.answerSCRYPT(p.Address, f.Data)
		if err != nil {
			// A wrong server cryptogram means the CP does not hold our base key. Refuse and forget
			// the half-built session rather than leaving it to be completed later.
			p.sc = nil
			return &Frame{Code: byte(RplNak), Data: []byte{byte(NakEncRequired)}}
		}
		return rep

	case CmdKeySet:
		// Installing a new base key is only permitted INSIDE an established session. Accepting it
		// in the clear would broadcast the site key to anyone tapping the pair, which defeats the
		// entire point of rekeying away from SCBK-D — and rekeying is exactly what a reader on the
		// default key is about to be asked to do.
		if !p.sc.established() {
			return &Frame{Code: byte(RplNak), Data: []byte{byte(NakEncRequired)}}
		}
		// [key type, key length, key bytes…]
		if len(f.Data) < 2+16 || f.Data[1] != 16 {
			return &Frame{Code: byte(RplNak), Data: []byte{byte(NakBadCommand)}}
		}
		copy(p.SCBK[:], f.Data[2:18])
		// The session was built on the OLD key and dies here — but NOT until this ACK has gone out.
		// The acknowledgement is the last message of the old session and must still be sealed with
		// it; tearing down first sends a cleartext reply on a secure session, which the CP correctly
		// treats as a downgrade attack and refuses. Deferred teardown, handled after marshalling.
		p.scTeardown = true
		return &Frame{Code: byte(RplAck)}

	default:
		return &Frame{Code: byte(RplNak), Data: []byte{byte(NakBadCommand)}}
	}
}

// capabilities returns the PDCAP triples, with the Faults applied to the security capability.
func (p *PD) capabilities() []Capability {
	if p.Caps != nil {
		return p.Caps
	}
	sec := Capability{Function: CapCommSecurity, Compliance: CommSecAES128}
	if p.Faults.NoSecureChannel {
		sec.Compliance = 0
	}
	if p.Faults.DefaultSCBK {
		sec.NumItems = CommSecDefaultKey
	}
	return []Capability{
		{Function: CapContactStatus, Compliance: 1, NumItems: byte(len(p.Inputs))},
		{Function: CapOutputControl, Compliance: 1, NumItems: byte(len(p.Outputs))},
		{Function: CapCardDataFormat, Compliance: 1, NumItems: 0},
		{Function: CapLEDControl, Compliance: 1, NumItems: 1},
		{Function: CapAudibleOutput, Compliance: 1, NumItems: 1},
		sec,
		// Receive buffer size is reported LSB in Compliance, MSB in NumItems.
		{Function: CapReceiveBuffer, Compliance: byte(MaxFrameSize & 0xFF), NumItems: byte(MaxFrameSize >> 8)},
		{Function: CapReaders, Compliance: 1, NumItems: 1},
	}
}

func (p *PD) outputBytes() []byte {
	data := make([]byte, len(p.Outputs))
	for i, v := range p.Outputs {
		data[i] = b2b(v)
	}
	return data
}

// garble wraps a corrupted copy of the real reply in reproducible junk, salted with stray SOM bytes.
//
// The structure matters, and the first version of this got it wrong: emitting pure random bytes
// produced a burst that essentially never contains a plausible length field, so a CP's scanner
// simply buffers it and the reader presents as SILENT. That is a timeout test, not a resync test,
// and the resync path the `garbage` scenario exists to exercise never ran.
//
// Wrapping a real (but CRC-broken) frame in junk forces the whole path: skip the leading noise,
// latch onto a frame boundary, reject it on the CRC, then skip the trailing noise and recover in
// time for the next poll. Junk on both sides is what separates this from the plain bad-crc fault.
func (p *PD) garble(reply []byte) []byte {
	out := make([]byte, 0, len(reply)+24)
	out = append(out, p.junkBytes(9)...)
	corrupt := append([]byte(nil), reply...)
	corrupt[len(corrupt)-1] ^= 0xA5 // keep the framing, break the CRC
	out = append(out, corrupt...)
	return append(out, p.junkBytes(11)...)
}

// junkBytes returns n bytes of reproducible noise. SOM is salted in deliberately so a scanner meets
// false frame starts, not merely unrecognisable bytes.
func (p *PD) junkBytes(n int) []byte {
	out := make([]byte, n)
	for i := range out {
		p.junk = p.junk*1664525 + 1013904223
		b := byte(p.junk >> 16)
		if b%7 == 0 {
			b = SOM
		}
		out[i] = b
	}
	return out
}

func b2b(v bool) byte {
	if v {
		return 1
	}
	return 0
}
