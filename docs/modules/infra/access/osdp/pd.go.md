# Module: infra/access/osdp/pd.go

## Purpose

The PD (Peripheral Device / reader) side of OSDP — `mypintusan` is a CP and will never ship a PD.
This exists so `tools/osdp-sim` and this package's tests have a reader that answers correctly, and
— the reason it lives here rather than under `tools/` — so the simulator and the production
PD-side decoding logic are literally the same code. A simulator with its own private frame parser
is a simulator that agrees with itself and disagrees with the wire. `PD.Handle` does no I/O and
never sleeps: it is a pure function from an inbound byte slice to an outbound one (or to `nil`,
meaning "say nothing"), which is what lets the whole fault matrix in `MYPINTUSAN_OSDP_PLAN.md` §4.1
run as a unit test instead of on a bench.

## Key Type: PD

```go
type PD struct {
    Address uint8
    Info    PDInfo
    Caps    []Capability
    Faults  Faults
    Inputs, Outputs []bool // door contact / strike relay
    SCBK    SCBK           // base key this reader accepts; defaults to SCBKDefault
    // unexported: pending queue, sequence/replay state, Secure Channel session
}
```

`NewPD(addr)` returns a well-behaved reader: card/LED/buzzer/tamper capable, AES-128 capable, on
`SCBKDefault` (a factory-fresh reader's actual out-of-box state). Every deviation from that is an
explicit `Faults` field, so a test's intent is visible in the test rather than in a constructor.

## Key Type: Faults

One field per row of the OSDP plan §4.1 fault table: `Silent`, `Busy`, `BadCRC`, `Garbage`,
`SequenceSkew`, `RefuseSecureChannel`, `NoSecureChannel`, `DropSecureChannel`, `DefaultSCBK`,
`Tamper`, `PowerFail`, `ReplyDelay`. `RefuseSecureChannel` and `DropSecureChannel` are called out
as THE security-critical faults — refusing a handshake is caught at enrolment, but losing an
established session mid-conversation happens to a reader that is already trusted and bound to a
live door, and a real reader will not do that on request.

## Responsibilities

- `PresentCard`/`PresentKeypad`/`Pending()` — queue a credential/PIN event for the next poll reply.
  Presenting a card is a PD-side event with no CP involvement, which is why the driver's API is a
  channel of events (`bus.go`'s `Events()`) rather than a `ReadCard()` call.
- `Handle(raw []byte) []byte` — decodes one inbound frame, applies sequence/replay/Secure-Channel
  rules, dispatches to `command`, seals the reply if a session is established, and applies
  `Garbage`/`BadCRC` faults to the outgoing bytes. Replays the exact previous reply bytes (not a
  re-run) on a repeated sequence number, since re-running an `OUT` would fire a strike twice and
  re-running a `POLL` would consume a queued card the CP never actually received.
- `command(f *Frame) *Frame` — answers `CmdPoll`/`CmdID`/`CmdCap`/`CmdLStat`/`CmdIStat`/
  `CmdOStat`/`CmdRStat`/`CmdOut`/`CmdLED`/`CmdBuz`/`CmdText`/`CmdComSet`/`CmdChlng`/`CmdSCrypt`/
  `CmdKeySet`. `CmdComSet` adopts the new address **immediately**, so the CP's very next poll must
  already use it. `CmdKeySet` only succeeds inside an established session (accepting a new key in
  the clear would broadcast it to anyone tapping the pair) and defers session teardown until after
  the `ACK` is sealed and sent, since that `ACK` is the last message of the session being replaced.
- `garble(reply []byte) []byte` — wraps a CRC-broken copy of the real reply in reproducible junk.
  Deliberately not pure random noise: an earlier version was, and a CP's frame scanner simply
  buffered it (no plausible length field), so the reader presented as `Silent` — a timeout test
  wearing the resync test's name. Wrapping a real, framed, CRC-broken frame forces the actual
  resync path.
- `parsePDID`/`parsePDCap`/`parseCardRead`/`parseKeypad` — decode the corresponding reply payloads;
  shared with `cp.go`'s `handleReply`, so encode and decode cannot drift apart between the two
  sides that live in this one package.

## Notes

- Secure Channel handling in `Handle` routes through `securechannel.go.md`'s `secureChannel`; a
  `DropSecureChannel` fault discards the session and returns silence rather than a NAK, mirroring
  how a real reader losing its session would go quiet mid-transaction.
- Covered by `pd_test.go`.
