package osdp

import (
	"bytes"
	"errors"
	"fmt"
)

// The OSDP packet, per MYPINTUSAN_OSDP_PLAN.md §2.1:
//
//	┌──────┬──────────┬─────────┬─────────┬─────────┬─────────┬──────────┬──────────┬───────┐
//	│ SOM  │ ADDRESS  │ LEN_LSB │ LEN_MSB │  CTRL   │  [SCB]  │ CMND/RPY │  DATA…   │  CRC  │
//	│ 0x53 │  0–0x7F  │      total len     │         │  opt.   │          │          │ 2 B   │
//	└──────┴──────────┴─────────┴─────────┴─────────┴─────────┴──────────┴──────────┴───────┘
//
// Framing is genuinely easier than Modbus RTU here: LEN is explicit and covers the whole packet, so
// none of the response-length inference that infra/iot/modbus/transport.go has to do applies. A
// reader either hands us a length we can trust or a frame we can discard.
const (
	// SOM is the start-of-message mark. Resync after garbage means scanning for this byte.
	SOM = 0x53

	// MaxAddress is the highest PD address. 127 (0x7F) is the configuration/broadcast address;
	// real PDs live at 0–126.
	MaxAddress = 0x7F
	// BroadcastAddress reaches every PD on the bus. Useful for discovery, dangerous for anything
	// else on a multi-drop bus: every PD answers at once and the replies collide.
	BroadcastAddress = 0x7F

	// MinFrameSize is SOM + address + 2 length + control + code + 2 CRC.
	MinFrameSize = 8
	// MaxFrameSize matches libosdp's packet buffer. Anything claiming more is line noise that
	// happened to contain a 0x53, not a packet.
	MaxFrameSize = 1024

	// replyBit is set in the address byte on PD->CP traffic. It exists so a PD never acts on
	// another PD's reply and a CP never accepts another CP's command; on a half-duplex bus you
	// hear your own transmission, so without it a CP would parse its own POLL as a reply.
	replyBit = 0x80

	ctrlSequenceMask = 0x03 // sequence number, cycles 1→2→3→1
	ctrlUseCRC       = 0x04 // clear = legacy 8-bit checksum
	ctrlHasSCB       = 0x08 // Security Control Block present
)

// Security Control Block types. SCS_11..SCS_14 carry the handshake; SCS_15..SCS_18 mark established
// sessions, split by whether the payload is merely MAC'd or fully encrypted.
const (
	SCS11 = 0x11 // CHLNG  — handshake, CP->PD
	SCS12 = 0x12 // CCRYPT — handshake, PD->CP
	SCS13 = 0x13 // SCRYPT — handshake, CP->PD
	SCS14 = 0x14 // RMAC_I — handshake, PD->CP
	SCS15 = 0x15 // established, no data, MAC only (CP->PD)
	SCS16 = 0x16 // established, no data, MAC only (PD->CP)
	SCS17 = 0x17 // established, encrypted data (CP->PD)
	SCS18 = 0x18 // established, encrypted data (PD->CP)
)

var (
	ErrNoSOM        = errors.New("osdp: no start-of-message byte")
	ErrShortFrame   = errors.New("osdp: frame shorter than the minimum packet")
	ErrTruncated    = errors.New("osdp: buffer shorter than the declared frame length")
	ErrBadLength    = errors.New("osdp: declared length out of range")
	ErrBadCRC       = errors.New("osdp: CRC mismatch")
	ErrBadChecksum  = errors.New("osdp: checksum mismatch")
	ErrChecksumMode = errors.New("osdp: frame uses the legacy 8-bit checksum, CRC required")
	ErrBadAddress   = errors.New("osdp: address out of range")
	ErrBadSequence  = errors.New("osdp: sequence number out of range")
	ErrBadSCB       = errors.New("osdp: malformed security control block")
)

// Frame is one decoded OSDP packet. The same struct serves both directions; Reply records which
// way it was travelling, taken from (or written into) the address byte's direction bit.
type Frame struct {
	// Address is the PD address with the direction bit already masked off, so it can be compared
	// against a configured address without further arithmetic. Forgetting that mask is a classic
	// "the reader at address 3 never answers" bug.
	Address uint8
	// Reply is true for PD->CP traffic.
	Reply bool
	// Sequence is 0–3. Live traffic cycles 1→2→3→1; 0 is reserved to signal session start or
	// reset, and a PD that receives an unexpected sequence NAKs everything afterwards — a failure
	// that on site looks exactly like a wiring fault. See NextSequence.
	Sequence uint8
	// SCB is the raw Security Control Block including its own leading length byte, or nil when
	// the frame is unsecured. securechannel.go owns its contents; framing only checks that the
	// declared block length matches what is actually there.
	SCB []byte
	// Code is the command byte (CP->PD) or reply byte (PD->CP). Unknown codes decode fine; it is
	// not framing's job to police the vocabulary.
	Code byte
	// Data is the payload after the code byte, excluding the CRC. Empty, never nil, on decode.
	Data []byte
}

// Command returns the frame's code as a Command, for logging a CP->PD frame.
func (f *Frame) Command() Command { return Command(f.Code) }

// ReplyCode returns the frame's code as a Reply, for logging a PD->CP frame.
func (f *Frame) ReplyCode() Reply { return Reply(f.Code) }

// Secure reports whether the frame carried a Security Control Block.
func (f *Frame) Secure() bool { return len(f.SCB) > 0 }

func (f *Frame) String() string {
	dir, code := "CP->PD", f.Command().String()
	if f.Reply {
		dir, code = "PD<-CP", f.ReplyCode().String()
	}
	sc := ""
	if f.Secure() {
		sc = fmt.Sprintf(" scs=%#02x", f.SCB[1])
	}
	return fmt.Sprintf("%s addr=%d seq=%d %s%s data=%d", dir, f.Address, f.Sequence, code, sc, len(f.Data))
}

// NextSequence advances a per-PD sequence number: 1→2→3→1, skipping 0.
//
// Zero is reserved for "start of session" — a CP sends seq 0 to tell a PD to reset its own
// expectation, and the PD's reply to a seq-0 command is the only legitimate seq 0 on the wire.
// Anything that treats this as a plain 2-bit counter will emit a 0 mid-session and the PD will NAK
// with NakBadSequence forever after. This one function is the difference between a working bus and
// the lost day MYPINTUSAN_OSDP_PLAN.md §3.1 warns about.
func NextSequence(cur uint8) uint8 {
	next := (cur + 1) & ctrlSequenceMask
	if next == 0 {
		return 1
	}
	return next
}

// SCB builds a Security Control Block: a leading length byte (counting itself and the type), the
// SCS type, then any block-specific bytes.
func SCB(scsType byte, extra ...byte) []byte {
	b := make([]byte, 0, 2+len(extra))
	b = append(b, byte(2+len(extra)), scsType)
	return append(b, extra...)
}

// Marshal encodes the frame, appending the CRC. It always sets the CRC control bit: the legacy
// 8-bit checksum is decodable (so a misconfigured PD can be diagnosed) but never transmitted.
func (f *Frame) Marshal() ([]byte, error) {
	if f.Address > MaxAddress {
		return nil, fmt.Errorf("%w: %d", ErrBadAddress, f.Address)
	}
	if f.Sequence > ctrlSequenceMask {
		return nil, fmt.Errorf("%w: %d", ErrBadSequence, f.Sequence)
	}
	if n := len(f.SCB); n > 0 {
		if n < 2 || int(f.SCB[0]) != n {
			return nil, fmt.Errorf("%w: block length byte %d, actual %d", ErrBadSCB, f.SCB[0], n)
		}
	}

	total := MinFrameSize + len(f.SCB) + len(f.Data)
	if total > MaxFrameSize {
		return nil, fmt.Errorf("%w: %d bytes", ErrBadLength, total)
	}

	addr := f.Address
	if f.Reply {
		addr |= replyBit
	}
	ctrl := f.Sequence | ctrlUseCRC
	if len(f.SCB) > 0 {
		ctrl |= ctrlHasSCB
	}

	out := make([]byte, 0, total)
	out = append(out, SOM, addr, byte(total), byte(total>>8), ctrl)
	out = append(out, f.SCB...)
	out = append(out, f.Code)
	out = append(out, f.Data...)
	return AppendCRC(out), nil
}

// Unmarshal decodes exactly one frame from buf, which must start at the SOM and be at least as long
// as the frame's declared length. Trailing bytes are ignored so a caller holding a stream buffer can
// hand over what it has; use ScanFrames to find frame boundaries in the first place.
//
// It never panics on malformed input — bad CRC, absurd lengths, an SCB that runs off the end of the
// packet. That is a stated requirement, not a nicety: a tapped or noisy RS-485 line is an untrusted
// input on a life-safety device, and "return garbage / bad CRC" is one of the scripted simulator
// faults in MYPINTUSAN_OSDP_PLAN.md §4.1.
func Unmarshal(buf []byte) (*Frame, error) {
	if len(buf) < MinFrameSize {
		return nil, ErrShortFrame
	}
	if buf[0] != SOM {
		return nil, ErrNoSOM
	}

	total := int(buf[2]) | int(buf[3])<<8
	if total < MinFrameSize || total > MaxFrameSize {
		return nil, fmt.Errorf("%w: %d", ErrBadLength, total)
	}
	if len(buf) < total {
		return nil, ErrTruncated
	}
	frame := buf[:total]

	ctrl := frame[4]
	if ctrl&ctrlUseCRC == 0 {
		// Verify the legacy checksum anyway so the caller can distinguish "a real PD is talking to
		// us in checksum mode" from "this was never a frame".
		body, want := frame[:total-1], frame[total-1]
		if checksum8(body) != want {
			return nil, ErrBadChecksum
		}
		return nil, ErrChecksumMode
	}
	if !CRCOK(frame) {
		return nil, ErrBadCRC
	}

	f := &Frame{
		Address:  frame[1] &^ replyBit,
		Reply:    frame[1]&replyBit != 0,
		Sequence: ctrl & ctrlSequenceMask,
	}

	body := frame[5 : total-2] // between CTRL and the CRC: [SCB] + code + data
	if ctrl&ctrlHasSCB != 0 {
		if len(body) < 2 {
			return nil, fmt.Errorf("%w: no room for a block", ErrBadSCB)
		}
		n := int(body[0])
		// n must cover its own length byte and the SCS type, and still leave a code byte behind.
		if n < 2 || n >= len(body) {
			return nil, fmt.Errorf("%w: block length %d, %d bytes available", ErrBadSCB, n, len(body))
		}
		f.SCB = append([]byte(nil), body[:n]...)
		body = body[n:]
	}
	if len(body) < 1 {
		return nil, ErrShortFrame
	}
	f.Code = body[0]
	f.Data = append([]byte{}, body[1:]...)
	return f, nil
}

// checkOK reports whether a complete candidate frame passes whichever integrity check its CTRL byte
// selected. ScanFrames uses it to decide how far to advance; Unmarshal is still the arbiter of what
// the frame actually means.
func checkOK(frame []byte) bool {
	if len(frame) < MinFrameSize-1 {
		return false
	}
	if frame[4]&ctrlUseCRC == 0 {
		return checksum8(frame[:len(frame)-1]) == frame[len(frame)-1]
	}
	return CRCOK(frame)
}

// ScanFrames is a bufio.SplitFunc that yields candidate OSDP packets from a byte stream, discarding
// bytes that cannot begin one. Tokens are candidates, not guarantees: a token can still fail
// Unmarshal, and callers MUST tolerate that rather than treating it as a fatal stream error.
//
// Resync matters more here than in a request/response protocol. The CP holds the port open for the
// life of the process and polls continuously, so a burst of noise — a reader powering up, a strike's
// back-EMF coupling into the pair — must not desynchronise the bus permanently. Two rules do that
// work, and the second is not obvious:
//
//  1. A candidate SOM whose length field is implausible is skipped one byte at a time, rather than
//     trusted and allowed to consume the real frames behind it.
//  2. A candidate that is complete but fails its CRC also only advances ONE byte. This is the case
//     a truncated frame creates: the tail of a cut-off packet declares a plausible length and then
//     borrows the first bytes of the *next*, perfectly good frame to reach it. Advancing by the
//     declared length there would discard a valid frame every time a partial one arrived — a
//     one-frame loss that on a polled bus reads as an intermittently unresponsive reader.
//
// Bad candidates are still emitted rather than silently swallowed, deliberately: CRC failure rate is
// the primary diagnostic for a sick RS-485 segment (missing termination, a ground loop from
// shielding both ends), and a framer that hides those errors hides the bus fault with them.
func ScanFrames(data []byte, atEOF bool) (advance int, token []byte, err error) {
	for off := 0; ; {
		i := bytes.IndexByte(data[off:], SOM)
		if i < 0 {
			// No candidate at all: drop the lot (a trailing partial SOM is impossible — a SOM is
			// one byte) and ask for more.
			return len(data), nil, nil
		}
		off += i

		if len(data)-off < 4 {
			if atEOF {
				return len(data), nil, nil
			}
			return off, nil, nil // keep the candidate, wait for its length field
		}
		total := int(data[off+2]) | int(data[off+3])<<8
		if total < MinFrameSize-1 || total > MaxFrameSize {
			off++ // that 0x53 was noise, not a header (checksum-mode frames are one byte shorter)
			continue
		}
		if len(data)-off < total {
			if atEOF {
				return len(data), nil, nil
			}
			return off, nil, nil // wait for the rest of the packet
		}
		candidate := data[off : off+total]
		if !checkOK(candidate) {
			return off + 1, candidate, nil // rule 2: report it, but resync from the next byte
		}
		return off + total, candidate, nil
	}
}
