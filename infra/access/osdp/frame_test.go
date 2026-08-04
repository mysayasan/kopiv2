package osdp

import (
	"bufio"
	"bytes"
	"errors"
	"strings"
	"testing"
	"testing/iotest"
)

// TestCRCCheckValue pins CRC16 against the published check value for CRC-16/AUG-CCITT (poly 0x1021,
// init 0x1D0F, not reflected): the CRC of "123456789" is 0xE5CC.
//
// This is the single most important assertion in the package. Get the init value wrong — 0xFFFF is
// the far more common CCITT variant and the one an autocomplete will offer — and every frame we
// send is malformed, while the symptom on site is a reader that NAKs everything and looks miswired.
func TestCRCCheckValue(t *testing.T) {
	if got := CRC16([]byte("123456789")); got != 0xE5CC {
		t.Fatalf("CRC16(\"123456789\") = %#04x, want 0xe5cc", got)
	}
	// And it must NOT match the Modbus CRC's check value, which shares nothing but a name.
	if got := CRC16([]byte("123456789")); got == 0x4B37 {
		t.Fatal("CRC16 produced the Modbus check value — wrong polynomial/reflection")
	}
}

func TestAppendAndVerifyCRC(t *testing.T) {
	body := []byte{SOM, 0x00, 0x08, 0x00, 0x05, 0x60}
	framed := AppendCRC(body)
	if len(framed) != len(body)+2 {
		t.Fatalf("AppendCRC added %d bytes, want 2", len(framed)-len(body))
	}
	if !CRCOK(framed) {
		t.Error("CRCOK rejected a frame we just CRC'd")
	}
	for i := range body {
		bad := AppendCRC(append([]byte(nil), body...))
		bad[i] ^= 0xFF
		if CRCOK(bad) {
			t.Errorf("CRCOK accepted a frame corrupted at byte %d", i)
		}
	}
	if CRCOK([]byte{0x53}) {
		t.Error("CRCOK accepted a runt buffer")
	}
}

// TestNextSequence pins the 1→2→3→1 cycle. Zero is reserved for session start and must never be
// produced by the advance function.
func TestNextSequence(t *testing.T) {
	cases := []struct{ from, want uint8 }{
		{0, 1}, // after a session-start 0, the first live sequence is 1
		{1, 2},
		{2, 3},
		{3, 1}, // wraps past 0, not through it
	}
	for _, c := range cases {
		if got := NextSequence(c.from); got != c.want {
			t.Errorf("NextSequence(%d) = %d, want %d", c.from, got, c.want)
		}
	}
	// Walk a long session: 0 must never reappear.
	seq := uint8(1)
	for i := 0; i < 100; i++ {
		if seq = NextSequence(seq); seq == 0 {
			t.Fatalf("sequence hit the reserved value 0 after %d steps", i)
		}
	}
}

// TestMarshalPollLayout pins the exact bytes of the most common frame on the bus, so a change to the
// header layout cannot pass unnoticed.
func TestMarshalPollLayout(t *testing.T) {
	f := &Frame{Address: 0, Sequence: 1, Code: byte(CmdPoll)}
	got, err := f.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	want := AppendCRC([]byte{
		SOM,  // start of message
		0x00, // address 0, direction bit clear (CP->PD)
		0x08, // LEN_LSB — 8 bytes total
		0x00, // LEN_MSB
		0x05, // CTRL — sequence 1 | CRC in use (0x04)
		byte(CmdPoll),
	})
	if !bytes.Equal(got, want) {
		t.Errorf("POLL frame = % x, want % x", got, want)
	}
	if len(got) != MinFrameSize {
		t.Errorf("POLL frame is %d bytes, want the %d-byte minimum", len(got), MinFrameSize)
	}
}

// TestDirectionBit covers the bit that stops a CP parsing its own transmission on a half-duplex bus.
func TestDirectionBit(t *testing.T) {
	reply := &Frame{Address: 3, Reply: true, Sequence: 2, Code: byte(RplAck)}
	raw, err := reply.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if raw[1] != 0x83 {
		t.Errorf("reply address byte = %#02x, want 0x83 (addr 3 | reply bit)", raw[1])
	}
	got, err := Unmarshal(raw)
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	// The decoded address must have the direction bit stripped, or comparing it against a
	// configured PD address silently never matches.
	if got.Address != 3 {
		t.Errorf("decoded address = %d, want 3 (direction bit must be masked off)", got.Address)
	}
	if !got.Reply {
		t.Error("decoded frame lost its reply direction")
	}
}

func TestRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		f    Frame
	}{
		{"poll", Frame{Address: 0, Sequence: 1, Code: byte(CmdPoll)}},
		{"poll at max address", Frame{Address: MaxAddress, Sequence: 3, Code: byte(CmdPoll)}},
		{"session start seq 0", Frame{Address: 1, Sequence: 0, Code: byte(CmdPoll)}},
		{"led with data", Frame{Address: 2, Sequence: 2, Code: byte(CmdLED), Data: []byte{0x00, 0x00, 0x01, 0x02, 0x03}}},
		{"card read", Frame{Address: 1, Reply: true, Sequence: 1, Code: byte(RplRaw), Data: []byte{0x00, 0x01, 0x1A, 0x00, 0xDE, 0xAD, 0xBE, 0xEF}}},
		{"nak", Frame{Address: 1, Reply: true, Sequence: 2, Code: byte(RplNak), Data: []byte{byte(NakBadSequence)}}},
		{"secure handshake", Frame{Address: 1, Sequence: 1, SCB: SCB(SCS11, 0x00), Code: byte(CmdChlng), Data: bytes.Repeat([]byte{0xA5}, 8)}},
		{"secure established", Frame{Address: 1, Reply: true, Sequence: 3, SCB: SCB(SCS18), Code: byte(RplAck)}},
		{"max payload", Frame{Address: 1, Sequence: 1, Code: byte(CmdText), Data: bytes.Repeat([]byte{0x5A}, MaxFrameSize-MinFrameSize)}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			raw, err := c.f.Marshal()
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			if n := int(raw[2]) | int(raw[3])<<8; n != len(raw) {
				t.Errorf("LEN field says %d, frame is %d bytes", n, len(raw))
			}
			got, err := Unmarshal(raw)
			if err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			if got.Address != c.f.Address || got.Reply != c.f.Reply || got.Sequence != c.f.Sequence || got.Code != c.f.Code {
				t.Errorf("header round-trip: got %+v, want %+v", got, c.f)
			}
			if !bytes.Equal(got.SCB, c.f.SCB) {
				t.Errorf("SCB = % x, want % x", got.SCB, c.f.SCB)
			}
			if !bytes.Equal(got.Data, c.f.Data) {
				t.Errorf("data = % x, want % x", got.Data, c.f.Data)
			}
		})
	}
}

// TestUnmarshalRejects covers the malformed-input matrix. Every case must return an error rather
// than panic: RS-485 is an untrusted input on a life-safety device, and "return garbage / bad CRC →
// decoder does not panic" is a scripted simulator fault in the OSDP plan §4.1.
func TestUnmarshalRejects(t *testing.T) {
	good, err := (&Frame{Address: 1, Sequence: 1, Code: byte(CmdPoll), Data: []byte{0x01, 0x02}}).Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	corrupt := func(mutate func([]byte) []byte) []byte { return mutate(append([]byte(nil), good...)) }

	cases := []struct {
		name string
		in   []byte
		want error
	}{
		{"empty", nil, ErrShortFrame},
		{"runt", []byte{SOM, 0x00, 0x08}, ErrShortFrame},
		{"no SOM", corrupt(func(b []byte) []byte { b[0] = 0xFF; return b }), ErrNoSOM},
		{"length below minimum", corrupt(func(b []byte) []byte { b[2] = 0x03; return b }), ErrBadLength},
		{"length above maximum", corrupt(func(b []byte) []byte { b[2], b[3] = 0x00, 0xFF; return b }), ErrBadLength},
		{"truncated payload", good[:len(good)-1], ErrTruncated},
		{"flipped payload bit", corrupt(func(b []byte) []byte { b[6] ^= 0xFF; return b }), ErrBadCRC},
		{"flipped CRC byte", corrupt(func(b []byte) []byte { b[len(b)-1] ^= 0xFF; return b }), ErrBadCRC},
		// A frame claiming an SCB whose declared block length overruns the packet: the classic
		// buffer-overrun shape, and the reason SCB extraction is bounds-checked against the body.
		{"SCB overruns the frame", func() []byte {
			f := &Frame{Address: 1, Sequence: 1, SCB: SCB(SCS17), Code: byte(CmdPoll)}
			raw, _ := f.Marshal()
			raw[5] = 0x40 // block length 64 in a 10-byte frame
			return AppendCRC(raw[:len(raw)-2])
		}(), ErrBadSCB},
		{"SCB leaves no code byte", func() []byte {
			f := &Frame{Address: 1, Sequence: 1, SCB: SCB(SCS17), Code: byte(CmdPoll)}
			raw, _ := f.Marshal()
			raw[5] = 0x03 // swallows the code byte
			return AppendCRC(raw[:len(raw)-2])
		}(), ErrBadSCB},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Unmarshal(c.in)
			if !errors.Is(err, c.want) {
				t.Fatalf("Unmarshal error = %v, want %v", err, c.want)
			}
			if got != nil {
				t.Error("a rejected frame must not also return a Frame")
			}
		})
	}
}

// TestChecksumModeIsDiagnosed proves we tell "a PD is talking to us in legacy checksum mode" apart
// from "this is not a frame". Both refuse the frame — we never accept non-CRC traffic — but only one
// of them should send an installer looking for a cabling fault.
func TestChecksumModeIsDiagnosed(t *testing.T) {
	// CTRL 0x01 = sequence 1 with the CRC bit CLEAR. A checksum-mode frame carries one trailing
	// byte instead of two, so this declares 9 bytes total: 8 of header/payload plus the checksum.
	body := []byte{SOM, 0x01, 0x09, 0x00, 0x01, byte(CmdPoll), 0xAA, 0xBB}
	frame := append(body, checksum8(body))

	_, err := Unmarshal(frame)
	if !errors.Is(err, ErrChecksumMode) {
		t.Fatalf("Unmarshal error = %v, want ErrChecksumMode", err)
	}

	frame[len(frame)-1] ^= 0xFF
	if _, err := Unmarshal(frame); !errors.Is(err, ErrBadChecksum) {
		t.Fatalf("corrupt checksum-mode frame: error = %v, want ErrBadChecksum", err)
	}
}

func TestMarshalRejects(t *testing.T) {
	cases := []struct {
		name string
		f    Frame
		want error
	}{
		{"address above 0x7F", Frame{Address: 0x80, Code: byte(CmdPoll)}, ErrBadAddress},
		{"sequence above 3", Frame{Address: 1, Sequence: 4, Code: byte(CmdPoll)}, ErrBadSequence},
		{"payload past the buffer", Frame{Address: 1, Sequence: 1, Code: byte(CmdText), Data: make([]byte, MaxFrameSize)}, ErrBadLength},
		{"SCB length byte lies", Frame{Address: 1, Sequence: 1, SCB: []byte{0x09, SCS17}, Code: byte(CmdPoll)}, ErrBadSCB},
		{"SCB too short to hold a type", Frame{Address: 1, Sequence: 1, SCB: []byte{0x01}, Code: byte(CmdPoll)}, ErrBadSCB},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := c.f.Marshal(); !errors.Is(err, c.want) {
				t.Fatalf("Marshal error = %v, want %v", err, c.want)
			}
		})
	}
}

// TestScanFramesResync feeds a stream that a real bus produces: noise, a truncated frame, a false
// SOM inside a payload, then good traffic. The scanner must recover and deliver exactly the real
// frames — permanent desync after one noise burst would take the whole bus down until restart.
func TestScanFramesResync(t *testing.T) {
	poll, _ := (&Frame{Address: 1, Sequence: 1, Code: byte(CmdPoll)}).Marshal()
	// A card read whose payload deliberately contains a 0x53 byte, to prove the scanner does not
	// treat every SOM-looking byte as a frame start.
	card, _ := (&Frame{Address: 1, Reply: true, Sequence: 2, Code: byte(RplRaw),
		Data: []byte{0x00, 0x01, 0x1A, 0x00, SOM, SOM, 0xBE, 0xEF}}).Marshal()

	var stream []byte
	stream = append(stream, 0xFF, 0x00, 0xAA)      // line noise, no SOM
	stream = append(stream, SOM, 0x01, 0x02, 0x00) // false SOM: length 2, below the minimum
	stream = append(stream, poll[:len(poll)-3]...) // a frame cut off mid-flight
	stream = append(stream, poll...)               // good
	stream = append(stream, 0x53, 0x53)            // bare SOMs with no header behind them
	stream = append(stream, card...)               // good, payload contains 0x53
	stream = append(stream, poll...)               // good

	sc := bufio.NewScanner(bytes.NewReader(stream))
	sc.Split(ScanFrames)

	var decoded []*Frame
	rejected := 0
	for sc.Scan() {
		// A bad token is expected, not fatal: the truncated frame's tail declares a plausible
		// length and borrows the head of the frame behind it. What must NOT happen is that good
		// frame going missing as a result.
		f, err := Unmarshal(sc.Bytes())
		if err != nil {
			rejected++
			continue
		}
		decoded = append(decoded, f)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scanner: %v", err)
	}

	if rejected == 0 {
		t.Error("expected the truncated frame to surface as a rejected token, not vanish silently")
	}
	if len(decoded) != 3 {
		t.Fatalf("decoded %d frames, want 3 (a truncated frame must not eat the good frame behind it)", len(decoded))
	}
	if decoded[0].Code != byte(CmdPoll) || decoded[1].Code != byte(RplRaw) || decoded[2].Code != byte(CmdPoll) {
		t.Errorf("frames out of order: %v", decoded)
	}
	if !bytes.Equal(decoded[1].Data, []byte{0x00, 0x01, 0x1A, 0x00, SOM, SOM, 0xBE, 0xEF}) {
		t.Errorf("card payload mangled: % x", decoded[1].Data)
	}
}

// TestScanFramesByteAtATime proves the split function is genuinely incremental: a serial port hands
// over whatever bytes arrived, frequently one at a time, and a scanner that only works on whole
// buffers works on the bench and fails on the wire.
func TestScanFramesByteAtATime(t *testing.T) {
	poll, _ := (&Frame{Address: 2, Sequence: 3, Code: byte(CmdPoll)}).Marshal()
	card, _ := (&Frame{Address: 2, Reply: true, Sequence: 3, Code: byte(RplRaw), Data: []byte{0xDE, 0xAD}}).Marshal()
	stream := append(append([]byte{}, poll...), card...)

	sc := bufio.NewScanner(iotest.OneByteReader(bytes.NewReader(stream)))
	sc.Split(ScanFrames)
	n := 0
	for sc.Scan() {
		if _, err := Unmarshal(sc.Bytes()); err != nil {
			t.Fatalf("token % x: %v", sc.Bytes(), err)
		}
		n++
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scanner: %v", err)
	}
	if n != 2 {
		t.Fatalf("decoded %d frames from a one-byte-at-a-time reader, want 2", n)
	}
}

// TestScanFramesNeverPanics fuzzes the scanner with structured garbage. It asserts nothing about
// what comes out — only that neither the split function nor the decoder can be crashed by whatever a
// tapped or failing bus puts on the wire.
func TestScanFramesNeverPanics(t *testing.T) {
	seeds := [][]byte{
		{},
		{SOM},
		{SOM, 0x00},
		{SOM, 0x00, 0xFF, 0xFF, 0xFF},
		bytes.Repeat([]byte{SOM}, 300),
		bytes.Repeat([]byte{SOM, 0x00, 0x08, 0x00}, 100),
		bytes.Repeat([]byte{0x00}, 4096),
	}
	for i, seed := range seeds {
		sc := bufio.NewScanner(bytes.NewReader(seed))
		sc.Split(ScanFrames)
		for sc.Scan() {
			_, _ = Unmarshal(sc.Bytes())
		}
		if err := sc.Err(); err != nil && err != bufio.ErrTooLong {
			t.Errorf("seed %d: scanner error %v", i, err)
		}
	}
}

func TestCodeNames(t *testing.T) {
	cases := []struct{ got, want string }{
		{CmdPoll.String(), "POLL"},
		{CmdKeySet.String(), "KEYSET"},
		{RplRaw.String(), "RAW"},
		{RplBusy.String(), "BUSY"},
		{NakBadSequence.String(), "bad-sequence"},
		{Command(0xEE).String(), "CMD(0xee)"}, // unknown codes stay readable in a log
		{Reply(0x01).String(), "RPY(0x01)"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("String() = %q, want %q", c.got, c.want)
		}
	}

	f := &Frame{Address: 1, Reply: true, Sequence: 2, SCB: SCB(SCS18), Code: byte(RplRaw), Data: []byte{1, 2, 3}}
	if s := f.String(); !strings.Contains(s, "RAW") || !strings.Contains(s, "scs=0x18") {
		t.Errorf("Frame.String() = %q, want it to name the reply and the secure block", s)
	}
}
