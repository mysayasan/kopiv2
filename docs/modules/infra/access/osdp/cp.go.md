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

`EventCard`, `EventKeypad`, `EventOnline`, `EventOffline`, `EventStatus`, `EventInput`,
`EventFault`. An `EventFault` also carries a **`Fault FaultKind`** — `FaultProtocol` (framing,
sequencing, addressing, decode: the reader is present and the conversation with it is broken) or
`FaultSecureChannel` (the encrypted session could not be established, was lost, or a credential
arrived on a reader required to have one). The zero value is `FaultProtocol`, deliberately: an
unclassified fault is reported as a wiring problem rather than a security one, because
over-reporting security faults is how an operator learns to ignore them. It exists because
`mypintusan` had no way to tell the two apart and titled every one of them "Reader secure channel
fault" — a skewed sequence number on a reader with no Secure Channel configured at all sent an
installer hunting for a bus tap. See `MYPINTUSAN_OSDP_PLAN.md` §12. `EventStatus` carries the LSTATR flags (`Tamper`, `PowerFail`); `EventInput` carries
the ISTATR contact states (`Inputs`), where **input 0 is the door-position contact and true means
open** — a convention, not something OSDP states, since the protocol reports an input's electrical
state and leaves the meaning to the installer (per-input polarity is not yet a reader-profile
setting). Both are edge-triggered by `bus.go`: they arrive when the reading CHANGED, so a reader
whose case is left open reports once rather than once per status interval forever. Card reads
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
- `nextCommand(now, statusEvery)` — queued command, else the next identification step (`ID` then
  `CAP`), else a due status command, else `Poll`.
- `dueStatus(now, every)` / `inputCount()` — the supervision scheduler, and the reason `tamper`,
  `door-forced` and `door-held-open` could not fire on any installation before it existed. A PD
  answers `POLL` with an ACK or a queued card read and **never volunteers a status change**, so a
  CP that only polls learns nothing about an enclosure being opened or a door being pushed. LSTAT
  and ISTAT alternate (they answer different questions and interleaving costs one slot per interval
  instead of two), and ISTAT is skipped entirely on a reader whose PDCAP claims no supervised
  inputs — asking a keypad-only reader for a door contact earns a NAK every second, and a NAK is a
  failed transaction, which is what declares a healthy reader offline.
- `pollsSinceStatus` / `minPollsPerStatus` — supervision is capped as a FRACTION of a reader's
  slots (one in four), not merely as a rate. A card read is queued at the PD and handed over on a
  `POLL`, so a slot spent on LSTAT is a slot the card waits. That is a rounding error on a healthy
  bus and a catastrophe on a sick one: every dead reader costs a full reply timeout, so a degraded
  segment can take longer to go round than `StatusInterval` — at which point a purely time-gated
  scheduler sends a status command every single time and **no badge is ever delivered**, while the
  bus looks up and supervision looks healthy. Pinned by
  `TestBusStatusNeverStarvesCardDelivery`.
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
