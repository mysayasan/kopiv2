# Module: infra/access/osdp/cp.go

## Purpose

The CP-side per-PD state and the pure state-transition/decode helpers `bus.go`'s poll loop uses.
`bus.go` owns the serial/TCP port and the round-robin loop; this file owns everything the CP knows
about *one* reader — its lifecycle, its sequence number, its Secure Channel session, its stats —
and the reply-payload parsers shared with the PD side.

## Key Type: PDStatus

`StatusOffline` → `StatusIdentifying` (ID/CAP in flight) → `StatusSecuring` (handshake in flight,
door NOT served) → `StatusOnline`. A reader is deliberately not usable in `Identifying` (trust
rules would be enforced against unknown capabilities) or `Securing` (serving it would mean
answering badges over a channel just decided to require encryption, before it exists).

## Key Type: Event / EventKind

`EventCard`, `EventKeypad`, `EventOnline`, `EventOffline`, `EventStatus`, `EventFault`. Card reads
arrive as `EventCard` and nowhere else — a PD hands one over unprompted on a poll reply, so there
is no call that returns a card and never will be. `Event.SecureSession`/`DefaultKeySession` (on
`EventOnline`) are the *observed* fact of an established, keyed session, distinct from
`SupportsSecureChannel`/`DefaultKey` (the reader's mere capability claim) — only the observed fact
is worth binding a `critical` door to.

## Key Type: PDStats

Per-reader counters (`Transactions`, `Timeouts`, `CrcErrors`, `Naks`, `Busy`, `SequenceErrs`,
`Offlines`, `SecureFailures`, `UnframedBytes`) — the numbers that make a sick RS-485 segment
visible before it becomes an outage (a rising `CrcErrors` well before any reader actually drops
offline), and that separate "two PDs sharing one address" (`UnframedBytes` against a reader that
never answers) from an ordinary fault.

## Key Type: pdState

Everything the CP knows about one reader: `status`, `seq`/`resetSeq` (per-PD, never shared across
the bus — reusing a number a PD has already seen makes it replay its previous reply, silently
swallowing a queued card read), `info`/`caps`, `failures`/`announced`/`lastGoodAt` (see
`secureChannelUnusable` below and `bus.go`'s `checkSupervision`), the Secure Channel fields
(`scbk`, `requireSC`, `sc`, `scNext`, `scFailures`/`scRetryAt` backoff), and `pending` (a
queued operator/door command that jumps the round-robin, because badge-to-strike latency matters).

## Responsibilities

- `nextSeq()` — an OFFLINE reader always gets sequence 0 (the session-start value); you cannot be
  mid-session with a device that has not answered, since the CP's counter keeps advancing while
  the reader's does not.
- `nextCommand()` — queued command, else the next identification step (`ID` then `CAP`), else
  `Poll`.
- `wantsSecureChannel(now)` — true only once identification has completed, because
  `DiversifySCBK` needs the reader's serial and PDCAP capability 9 says whether Secure Channel is
  even possible; challenging an unidentified reader means guessing at both.
- `secureRetryDelay(base, max, n)` — exponential backoff after `n` consecutive session failures, so
  a reader that establishes then immediately loses a session settles into a quiet retry loop
  instead of flapping a door in and out of service several times a second.
- `secureChannelUnusable()` — true when the door requires Secure Channel and it is not established;
  the fail-closed gate `bus.go`'s `credentialsBlocked` checks before delivering a card/PIN.
- `parsePDID`/`parsePDCap`/`parseCardRead`/`parseKeypad` — decode `RplPDID`/`RplPDCap`/`RplRaw`/
  `RplKeypad` payloads. Shared verbatim with `pd.go`'s encode side.

## Notes

- `bus.go` is the only caller of everything in this file; `cp.go` itself does no I/O.
- Covered indirectly by `bus_test.go` (state-machine behaviour exercised through `Bus`).
