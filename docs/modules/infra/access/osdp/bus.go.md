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
establishes a session and immediately loses it), `StatusInterval` (1s — how often an online reader
is asked for LOCAL status (tamper, mains) and INPUT status (the door contact) instead of a bare
poll; it exists because a PD volunteers neither, see `cp.go.md`'s `dueStatus`), `EventBuffer`
(256).

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
  `readLoop`, then loops: check `portErr` (below) for a dead transport, drain queued commands,
  pick the next PD (queued-command readers first, else round-robin), `transact`, sleep out the
  remainder of `SlotInterval`. Closing the port on the way out is what actually stops `readLoop`,
  since a blocked `Read` does not observe context cancellation.
- `failPort(err)` / `portErr` — **records that the transport itself is unusable and makes `Run`
  return**, added because a dead port is not the same as a dead reader and treating it as one was
  a production outage: before this, a CP whose USB-RS485 adapter was unplugged — or whose
  serial-to-Ethernet gateway rebooted — kept polling a wire nobody was listening to, marked every
  reader offline (via `OfflineAfter`), and stayed that way until the process was restarted. Found
  by booting `mypintusan` against `tools/osdp-sim` and killing the simulator, not by any unit test
  (the suite's own recorded lesson: "boot & exercise, don't trust green" — see
  `apps/mypintusan/app/runtime.go.md`, whose `superviseBus` re-dials on exactly this return).
  `failPort` is `sync.Once`-guarded (one-shot: the first failure is the diagnosis, the hundred
  that would follow are noise) and is called from two places: `readLoop` on any read failure
  (including a clean EOF, treated as "port closed by the peer" rather than silently swallowed),
  and `transact` on a write failure. `Run` checks the `portErr` channel non-blockingly at the top
  of each poll iteration and, if set, fails every pending command and returns the error —
  **`Run` returning is the whole point**: it is what lets the owner re-dial a fresh transport
  rather than polling a corpse forever.
- `readLoop` — the sole reader of the port, via `bufio.Scanner` + `ScanFrames` (`frame.go.md`),
  running continuously (not driven by the poll loop) so a late reply is still decoded and
  attributed instead of desynchronising the stream for every later transaction. On exit (and only
  if not an ordinary shutdown, i.e. `ctx.Err() == nil`), logs the read error (defaulting to a
  synthetic "port closed by the peer" on a clean EOF) and calls `failPort`.
- `transact(ctx, pd)` — one command/reply exchange: builds the Secure Channel handshake frame if
  one is in flight or due (`pdState.wantsSecureChannel`), else the next ordinary command; seals it
  if a session is established; writes it; `awaitReply`s; unseals the reply if secure; hands the
  result to `handleReply`.
- `secureChannelLost(pd, reason)` — the security-critical rule from the hardware plan §3.2, made
  concrete: on a door that `requireSC`, there is **no cleartext fallback, ever** — the reader goes
  out of service and alarms (`EventOffline`). Where the door does not require it, the failure is
  surfaced (`EventFault`) but the reader is not taken down. Re-announces the downgrade from
  **either** `StatusSecuring` (a handshake that never came up) **or `StatusOnline`** (a session
  that established and then dropped mid-conversation). Only the former was covered originally;
  a reader already `StatusOnline` produced no event on loss, so every consumer kept the
  `SecureSession: true` it was told at handshake, permanently — measured against
  `tools/osdp-sim`'s `sc-drop` scenario, where a door requiring an encrypted session went on
  granting badges on a reader whose session had died. The PD's own comment calls this "the
  harder half": refusing a handshake is caught at enrolment, but losing a session mid-conversation
  happens to a reader that is already trusted and already bound to a live door.
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
- `secureChannelLost`'s `StatusOnline` branch (above) is live-benched, not just unit-tested:
  `tools/fleetbench/bench_pintusan_securechannel.py` drives `sc-drop` against a real appliance
  and asserts on the resulting access decision, since a unit test over `Bus` alone cannot show
  what a consumer three layers up does with the event it receives (or, before the fix, does not
  receive).
- Covered by `bus_test.go`, including `TestBusRunReturnsWhenThePortDies` — the regression test
  for the reconnect fix above, exercised two ways: the peer closing its end of a `net.Pipe`, and
  a transport whose every `Write` fails (`deadTransport`). Both assert `Run` returns a non-nil
  error within 2s rather than hanging, since the earlier behaviour was to keep polling forever.
- **This is currently shared infra used only by `mypintusan`.** The reconnect behaviour above is
  a behaviour change to `Bus.Run`'s contract (it can now return while `ctx` is still live, which
  it previously never did outside `ErrBusClosed`/`ctx.Err()`); any future second consumer of this
  package needs to re-dial on a non-context-cancellation error the same way
  `apps/mypintusan/app/runtime.go.md`'s `superviseBus` does, or it will silently stop polling on
  its first port failure.
