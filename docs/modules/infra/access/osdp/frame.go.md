# Module: infra/access/osdp/frame.go

## Purpose

The OSDP packet: `SOM | ADDRESS | LEN | CTRL | [SCB] | CMND/RPY | DATA | CRC`
(`MYPINTUSAN_OSDP_PLAN.md` §2.1). Framing is genuinely easier than Modbus RTU here — `LEN` is
explicit and covers the whole packet, so none of the response-length inference
`infra/iot/modbus/transport.go` needs applies. `Marshal`/`Unmarshal` never panic on malformed
input by design: a tapped or noisy RS-485 line is untrusted input on a life-safety device, and
"return garbage / bad CRC" is one of the scripted simulator faults in the OSDP plan §4.1.

## Key Type: Frame

```go
type Frame struct {
    Address  uint8  // direction bit already masked off
    Reply    bool   // true for PD->CP traffic
    Sequence uint8  // 0-3; 0 is reserved for session start/reset
    SCB      []byte // Security Control Block, or nil when unsecured
    Code     byte   // command (CP->PD) or reply (PD->CP) code
    Data     []byte // payload after the code byte, excluding the CRC
}
```

Serves both directions; `Reply` records which way it travelled, taken from (or written into) the
address byte's `0x80` direction bit — the bit that stops a PD acting on another PD's reply and a
CP accepting another CP's command on a half-duplex bus where every node hears its own
transmission.

## Responsibilities

- `NextSequence(cur uint8) uint8` — advances 1→2→3→1, skipping 0. Getting this wrong is called out
  in the OSDP plan §3.1 as "the single most likely source of a lost day": a PD that receives an
  unexpected sequence NAKs everything afterwards, and on site that looks exactly like a wiring
  fault.
- `SCB(scsType byte, extra ...byte) []byte` — builds a Security Control Block (leading length byte
  + SCS type + block-specific bytes); `securechannel.go` is the only caller.
- `(*Frame) Marshal() ([]byte, error)` — encodes and appends the CRC. Always sets the CRC control
  bit; the legacy 8-bit checksum is decodable but never transmitted.
- `Unmarshal(buf []byte) (*Frame, error)` — decodes exactly one frame starting at `buf[0]`. Trailing
  bytes are ignored, so a caller holding a stream buffer can hand over what it has.
- `ScanFrames(data []byte, atEOF bool) (advance int, token []byte, err error)` — a
  `bufio.SplitFunc` that finds candidate frames in a byte stream and resyncs after garbage. Two
  rules matter: (1) a candidate SOM with an implausible length is skipped one byte at a time
  rather than trusted, and (2) a candidate that is complete but fails its CRC also advances only
  one byte, not the declared frame length — advancing further there would discard a real frame
  whose first bytes were borrowed to complete a truncated one. Bad candidates are still emitted
  (not swallowed): CRC failure rate is the primary diagnostic for a sick RS-485 segment.
- `Command()`/`ReplyCode()`/`Secure()`/`String()` — logging/introspection helpers on `Frame`.

## Notes

- `crc.go` supplies `AppendCRC`/`CRCOK`/`checksum8`.
- `securechannel.go`'s `seal`/`unseal` operate on the `Frame` this file produces/consumes, and on
  the raw bytes `Marshal`/`Unmarshal` round-trip (a Secure Channel MAC is computed over the wire
  form, not the decoded struct).
- `bus.go`'s `readLoop` runs `ScanFrames` continuously and hands tokens to `Unmarshal`; a token
  that fails to unmarshal is logged and discarded, never treated as a fatal stream error.
- Covered by `frame_test.go`.
