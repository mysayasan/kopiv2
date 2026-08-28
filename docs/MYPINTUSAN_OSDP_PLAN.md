# MyPintuSan — `infra/access/osdp` and the PD Simulator

Status: **PARTIALLY BUILT — build order §5 steps 1–4 of 6 done, and now supervised in a real
process.** `infra/access/osdp` (CP-mode OSDP driver: `crc.go`, `codes.go`, `frame.go`, `pd.go`,
`cp.go`, `bus.go`, `transport.go`, `securechannel.go`, all with tests) and `tools/osdp-sim` (the
TCP PD simulator, 17 fault-injection scenarios) exist and are covered by unit tests. Steps 5–6 —
serial transport and a real reader on the bench, confirming the CRC byte order (§2.1) and Secure
Channel's crypto constants (§2.3, and see `securechannel.go`'s header comment) — are **not done
and need hardware**. `apps/mypintusan` is now a runnable app, with a React SPA and a first-run
wizard on top of it (`docs/MYPINTUSAN_DATA_MODEL.md`'s Status line): entities,
the decision path, the door state machine, Wiegand decode/encode, a SQLite-backed `Store`, and —
new — the composition root (`apps/mypintusan/app/app.go`) and the OSDP bus supervisor
(`apps/mypintusan/app/runtime.go`) that keeps `bus.Run` alive across a dead transport (see the new
§7 below) are all built and tested. This document itself, the OSDP wire protocol, is
correspondingly no longer only a library dependency: `bus.go`'s `Run` now has a supervised owner
in a real process, verified live against `tools/osdp-sim` (191 granted badge events over one bus).
Companion to
[`MYPINTUSAN_HARDWARE_PLAN.md`](MYPINTUSAN_HARDWARE_PLAN.md) (reader profiles, trust tiers,
reference kit — trust-rule enforcement still design-only) and
[`MYPINTUSAN_DATA_MODEL.md`](MYPINTUSAN_DATA_MODEL.md) (doors, credentials, decision path — now
P1 built and runnable; see its Status line).

Covers the OSDP driver in **CP (Control Panel) mode** — `mypintusan` polls readers, readers
reply — plus the simulator that must be written **first**.

---

## 1. Build or borrow?

**`github.com/verkada/go-osdp` exists, is MIT-licensed, and does not solve our problem.**
Worth knowing precisely, because "there's a Go library" would otherwise look like a
shortcut:

| | |
| --- | --- |
| License | MIT — permissive, no contagion (unlike libosdp's Apache-2.0 C, which would force CGo and break the suite's pure-Go promise) |
| PD mode | ✅ supported |
| **CP mode** | ❌ **not implemented** — "the library doesn't manage command sequences/state machines" |
| Secure Channel | ✅ AES encrypt/decrypt + MAC, `SCS_11`–`SCS_18` |
| Maturity | v0.2.x, self-described **BETA and not actively maintained** |

CP orchestration — the polling loop, per-PD sequence numbers, the SC handshake state
machine, online/offline supervision — is exactly the part we need and exactly the part it
lacks. Its framing and crypto primitives are sound reference material though.

**Decision: write `infra/access/osdp` ourselves, pure Go.** Read `verkada/go-osdp` (MIT) and
`goToMain/libosdp` (Apache-2.0 C) as references; vendor neither. The protocol is small, and
AES-128 + CMAC are `crypto/aes` + `crypto/cipher` stdlib. This keeps the "single pure-Go
binary: no ffmpeg, no Python" claim that all four existing product cards make.

---

## 2. Verified wire facts

Confirmed against [libosdp source](https://github.com/goToMain/libosdp/blob/master/src/osdp_phy.c)
and the [command/reply reference](https://doc.osdp.dev/protocol/commands-and-replies) —
**not** written from memory.

### 2.1 Frame

```
 ┌──────┬──────────┬─────────┬─────────┬─────────┬─────────┬──────────┬──────────┬───────┐
 │ SOM  │ ADDRESS  │ LEN_LSB │ LEN_MSB │  CTRL   │  [SCB]  │ CMND/RPY │  DATA…   │  CRC  │
 │ 0x53 │  0–0x7F  │         │         │         │  opt.   │          │          │ 2 B   │
 └──────┴──────────┴─────────┴─────────┴─────────┴─────────┴──────────┴──────────┴───────┘
```

- **SOM** = `0x53`.
- **ADDRESS** — PD address 0–126. **Bit `0x80` is the direction bit**: clear on CP→PD
  commands, *set* on PD→CP replies. It exists so a PD never processes another PD's reply
  and a CP never accepts another CP's command. Mask it off before comparing addresses.
- **LEN** — total packet length, little-endian. Unlike Modbus RTU this is explicit, so the
  ugly response-length inference at [`transport.go:110`](../infra/iot/modbus/transport.go#L110)
  has no equivalent here. Framing is *easier* than Modbus.
- **CTRL** bit fields:
  - `0x03` — sequence number (2 bits, cycles **1→2→3→1**; **0 is reserved** to signal
    session start/reset)
  - `0x04` — CRC-16 in use (clear = 8-bit checksum; always set CRC)
  - `0x08` — Security Control Block present
- **SCB** (when `CTRL & 0x08`) — `[0]` = block length, `[1]` = SCS type, one of
  `SCS_11`…`SCS_18`.
- **CRC** — CRC-16-CCITT/ITU-T, polynomial `0x1021`, **initial value `0x1D0F`**
  (`crc16_itu_t(0x1D0F, buf, len)` in libosdp). Note this is a *different* algorithm from
  the Modbus CRC at [`crc.go:15`](../infra/iot/modbus/crc.go#L15) (reflected `0xA001`,
  init `0xFFFF`) — new code, no reuse.

> ✅ **Confirmed 2026-08-04** against libosdp's `osdp_phy.c`, which appends the CRC with
> `bwrite_u16_le(crc16, buf, &len)` — **little-endian, LSB first**, as implemented in
> [`crc.go`](../infra/access/osdp/crc.go). This closes what was previously an open bench item.

### 2.2 Codes we need for P1

Commands: `POLL 0x60`, `ID 0x61`, `CAP 0x62`, `LSTAT 0x64`, `ISTAT 0x65`, `OSTAT 0x66`,
`RSTAT 0x67`, `OUT 0x68`, `LED 0x69`, `BUZ 0x6A`, `TEXT 0x6B`, `COMSET 0x6E`,
`KEYSET 0x75`, `CHLNG 0x76`, `SCRYPT 0x77`.

Replies: `ACK 0x40`, `NAK 0x41`, `PDID 0x45`, `PDCAP 0x46`, `LSTATR 0x48`, `ISTATR 0x49`,
`OSTATR 0x4A`, `RSTATR 0x4B`, **`RAW 0x50` (card data)**, `KEYPAD 0x53`, `COM 0x54`,
`CCRYPT 0x76`, `RMAC_I 0x78`, `BUSY 0x79`.

### 2.3 Secure Channel

Handshake: CP sends `CHLNG` (server random) → PD replies `CCRYPT` (client id + random +
cryptogram) → CP sends `SCRYPT` (server cryptogram) → PD replies `RMAC_I` (client cryptogram
+ initial R-MAC). Thereafter packets carry an SCB and are AES-128 encrypted and MAC'd.

**SCBK-D**, the well-known default base key, is the byte sequence `0x30 0x31 … 0x3F`
(confirmed via [BALTECH's OSDP docs](https://docs.baltech.de/developers/osdp.html)). Key
diversification follows Appendix D.4.1 of the OSDP spec. A reader still on SCBK-D is **not
secure** — the hardware plan caps it to `interior` doors until rekeyed via `KEYSET`.

---

## 3. Package layout — `infra/access/osdp`

```
infra/access/osdp/
  codes.go          command/reply constants (§2.2)
  crc.go            CRC-16-CCITT, poly 0x1021, init 0x1D0F
  frame.go          Frame marshal/unmarshal, direction bit, SCB, sequence handling
  securechannel.go  SCBK/SCBK-D, CHLNG→CCRYPT→SCRYPT→RMAC_I, AES-128 + CMAC, SCS_11-18
  bus.go            persistent port ownership + round-robin multi-drop poll loop
  cp.go             per-PD state machine: offline → id/cap → SC handshake → online
  pd.go             minimal PD side — used by the simulator and by tests
  transport.go      io.ReadWriteCloser over serial (go.bug.st/serial) or TCP (for the sim)
```

### 3.1 What reuses from `infra/iot/modbus`, and what must not

**Reuses cleanly:**
- `go.bug.st/serial` — already vendored at [`go.mod:96`](../go.mod#L96), no new dependency.
- The `(0, nil)` read-timeout quirk already discovered and handled at
  [`serial.go:71-79`](../infra/iot/modbus/serial.go#L71-L79). A bug you do not get to find
  twice.
- The per-port mutex discipline at [`serial.go:43-61`](../infra/iot/modbus/serial.go#L43-L61)
  — a multi-drop bus is one talker at a time, same as RTU.

**Must NOT be copied — three real divergences:**

1. **Connection lifetime is inverted.** `DialSerial` opens → polls → closes every tick.
   That is *wrong* for OSDP: the CP must hold the port open permanently and poll each PD
   roughly every **100–200 ms**, both to keep the Secure Channel session alive and to pick
   up card reads promptly. One goroutine owns the port for the process lifetime.
2. **Card reads are unsolicited-by-poll.** You never "ask for a card". You poll, and the PD
   hands you a `RAW 0x50` reply when someone badges. This is the central mental-model shift
   from Modbus's request/response register reads, and it drives the whole API shape:
   `bus.go` emits card events on a channel rather than returning them from a call.
3. **Sequence numbers are stateful per PD.** `CTRL & 0x03` cycles 1→2→3→1, with 0 reserved
   for session start. Getting this wrong makes the PD `NAK` everything, and it looks
   identical to a wiring fault. This is the single most likely source of a lost day.

---

## 4. Write the simulator first

You already built `tools/sunspec-sim` for exactly this reason, and the suite's own recorded
lesson is *"boot & exercise, don't trust green."*

`tools/osdp-sim` — a Go OSDP **PD** simulator, speaking over TCP (default, zero setup) or a
virtual serial pair (`com0com` on Windows, `socat` on Linux). It shares `pd.go` with the
driver, so the simulator and the production PD-side decoder cannot drift apart.

It unblocks everything: the CP driver, credential store, door state machine, schedules,
lockdown, and the whole UI get built and regression-tested with **zero hardware**. Real
readers then become a validation step rather than a prerequisite — which matters when the
reader is a three-week shipment from Shenzhen.

### 4.1 The point is fault injection

Simulating the happy path is the easy half and the less valuable one. **You cannot make a
real reader misbehave on demand**, and every one of these is a failure the app must survive:

| Scripted fault | What it proves |
| --- | --- |
| Present card X at t+5s | Happy path; `RAW 0x50` decode |
| Unknown card | Deny path + `AccessEvent` with raw value retained |
| Reply `BUSY 0x79` | CP retries rather than declaring the PD dead |
| Wrong sequence number | The §3.1-3 trap, caught by a test instead of on site |
| Refuse Secure Channel | **Fail-closed** — reader out of service, alarm, no cleartext fallback |
| Establish SC then drop it mid-session | Same, on the harder path |
| Still on SCBK-D | Door capped to `interior`, UI nag |
| Go silent (cut the bus) | Offline supervision + degraded-mode alert |
| Return garbage / bad CRC | Decoder does not panic; frame resync works |
| Two PDs both at address 0 | Onboarding wizard handles the out-of-box collision |
| Tamper asserted | `LSTATR` → alarm path (`tamper` scenario) |
| Door contact opens with no grant | `ISTATR` → `door-forced`, then `door-held-open` (`contact-open`) |
| Door contact opens just after a grant | The shunt suppresses `door-forced` and nothing else (`contact-cycle`) |
| Slow reply (near timeout) | Poll loop does not starve the other PDs on the bus |

The "refuse Secure Channel" and "drop SC mid-session" cases are the security-critical ones.
They are also precisely the cases nobody tests, because a real reader will not do it on
request.

> **The CP must ASK for status; a PD never volunteers it.** A PD answers `POLL` with an `ACK`,
> or with a queued card read or keypad entry — never with a status change. So `LSTAT` and
> `ISTAT` have to be scheduled by the poll loop, and until they were, three of this app's six
> declared alarms (`tamper`, `door-forced`, `door-held-open`) could not fire on any
> installation: the state machine, the audit rows, the severities and four translations all
> existed with nothing on the wire to reach them. `Options.StatusInterval` (default 1s) is that
> schedule. It is capped at one status command per four transactions per reader as well as by
> elapsed time, because a card is delivered on a `POLL` and a degraded segment — each dead
> reader costing a full reply timeout — can take longer to go round than the interval, at which
> point a purely time-gated scheduler starves badge delivery completely while the bus looks
> healthy. Found live by `tools/fleetbench/bench_pintusan_alarms.py`.

### 4.2 Multi-drop

The simulator must be able to host **several PDs on one virtual bus** at different
addresses, so round-robin polling, per-PD sequence state, and one-PD-down-others-fine are
exercised before real hardware exists.

---

## 5. Build order

1. ✅ **Done.** `crc.go` + `frame.go` + `codes.go` — pure functions, table-driven tests, no I/O.
2. ✅ **Done.** `pd.go` + `tools/osdp-sim` over TCP — a PD that answers `POLL`/`ID`/`CAP` (plus
   the full 17-scenario fault matrix from §4.1).
3. ✅ **Done.** `bus.go` + `cp.go` — polling loop, sequence state, online/offline supervision.
   **The driver is now consumed by application code** — `apps/mypintusan/app/runtime.go`'s
   `superviseBus` re-dials a fresh `Bus`/transport (1s→30s backoff) whenever `bus.go`'s `Run`
   returns, whether from `ctx` cancellation or a dead port (`failPort`, see §7 below). This closed
   a real gap `Run`'s original contract didn't cover: **online/offline supervision is per-reader**
   (§4.1's fault matrix), but nothing previously supervised the **port itself** — a CP whose
   transport died kept polling a dead wire, marking every reader offline and staying that way
   until the process restarted. Found by booting the app against the simulator and killing it, not
   by any test.
4. ✅ **Done, cryptographically unconfirmed.** `securechannel.go` — handshake, fail-closed
   enforcement tests from §4.1. The protocol structure is implemented and tested; the
   cryptographic constants are reconstructed from libosdp without the source at hand and are
   **not** verified against real hardware or a known-answer vector — see the file's header
   comment.
5. ⬜ **Not done — needs hardware.** Serial transport + real reader on the bench. Confirm the CRC
   byte order (§2.1) and the line settings here, not earlier.
6. ⬜ **Not done — needs hardware.** Second reader on the same bus → multi-drop, address
   assignment, and the out-of-box double-zero collision.

Steps 1–4 need **no hardware at all**. That is the entire argument for the ordering.

---

## 7. Port supervision (added 2026-08-04)

`bus.go`'s `Run(ctx)` previously returned only on `ctx` cancellation or `ErrBusClosed`; any other
transport failure (a write erroring, the peer closing the connection, a read erroring) was logged
and polling continued — against a port that could never again produce a valid reply. Fixed with a
`sync.Once`-guarded `failPort(err)` that records the failure on a buffered `portErr` channel;
`Run` checks it non-blockingly each poll iteration and, if set, fails every pending command and
**returns the error**. `failPort` is called from `readLoop` (any read failure, including a clean
EOF, which is treated as "port closed by the peer" rather than silently swallowed) and from
`transact` (a write failure).

Returning is the point, not a side effect: it hands control back to an owner that can re-dial.
`apps/mypintusan/app/runtime.go`'s `superviseBus` is that owner — a fresh `Bus` and
`services.Controller` are built on every attempt, deliberately, since the per-PD sequence number
and any established Secure Channel session belong to the dead session and must not be resumed
against a possibly power-cycled or swapped reader. See
`docs/modules/infra/access/osdp/bus.go.md` and `docs/modules/apps/mypintusan/app/runtime.go.md`
for the full writeup, and `bus_test.go`'s `TestBusRunReturnsWhenThePortDies` for the regression
coverage (peer-closes-the-pipe and every-write-fails, both asserting `Run` returns within 2s).

This is currently exercised by exactly one consumer, `mypintusan`. Any future second consumer of
`infra/access/osdp` must re-dial on this return the same way, or it will silently stop polling on
its first port failure.

---

## 8. Open items

1. ~~**CRC byte order** on the wire~~ — **RESOLVED 2026-08-04** against libosdp `osdp_phy.c`
   (little-endian, §2.1). The Secure Channel constructions were verified against `osdp_sc.c` at the
   same time; that review found and fixed one divergence (payload padding always appends the 0x80
   marker, even when block-aligned). Hardware *interop* remains unproven until step 5.
2. **Poll cadence vs bus size.** 100–200 ms per PD is fine for 2–4 readers; a 16-reader bus
   at 9600 baud will not sustain it. Needs a measured budget, and probably a
   documented max-readers-per-bus figure at each baud rate for the compatibility page.
3. **`ACURXSIZE` / max reply size negotiation** — deferred past P1, but note that a
   biometric or PIV reader will exceed default buffers.
4. **Which SCS types to implement.** `SCS_11`–`SCS_18` span handshake plus
   MAC-only and full-encryption data modes; P1 likely needs a subset. Decide before
   `securechannel.go` rather than during.
5. **Reader firmware pinning.** `ReaderProfile.VerifiedFirmware` exists (hardware plan §2)
   but nothing yet *checks* the `PDID` reply against it at runtime. Cheap to add, and it is
   what turns a verified profile from a claim into an enforced one.
6. ~~**SCBK-D never checked at runtime**~~ — partially resolved 2026-08-28. See §9.

---

## 9. First live bench of Secure Channel (added 2026-08-28)

The §4.1 fault table's two "security-critical" rows — refuse Secure Channel, and establish-then-
drop mid-session — had never actually been driven against a live appliance; `tools/osdp-sim`'s
`refuse-sc`/`no-sc`/`wrong-key` scenarios presented no card at all, so they could show a session
being refused but not what a badge does next, which is the half of "must fail closed" that
matters. `tools/osdp-sim/main.go` now runs a `cardLoop` for `refuse-sc`, `no-sc` and `wrong-key`
the same way `secure`/`sc-drop`/`default-scbk` already did.
`tools/fleetbench/bench_pintusan_securechannel.py` (18 checks) then drives all six Secure Channel
scenarios against a real appliance, one fresh boot per episode — a reader's key and its
`RequireSecureChannel` policy are seeded from config on **first boot only**, with no API to
change either afterwards, so a question like "what happens with the wrong site key" can only be
asked of a new appliance.

Two real defects, both fixed:

1. **A session that established and then dropped left the door granting on cleartext.**
   `infra/access/osdp/bus.go`'s `secureChannelLost` re-announced the downgrade only from
   `StatusSecuring` (a handshake that never came up); a reader already `StatusOnline` when its
   session died produced no event, so every consumer kept the `SecureSession: true` it was told
   at handshake, permanently. Measured against `sc-drop`: a door requiring an encrypted session
   went on granting badges on a reader whose session had died — the exact RS-485 tap this
   mechanism exists to defeat. Fixed by re-announcing from `StatusOnline` too (see
   `docs/modules/infra/access/osdp/bus.go.md`).
2. **A `critical`/`perimeter` door created without mentioning Secure Channel did not get one.**
   §3.2's stated default (on for `perimeter`/`critical`) was documented on
   `entities.Door.RequireSecureChannel`'s own comment and applied nowhere — the create handler
   passed the request body's boolean straight through. Measured live: a card on a plaintext
   reader opened a `critical` door. Fixed with `entities.SecureChannelDefault(class)` and a
   `*bool` on the create request so an explicit `false` (the interior escape hatch) is
   distinguishable from silence. This one mattered more than an ordinary defaulting bug because
   there is no `PUT /api/doors` — a door created with the wrong policy keeps it for good. See
   `docs/modules/apps/mypintusan/entities/door.go.md` and `.../apis/doors.go.md`.

Proven good and unchanged: a healthy session grants; `no-sc`/`refuse-sc` both deny for the
specific `secure-channel-failed` reason, not a vaguer one; a door that does **not** require SC
still works with a reader that cannot do it; a key mismatch never becomes a cleartext session and
is alarmed (`access.secure-channel`).

**Still a gap, found not built:** the SCBK-D row in the fault table above ("still on SCBK-D →
door capped to `interior`, UI nag") remains unenforced. `Reader.ScbkState` is written once at
creation and never updated; `infra/access/osdp` already carries the signal
(`Event.DefaultKey`/`DefaultKeySession`, via `PDCAP`) but nothing in `apps/mypintusan` reads it.
See `docs/modules/apps/mypintusan/entities/reader.go.md` and
`docs/MYPINTUSAN_HARDWARE_PLAN.md` §3.1.

18/18 checks pass with both fixes; 13/18 against the unfixed app (the simulator's card-loop
change is bench tooling and was held constant across both runs).

---

## 10. The CP never asked for status (added 2026-08-28)

`tools/fleetbench/bench_pintusan_alarms.py`, **19/19**; 9/19 against the unfixed app.

`services/controller.go` declares six alarm kinds. **Three of them could not fire on any
installation**, and one omission explains all three.

A PD answers `POLL` with an `ACK`, or with a queued card read or keypad entry. **It never
volunteers a status change.** §2.2 lists `LSTAT 0x64` and `ISTAT 0x65` among the commands needed
for P1, and `infra/access/osdp/pd.go` answers both — but `bus.go`'s poll loop sent neither, ever.
Everything downstream of that hole was built and unit-tested: the door state machine,
`Controller.ContactChanged`, `EvForced`/`EvHeldOpen`, the audit reasons, the CRITICAL severity on
`door-forced`, four translations of "Door forced open", and a `TestBusTamperSurfaces` that passed
for the life of the driver **by handing the bus an `LSTAT` itself**. The alarms were reachable from
a test and from nowhere else.

| Alarm | Condition | Before |
| --- | --- | --- |
| `tamper` | reader enclosure opened | never fired — no `LSTAT` was sent |
| `door-forced` | door opens with no grant, no REX | never fired — no `ISTAT`, and `ContactChanged` had no caller |
| `door-held-open` | door wedged past its threshold | never fired — same |

A fourth defect sat in the other half of an alarm. `Reader.TamperState` was written once as `ok`
when the reader was enrolled and never again, and `LastSeenAt` was never written at all. Measured
live: while the `reader-offline` alarm was firing correctly, `GET /api/readers` still reported
`tamperState: ok, lastSeenAt: 0`. The alarm woke somebody and the screen they opened lied to them.

**The fix.** `Options.StatusInterval` (default 1s, `statusMillis` per bus in config) schedules a
status command in place of a bare poll. `LSTAT` and `ISTAT` alternate — they answer different
questions and interleaving costs one slot per interval instead of two — and `ISTAT` is skipped
entirely on a reader whose PDCAP claims no supervised inputs, because asking a keypad-only reader
for a door contact earns a NAK every second and a NAK is a failed transaction, which is what
declares a healthy reader offline. Both are emitted on the EDGE: a reader whose case stays open
would otherwise republish the alarm every second forever. `Controller` maps an input report to the
door bound to that reader and calls `ContactChanged`; **input 0 is the door contact and true means
open**, a convention the driver documents rather than something OSDP states (per-input polarity is
a reader-profile setting that does not exist yet).

**A trap this work fell into first, worth recording.** Gating supervision only on elapsed time
turned `TestBusOneReaderDownDoesNotTakeTheBus` red, and the reason is a real production failure:
a card is queued at the PD and handed over on a `POLL`, and every dead reader on a segment costs a
full reply timeout — so a degraded bus can take longer to go round than the status interval, at
which point every single transaction is a status command and **no badge is ever delivered**, while
the bus looks up and supervision looks healthy. Supervision is now capped at one slot in four per
reader as well as by time (`minPollsPerStatus`), pinned by
`TestBusStatusNeverStarvesCardDelivery`.

`tools/osdp-sim` needed two new scenarios (`contact-open`, `contact-cycle`) — the PD had always
modelled `Inputs` and nothing had ever driven them — and `tamper`/`silent` gained card loops for
the same reason `refuse-sc`/`no-sc`/`wrong-key` did in §9: a reader that never came up produces no
grants and no alarms, which reads exactly like a reader that came up and whose alarm is dead.

**Still a gap, found not built:** the `myiotsan` door-contact binding (`Door.ContactDeviceKey`) has
no path in, so a site whose contacts terminate on a relay board rather than on the reader still has
no forced or held-open detection. `Controller.RequestToExit` likewise has no caller, so a REX
button wired to the reader does not shunt the forced alarm.

---

## 11. Offline mode and the cache TTL (added 2026-08-28)

`tools/fleetbench/bench_pintusan_offline.py`, **19/19**; 12/19 against the unfixed app. Item 3 of
the mypintusan hardening register, after Secure Channel (§9) and the six alarms (§10). §2 of
`docs/MYPINTUSAN_DATA_MODEL.md` is the app's answer to the oldest attack on an access control
system — "past the TTL the door denies... there is no allow-all option... 'fail open on network
loss' is a documented attack." Every line of it is implemented in `Decide()`'s GATE 10. This bench
asks whether any of it can actually happen on a running appliance, and finds four ways the answer
was no, all of the same shape as §10: the gate was right and nothing fed it.

1. **The cache could never expire.** `ControllerConfig.CacheAge` has always been declared and
   compared against a door's TTL; nothing assigned it outside a unit test. In production it was
   nil, `Snapshot.CacheAge` was always zero, `s.CacheAge > ttl` was always false, and
   `offline-cache-expired` could not be produced on any install. Measured: a door 20 seconds past a
   2-second TTL still granted. Fixed by `services.CacheClock`
   (`docs/modules/apps/mypintusan/services/cache_clock.go.md`), which persists the time since the
   last contact with an authority over the access data — the fleet control channel connecting
   (`ControlChannelManager.SetOnContact`, `docs/modules/domain/shared/fleetnode/doc.go.md`), or an
   accepted administrative edit made on the appliance itself (`app.go`'s `ruleChangeTouch`
   middleware, scoped to `POST`/`PUT`/`PATCH`/`DELETE` under the access-rule paths and to a 2xx
   response). Persisted, deliberately: a reboot mid-outage must not hand a week-stale replica a
   fresh 72 hours.
2. **`offlinePolicy: deny` and `offlineTtlSeconds` were unreachable.** `createDoorRequest` had
   neither field; the handler hardcoded `OfflinePolicy: entities.OfflineCached` and left the TTL at
   the class default (8/24/72 hours). There is no `PUT /api/doors`, so every door on every install
   was born `cached` at the class default and kept it for good. Fixed: both fields are now accepted
   on create, `offlinePolicy` is validated to `cached`/`deny` and REFUSED (400) for anything else —
   an installer's likeliest invented value is some spelling of "allow", and coercing it to `cached`
   silently would leave them believing they had configured a fail-open door that does not exist —
   and a negative TTL is refused.
3. **Turning offline mode on did not reach the running controllers.** `AccessSettings.Offline` was
   read once at process start. `PUT /api/settings/access` with `offline: true` persisted, returned
   200, read back `true`, and every door kept deciding as though online. Fixed:
   `ControllerConfig.Offline` is now `func() bool` backed by an atomic on the runtime
   (`app/runtime.go.md`'s `runtime.offline`/`SetOffline`/`Offline`), and
   `IAccessSettingsService.OnChange` (`docs/modules/apps/mypintusan/services/runtime_settings.go.md`)
   publishes on `Save`/`Reset` to `runtime.ApplySettings`. Only `offline` applies live; the bus
   list, reader keys, tick and site timezone still need a restart, and the Settings screen's
   restart note now names exactly which fields that covers.
4. **Nobody was told the site was degraded.** §2 has said since P1 that a controller serving from
   cache "raises a degraded-mode alert immediately"; nothing raised one, and a cached grant is
   byte-identical to a live one at the door and on every screen. Fixed: new alarm kind
   `services.AlarmDegraded` ("degraded", WARNING, headline "Running from cache — access rules
   cannot be updated"), raised from `runtime.SetOffline` on the transition into offline mode AND at
   boot for an appliance that starts offline — a site that never crosses the online→offline edge
   would otherwise run from cache forever with nobody told.

**Also fixed, not a defect but a gap:** offline mode had no control on the Settings screen at all,
so the only way to turn it on was editing `config.json` before an appliance's first boot — on a
facilities-managed install, never. A checkbox was added
(`views/react-webpack/src/views/Settings.js`, `settings.offlineHead`/`offline`/`offlineHint`,
translated in en/ms/zh/ar).

Proven good and unchanged, recorded rather than reported as fixed: GATE 10 does run on a live
controller — the bench's positive control is a critical door denying `offline-not-allowed` for a
holder not flagged `OfflineAllowed`, since that reason code comes from nowhere else; the gate
ordering holds (a revoked credential is denied `credential-revoked`, not masked by a stale cache);
`Holder.OfflineAllowed` was already reachable via `POST /api/holders` and opens a critical door
while offline; the access log's `offline` flag is set correctly on a cache-served decision; and no
`allow-all` offline policy can be created, before or after the fix — the invariant §2 argues for
held throughout.

**Two things deliberately not built, reported rather than papered over:**

- There is still no `PUT /api/doors` and no doors-admin screen (doors are only created by the
  first-run wizard), so `offlinePolicy`/`offlineTtlSeconds` above are reachable through the API
  only.
- There is no replica-refresh mechanism anywhere in this suite — `myseliasan` pushes no access data
  down to a node, and no other route does either — so the appliance's own database remains its
  only authority and "offline mode" stays an operator-declared deployment mode rather than
  something a controller detects for itself. `docs/MYPINTUSAN_DATA_MODEL.md` §2's old sentence
  describing "a full local replica... refreshed on change" described a topology that never existed;
  it has been corrected to say so.
