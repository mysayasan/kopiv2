# MyPintuSan — `infra/access/osdp` and the PD Simulator

Status: **PARTIALLY BUILT — build order §5 steps 1–4 of 6 done.** `infra/access/osdp` (CP-mode
OSDP driver: `crc.go`, `codes.go`, `frame.go`, `pd.go`, `cp.go`, `bus.go`, `transport.go`,
`securechannel.go`, all with tests) and `tools/osdp-sim` (the TCP PD simulator, 17
fault-injection scenarios) exist and are covered by unit tests. Steps 5–6 — serial transport and
a real reader on the bench, confirming the CRC byte order (§2.1) and Secure Channel's crypto
constants (§2.3, and see `securechannel.go`'s header comment) — are **not done and need
hardware**. There is still **no `apps/mypintusan`** — zero application code; this document and
its two companions remain design-only for anything above the driver layer. Companion to
[`MYPINTUSAN_HARDWARE_PLAN.md`](MYPINTUSAN_HARDWARE_PLAN.md) (reader profiles, trust tiers,
reference kit) and [`MYPINTUSAN_DATA_MODEL.md`](MYPINTUSAN_DATA_MODEL.md) (doors,
credentials, decision path) — both still DRAFT, design only.

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
| Tamper asserted | `RSTATR` → alarm path |
| Slow reply (near timeout) | Poll loop does not starve the other PDs on the bus |

The "refuse Secure Channel" and "drop SC mid-session" cases are the security-critical ones.
They are also precisely the cases nobody tests, because a real reader will not do it on
request.

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
   **The driver can be built against the simulator now** — there is still no application code
   above it.
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

## 6. Open items

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
