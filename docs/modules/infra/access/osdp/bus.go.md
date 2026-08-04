# Module: infra/access/osdp/bus.go

## Purpose

`Bus` is the CP: it owns one RS-485 segment (or one TCP connection to a simulator/gateway) and
polls every reader on it, forever. Three shape decisions here are explicit divergences from
`infra/iot/modbus`, called out in `MYPINTUSAN_OSDP_PLAN.md` §3.1: (1) the port is opened **once**
and held for the process lifetime — `DialSerial`-style open/poll/close would tear down the Secure
Channel session on every tick and add latency to every badge; (2) card reads leave by **channel**
(`Events()`), not by return value — a PD hands one over unprompted when a human badges, so the CP
can never simply ask for one; (3) sequence numbers are per-PD (`pdState.seq`, `cp.go.md`), never
per-bus.

## Key Type: Bus / Options

```go
type Bus struct {
    port Transport
    pds  map[uint8]*pdState
    // round-robin order, event/command channels, dropped-event counter
}
```

`Options` (with a `Defaults()` filling zero values): `SlotInterval` (50ms — per-PD cadence is this
× reader count, the open budget question in the OSDP plan §6.2), `ReplyTimeout` (200ms),
`OfflineAfter` (3 consecutive failures — one dropped frame on a noisy segment is normal and must
not flap a door), `SupervisionTimeout` (5s — declares a reader offline when it answers but never
completes a *usable* transaction, closing the "NAKs everything forever" limbo hole a pure failure
counter cannot catch), `SecureRetryBackoff`/`SecureRetryMax` (500ms→30s, damps a reader that
establishes a session and immediately loses it), `EventBuffer` (256).

## Responsibilities

- `NewBus`/`Add`/`AddPD` — construct and populate the polling rotation. `AddPD(PDConfig)` sets a
  per-reader Secure Channel policy (`SCBK`, `RequireSecureChannel`); refuses
  `RequireSecureChannel` with no `SCBK` at add time rather than leaving a door permanently and
  puzzlingly out of service at runtime.
- `Events() <-chan Event` — the only channel card reads, keypad entries and status changes arrive
  on; a stalled consumer eventually causes drops (`Dropped()`), and a dropped event is a badge that
  never opened a door.
- `Send(ctx, addr, code, data...) (*Frame, error)` / `Output(ctx, addr, output, on, hold)` — the
  low-level actuation primitives. `Output` is explicitly **not** the application's actuation
  chokepoint: every unlock in `mypintusan` (badge, plate, operator override, schedule, flow, API)
  must still funnel through one audited `Issue`-style call before reaching here, the same pattern
  `myiotsan` uses for `CommandService.Issue`. A queued command jumps the round-robin so it is
  serviced in that reader's next slot, since badge-to-strike latency is what matters.
- `Run(ctx) error` — owns the port for its lifetime; the only goroutine that writes to it. Spawns
  `readLoop`, then loops: drain queued commands, pick the next PD (queued-command readers first,
  else round-robin), `transact`, sleep out the remainder of `SlotInterval`. Closing the port on the
  way out is what actually stops `readLoop`, since a blocked `Read` does not observe context
  cancellation.
- `readLoop` — the sole reader of the port, via `bufio.Scanner` + `ScanFrames` (`frame.go.md`),
  running continuously (not driven by the poll loop) so a late reply is still decoded and
  attributed instead of desynchronising the stream for every later transaction.
- `transact(ctx, pd)` — one command/reply exchange: builds the Secure Channel handshake frame if
  one is in flight or due (`pdState.wantsSecureChannel`), else the next ordinary command; seals it
  if a session is established; writes it; `awaitReply`s; unseals the reply if secure; hands the
  result to `handleReply`.
- `secureChannelLost(pd, reason)` — the security-critical rule from the hardware plan §3.2, made
  concrete: on a door that `requireSC`, there is **no cleartext fallback, ever** — the reader goes
  out of service and alarms (`EventOffline`). Where the door does not require it, the failure is
  surfaced (`EventFault`) but the reader is not taken down.
- `checkSupervision`/`fail` — the two paths to `StatusOffline`. `fail` counts consecutive
  transaction failures (`OfflineAfter` threshold); `checkSupervision` catches the case
  `OfflineAfter` cannot — a reader that keeps answering (NAK/BUSY/out-of-sequence forever) but
  never completes a usable transaction, discovered live against the simulator's bad-sequence
  scenario (the CP emitted the same fault 15 times in 7 seconds, never recovered, never alarmed).
  Both clear the Secure Channel session and force `resetSeq` on the way down, since a reader that
  vanished may have been power-cycled or swapped, and resuming an old session against a possibly
  new device is exactly the substitution Secure Channel exists to prevent.
- `awaitReply` — distinguishes three faults that look identical "from a distance" (nothing on the
  wire vs. wreckage on the wire vs. well-formed frames failing CRC) and reports a distinct,
  actionable reason for each, because they send an installer to three different places (addressing
  vs. two-readers-at-one-address vs. termination/grounding).
- `handleReply` — applies one reply to `pdState`: sequence-mismatch handling, `RplBusy`/`RplNak`
  (with a `NakBadSequence`/handshake-refusal split), `RplPDID`/`RplPDCap` (identification →
  `StatusOnline` or `StatusSecuring`), `RplCCrypt`/`RplRMacI` (handshake progression, via
  `securechannel.go.md`), `RplRaw`/`RplKeypad` (credential delivery — gated by
  `credentialsBlocked`, **the** fail-closed boundary that stops a required-but-absent Secure
  Channel from ever delivering a badge in the clear), `RplLStatR` (tamper/power).
- `emit(ev)` — publishes an event, bounded-blocking then counted-dropping rather than stalling the
  whole poll loop (and with it supervision and tamper detection for every other reader) behind one
  slow consumer.

## Notes

- `Status`/`Stats`/`Secure`/`Dropped` are read-only snapshots for callers/UI.
- Depends on `frame.go` (framing), `cp.go` (`pdState`, parsers), `transport.go` (`Transport`,
  `countingReader`, `sleepCtx`), `securechannel.go` (session establishment/seal/unseal).
- Covered by `bus_test.go`.
