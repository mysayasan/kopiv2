package osdp

import (
	"bufio"
	"bytes"
	"testing"
)

// cmd builds a CP->PD command frame's bytes, the way a CP will.
func cmd(t *testing.T, addr uint8, seq uint8, c Command, data ...byte) []byte {
	t.Helper()
	raw, err := (&Frame{Address: addr, Sequence: seq, Code: byte(c), Data: data}).Marshal()
	if err != nil {
		t.Fatalf("marshal %s: %v", c, err)
	}
	return raw
}

// ask sends one command and decodes the reply, failing if the PD stayed silent.
func ask(t *testing.T, p *PD, seq uint8, c Command, data ...byte) *Frame {
	t.Helper()
	out := p.Handle(cmd(t, p.Address, seq, c, data...))
	if out == nil {
		t.Fatalf("%s: PD stayed silent", c)
	}
	f, err := Unmarshal(out)
	if err != nil {
		t.Fatalf("%s: reply undecodable % x: %v", c, out, err)
	}
	if !f.Reply {
		t.Fatalf("%s: PD reply did not set the direction bit", c)
	}
	return f
}

// TestPDAnswersTheBasics covers build-order step 2's stated goal: a PD that answers POLL/ID/CAP.
func TestPDAnswersTheBasics(t *testing.T) {
	p := NewPD(1)

	if f := ask(t, p, 1, CmdPoll); Reply(f.Code) != RplAck {
		t.Errorf("idle POLL = %s, want ACK", f.ReplyCode())
	}

	id := ask(t, p, 2, CmdID, 0x00)
	if Reply(id.Code) != RplPDID {
		t.Fatalf("ID = %s, want PDID", id.ReplyCode())
	}
	if len(id.Data) != 12 {
		t.Errorf("PDID payload is %d bytes, want 12", len(id.Data))
	}
	// Serial is little-endian in the reply; getting this backwards would give every simulated
	// reader a different identity than it reports.
	serial := uint32(id.Data[5]) | uint32(id.Data[6])<<8 | uint32(id.Data[7])<<16 | uint32(id.Data[8])<<24
	if serial != p.Info.Serial {
		t.Errorf("PDID serial = %#08x, want %#08x", serial, p.Info.Serial)
	}

	cap := ask(t, p, 3, CmdCap, 0x00)
	if Reply(cap.Code) != RplPDCap {
		t.Fatalf("CAP = %s, want PDCAP", cap.ReplyCode())
	}
	if len(cap.Data)%3 != 0 || len(cap.Data) == 0 {
		t.Fatalf("PDCAP payload is %d bytes, want a non-empty multiple of 3", len(cap.Data))
	}
}

// findCap extracts one capability triple from a PDCAP payload.
func findCap(data []byte, fn byte) (Capability, bool) {
	for i := 0; i+2 < len(data); i += 3 {
		if data[i] == fn {
			return Capability{Function: data[i], Compliance: data[i+1], NumItems: data[i+2]}, true
		}
	}
	return Capability{}, false
}

// TestPDReportsDefaultSCBK is the trust-model test. A reader still on the well-known default base
// key must be discoverable WITHOUT any crypto, because that discovery is what caps it at `interior`
// doors (hardware plan §3.1) before a Secure Channel is ever attempted.
func TestPDReportsDefaultSCBK(t *testing.T) {
	cases := []struct {
		name              string
		faults            Faults
		wantAES, wantDflt bool
	}{
		{"rekeyed reader", Faults{}, true, false},
		{"still on SCBK-D", Faults{DefaultSCBK: true}, true, true},
		{"cannot do secure channel", Faults{NoSecureChannel: true}, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := NewPD(1)
			p.Faults = c.faults
			sec, ok := findCap(ask(t, p, 1, CmdCap, 0x00).Data, CapCommSecurity)
			if !ok {
				t.Fatal("PDCAP omitted the communication-security capability")
			}
			if gotAES := sec.Compliance&CommSecAES128 != 0; gotAES != c.wantAES {
				t.Errorf("AES-128 supported = %v, want %v", gotAES, c.wantAES)
			}
			if gotDflt := sec.NumItems&CommSecDefaultKey != 0; gotDflt != c.wantDflt {
				t.Errorf("default SCBK in use = %v, want %v", gotDflt, c.wantDflt)
			}
		})
	}
}

// TestPDCardReadArrivesOnPoll pins the central mental-model shift from Modbus: a card read is never
// requested, it turns up as the reply to a routine poll.
func TestPDCardReadArrivesOnPoll(t *testing.T) {
	p := NewPD(1)
	card := []byte{0xDE, 0xAD, 0xBE, 0xEF}

	// Before anyone badges, polls are ACKs.
	if f := ask(t, p, 1, CmdPoll); Reply(f.Code) != RplAck {
		t.Fatalf("poll before card = %s, want ACK", f.ReplyCode())
	}

	p.PresentCard(CardRead{Format: 1, BitCount: 26, Data: card})

	f := ask(t, p, 2, CmdPoll)
	if Reply(f.Code) != RplRaw {
		t.Fatalf("poll after card = %s, want RAW", f.ReplyCode())
	}
	if bits := uint16(f.Data[2]) | uint16(f.Data[3])<<8; bits != 26 {
		t.Errorf("bit count = %d, want 26", bits)
	}
	if !bytes.Equal(f.Data[4:], card) {
		t.Errorf("card data = % x, want % x", f.Data[4:], card)
	}

	// The queue drains: the read is delivered exactly once.
	if f := ask(t, p, 3, CmdPoll); Reply(f.Code) != RplAck {
		t.Errorf("second poll re-delivered the card as %s, want ACK", f.ReplyCode())
	}
}

// TestPDSequenceDiscipline is the §3.1 trap, tested from the PD side. Every case here is a bug that
// on real hardware presents as a silent bus and sends an installer looking for a cabling fault.
func TestPDSequenceDiscipline(t *testing.T) {
	t.Run("correct cycle is accepted", func(t *testing.T) {
		p := NewPD(1)
		seq := uint8(0)
		for i := 0; i < 12; i++ {
			seq = NextSequence(seq)
			if f := ask(t, p, seq, CmdPoll); Reply(f.Code) == RplNak {
				t.Fatalf("step %d (seq %d) NAKed a correctly sequenced poll", i, seq)
			}
		}
	})

	t.Run("skipped number is NAKed", func(t *testing.T) {
		p := NewPD(1)
		ask(t, p, 1, CmdPoll)
		f := ask(t, p, 3, CmdPoll) // 2 was skipped
		if Reply(f.Code) != RplNak {
			t.Fatalf("out-of-order sequence = %s, want NAK", f.ReplyCode())
		}
		if len(f.Data) != 1 || NakCode(f.Data[0]) != NakBadSequence {
			t.Errorf("NAK code = % x, want bad-sequence (%#02x)", f.Data, byte(NakBadSequence))
		}
	})

	t.Run("sequence 0 resets a wedged session", func(t *testing.T) {
		p := NewPD(1)
		ask(t, p, 1, CmdPoll)
		if f := ask(t, p, 3, CmdPoll); Reply(f.Code) != RplNak {
			t.Fatal("setup: expected the skewed poll to NAK")
		}
		// Sequence 0 is the CP's recovery move; the PD must forget what it expected.
		if f := ask(t, p, 0, CmdPoll); Reply(f.Code) == RplNak {
			t.Fatal("sequence 0 did not reset the session — a wedged PD would stay wedged")
		}
		if f := ask(t, p, 1, CmdPoll); Reply(f.Code) == RplNak {
			t.Fatal("session did not resume cleanly after a reset")
		}
	})
}

// TestPDRetransmitReplaysNotReruns is the one that protects a door. A CP that misses a reply
// retransmits the same sequence number; if the PD re-RAN the command instead of replaying its
// answer, an OUT would fire the strike a second time and a POLL would swallow a card read the CP
// never received.
func TestPDRetransmitReplaysNotReruns(t *testing.T) {
	t.Run("poll does not lose a queued card", func(t *testing.T) {
		p := NewPD(1)
		p.PresentCard(CardRead{Format: 1, BitCount: 26, Data: []byte{0x01, 0x02, 0x03, 0x04}})

		first := p.Handle(cmd(t, 1, 1, CmdPoll))
		again := p.Handle(cmd(t, 1, 1, CmdPoll)) // CP did not hear us; it retries
		if !bytes.Equal(first, again) {
			t.Fatalf("retransmit produced different bytes:\n first % x\n again % x", first, again)
		}
		if p.Pending() != 0 {
			t.Error("card queue changed on a retransmit")
		}
		// And the card is not delivered a third time under a fresh sequence number.
		if f := ask(t, p, 2, CmdPoll); Reply(f.Code) != RplAck {
			t.Errorf("next poll = %s, want ACK (the card was already delivered)", f.ReplyCode())
		}
	})

	t.Run("output is not fired twice", func(t *testing.T) {
		p := NewPD(1)
		p.Outputs = []bool{false}
		fires := 0
		// Drive the strike, then retransmit the identical command.
		p.Handle(cmd(t, 1, 1, CmdOut, 0x00, 0x02, 0x00, 0x00))
		if p.Outputs[0] {
			fires++
		}
		p.Outputs[0] = false // observe whether a replay re-energises it
		p.Handle(cmd(t, 1, 1, CmdOut, 0x00, 0x02, 0x00, 0x00))
		if p.Outputs[0] {
			t.Error("a retransmitted OUT re-fired the strike — a replayed unlock is a door opening twice")
		}
		if fires != 1 {
			t.Errorf("the original OUT fired %d times, want 1", fires)
		}
	})
}

// TestPDFaultMatrix walks the scripted faults from MYPINTUSAN_OSDP_PLAN.md §4.1 that are
// expressible at this layer. The assertion in every case is the same: the PD misbehaves exactly as
// scripted, and nothing in our decoder panics on the result.
func TestPDFaultMatrix(t *testing.T) {
	t.Run("silent PD says nothing", func(t *testing.T) {
		p := NewPD(1)
		p.Faults.Silent = true
		if out := p.Handle(cmd(t, 1, 1, CmdPoll)); out != nil {
			t.Errorf("silent PD replied with % x", out)
		}
	})

	t.Run("busy then recovers", func(t *testing.T) {
		p := NewPD(1)
		p.Faults.Busy = 2
		seq := uint8(0)
		for i, want := range []Reply{RplBusy, RplBusy, RplAck} {
			seq = NextSequence(seq)
			if f := ask(t, p, seq, CmdPoll); Reply(f.Code) != want {
				t.Errorf("poll %d = %s, want %s", i, f.ReplyCode(), want)
			}
		}
	})

	t.Run("bad CRC is rejected not decoded", func(t *testing.T) {
		p := NewPD(1)
		p.Faults.BadCRC = true
		out := p.Handle(cmd(t, 1, 1, CmdPoll))
		if out == nil {
			t.Fatal("expected a (corrupt) reply")
		}
		if _, err := Unmarshal(out); err == nil {
			t.Error("a CRC-corrupted reply decoded successfully")
		}
	})

	// Garbage must exercise RESYNC, not just rejection. Pure random noise almost never contains a
	// plausible length field, so a scanner buffers it and the reader merely looks silent — which is
	// a timeout test wearing a resync test's name. The junk therefore wraps a framed-but-corrupt
	// reply, and this test asserts the scanner actually finds and rejects it.
	t.Run("garbage still frames up so resync is exercised", func(t *testing.T) {
		p := NewPD(1)
		p.Faults.Garbage = true

		framed, seq := 0, uint8(0)
		for i := 0; i < 20; i++ {
			seq = NextSequence(seq)
			out := p.Handle(cmd(t, 1, seq, CmdPoll))
			if out == nil {
				t.Fatal("expected junk bytes, got silence")
			}
			sc := bufio.NewScanner(bytes.NewReader(out))
			sc.Split(ScanFrames)
			for sc.Scan() {
				framed++
				if _, err := Unmarshal(sc.Bytes()); err == nil {
					t.Error("a garbage burst decoded as a valid frame")
				}
			}
		}
		if framed == 0 {
			t.Error("no candidate frame was ever found in the junk — the CP would only ever see a timeout, " +
				"so frame resynchronisation is never exercised")
		}
	})

	t.Run("sequence skew is visible to the CP", func(t *testing.T) {
		p := NewPD(1)
		p.Faults.SequenceSkew = 1
		f := ask(t, p, 1, CmdPoll)
		if f.Sequence == 1 {
			t.Error("skewed PD replied with the correct sequence number")
		}
	})

	t.Run("secure channel refused, never downgraded", func(t *testing.T) {
		p := NewPD(1)
		p.Faults.RefuseSecureChannel = true
		f := ask(t, p, 1, CmdChlng, bytes.Repeat([]byte{0xA5}, 8)...)
		if Reply(f.Code) != RplNak {
			t.Fatalf("CHLNG = %s, want NAK — a refused handshake must fail closed", f.ReplyCode())
		}
	})

	t.Run("tamper surfaces in both status replies", func(t *testing.T) {
		p := NewPD(1)
		p.Faults.Tamper = true
		if f := ask(t, p, 1, CmdLStat); f.Data[0] != 1 {
			t.Errorf("LSTATR tamper byte = %d, want 1", f.Data[0])
		}
		if f := ask(t, p, 2, CmdRStat); f.Data[0] != 2 {
			t.Errorf("RSTATR = %d, want 2 (tamper)", f.Data[0])
		}
	})

	t.Run("power failure surfaces in LSTATR", func(t *testing.T) {
		p := NewPD(1)
		p.Faults.PowerFail = true
		if f := ask(t, p, 1, CmdLStat); f.Data[1] != 1 {
			t.Errorf("LSTATR power byte = %d, want 1", f.Data[1])
		}
	})
}

// TestPDIgnoresTrafficNotItsOwn is the multi-drop rule. Every PD on the bus hears every byte; a PD
// that answered another PD's command would turn a shared pair into a collision generator.
func TestPDIgnoresTrafficNotItsOwn(t *testing.T) {
	p := NewPD(2)

	if out := p.Handle(cmd(t, 5, 1, CmdPoll)); out != nil {
		t.Errorf("PD at address 2 answered a poll for address 5: % x", out)
	}
	// Its own address: answered.
	if out := p.Handle(cmd(t, 2, 1, CmdPoll)); out == nil {
		t.Error("PD ignored a poll addressed to itself")
	}
	// Broadcast: answered (and on a real bus every PD answering at once is the collision the
	// simulator models deliberately).
	if out := p.Handle(cmd(t, BroadcastAddress, 2, CmdPoll)); out == nil {
		t.Error("PD ignored a broadcast")
	}

	// A reply frame — either another PD's, or our own transmission heard back on a half-duplex
	// bus — must never be treated as a command.
	echo, _ := (&Frame{Address: 2, Reply: true, Sequence: 1, Code: byte(RplAck)}).Marshal()
	if out := p.Handle(echo); out != nil {
		t.Errorf("PD replied to a reply frame: % x", out)
	}
}

// TestPDIgnoresNoise proves a PD never answers something it could not decode. A PD that replied to
// corrupt frames would amplify one noise burst into a storm of collisions from every reader on the
// segment — the opposite of what a bus under interference needs.
func TestPDIgnoresNoise(t *testing.T) {
	p := NewPD(1)
	noise := [][]byte{
		nil,
		{0x00},
		{SOM},
		bytes.Repeat([]byte{SOM}, 64),
		bytes.Repeat([]byte{0xFF}, 300),
		func() []byte { b := cmd(t, 1, 1, CmdPoll); b[len(b)-1] ^= 0xFF; return b }(), // bad CRC
		func() []byte { b := cmd(t, 1, 1, CmdPoll); return b[:5] }(),                  // truncated
	}
	for i, n := range noise {
		if out := p.Handle(n); out != nil {
			t.Errorf("noise %d: PD replied with % x", i, out)
		}
	}
	// And it is still healthy afterwards — noise must not wedge the reader.
	if f := ask(t, p, 1, CmdPoll); Reply(f.Code) != RplAck {
		t.Errorf("after a noise burst, poll = %s, want ACK", f.ReplyCode())
	}
}

// TestPDComSetAdoptsNewAddress covers the out-of-box problem: readers ship at address 0, so two new
// readers on one bus collide until one is moved. The PD must adopt the new address IMMEDIATELY —
// a CP that keeps polling the old one after a successful COMSET sees a reader that "disappeared".
func TestPDComSetAdoptsNewAddress(t *testing.T) {
	p := NewPD(0)
	f := ask(t, p, 1, CmdComSet, 0x07, 0x80, 0x25, 0x00, 0x00) // address 7, 9600 baud
	if Reply(f.Code) != RplCom {
		t.Fatalf("COMSET = %s, want COM", f.ReplyCode())
	}
	if f.Data[0] != 0x07 {
		t.Errorf("COM reply address = %d, want 7", f.Data[0])
	}
	if p.Address != 7 {
		t.Fatalf("PD address is %d after COMSET, want 7", p.Address)
	}
	if out := p.Handle(cmd(t, 0, 2, CmdPoll)); out != nil {
		t.Error("PD still answers its old address after COMSET")
	}
	if out := p.Handle(cmd(t, 7, 2, CmdPoll)); out == nil {
		t.Error("PD does not answer its new address after COMSET")
	}
}

// TestPDOutputControl covers the command that actually throws a door strike.
func TestPDOutputControl(t *testing.T) {
	p := NewPD(1)

	f := ask(t, p, 1, CmdOut, 0x00, 0x02, 0x00, 0x00) // permanent on
	if Reply(f.Code) != RplOStatR {
		t.Fatalf("OUT = %s, want OSTATR", f.ReplyCode())
	}
	if f.Data[0] != 1 || !p.Outputs[0] {
		t.Errorf("output not energised: reply % x, state %v", f.Data, p.Outputs)
	}

	f = ask(t, p, 2, CmdOut, 0x00, 0x01, 0x00, 0x00) // permanent off
	if f.Data[0] != 0 || p.Outputs[0] {
		t.Errorf("output not de-energised: reply % x, state %v", f.Data, p.Outputs)
	}

	// An out-of-range output index must be ignored, not panic — the CP's door binding is operator
	// data and can point at a channel the reader does not have.
	if f := ask(t, p, 3, CmdOut, 0x09, 0x02, 0x00, 0x00); Reply(f.Code) != RplOStatR {
		t.Errorf("out-of-range OUT = %s, want OSTATR", f.ReplyCode())
	}
}

// TestPDDoorContact covers the input side: the reed contact that turns "we energised the strike"
// into "the door actually opened", and with it door-forced and door-held-open detection.
func TestPDDoorContact(t *testing.T) {
	p := NewPD(1)
	if f := ask(t, p, 1, CmdIStat); f.Data[0] != 0 {
		t.Errorf("closed contact reported as %d, want 0", f.Data[0])
	}
	p.Inputs[0] = true
	if f := ask(t, p, 2, CmdIStat); f.Data[0] != 1 {
		t.Errorf("open contact reported as %d, want 1", f.Data[0])
	}
}

func TestPDUnknownCommandIsNaked(t *testing.T) {
	p := NewPD(1)
	f := ask(t, p, 1, Command(0xEE))
	if Reply(f.Code) != RplNak {
		t.Fatalf("unknown command = %s, want NAK", f.ReplyCode())
	}
	if NakCode(f.Data[0]) != NakBadCommand {
		t.Errorf("NAK code = %s, want bad-command", NakCode(f.Data[0]))
	}
}

func TestPDKeypadEntry(t *testing.T) {
	p := NewPD(1)
	p.PresentKeypad(0, []byte{'1', '2', '3', '4'})
	f := ask(t, p, 1, CmdPoll)
	if Reply(f.Code) != RplKeypad {
		t.Fatalf("poll after keypad entry = %s, want KEYPAD", f.ReplyCode())
	}
	if f.Data[1] != 4 || !bytes.Equal(f.Data[2:], []byte("1234")) {
		t.Errorf("keypad payload = % x", f.Data)
	}
}
