# osdp-sim — OSDP reader (PD) simulator

A simulator of one or more **OSDP peripheral devices** (card readers) on a virtual RS-485
multi-drop bus, served over TCP. It exists so mypintusan's **CP** driver
(`infra/access/osdp`), the credential store, the door state machine, schedules, lockdown and the
whole UI can be built and regression-tested with **zero hardware** — the same role
[`sunspec-sim`](../sunspec-sim/) plays for the Modbus/SunSpec driver.

Real readers then become a *validation* step rather than a *prerequisite*, which matters when the
reader is a three-week shipment from Shenzhen.

## One deliberate difference from sunspec-sim

`sunspec-sim` implements its own Modbus framing and depends on nothing under `infra/`. **This tool
imports `infra/access/osdp` on purpose.** It shares `pd.go` with the driver, so the simulator and
the production PD-side decoder cannot drift apart. A simulator with its own private frame parser is
one that agrees with itself and disagrees with the wire.

## The default card must be a card that can be GRANTED

`-card` defaults to `00880040` — Wiegand-26, facility 1, card number 4096, **valid parity**. It
used to default to `deadbeef`, which fails leading even parity, and a CP treats a parity failure
as a hard denial (a card one bit out may be somebody else's). The first live bench of mypintusan
spent a run watching every badge denied before working out that the simulator's own default could
never open a door. If you change it, decode the new value with `services.DecodeCard` first.

`-pin` sends keypad digits immediately **before** each card. The order matters: the CP buffers a
keypad entry and consumes it when the card arrives, so digits sent afterwards are left for
whoever badges next — which is exactly the behaviour that stops the person behind you in the
queue opening the door on your PIN.

## Usage

```sh
go run ./tools/osdp-sim -list                     # show every scenario
go run ./tools/osdp-sim -scenario happy -v        # one healthy reader, log every frame
go run ./tools/osdp-sim -scenario silent -fault-after 10s
```

Then point a CP at `127.0.0.1:4870`.

| Flag | Default | Meaning |
| --- | --- | --- |
| `-addr` | `:4870` | TCP listen address for the simulated bus |
| `-scenario` | `happy` | which fault to run (see below) |
| `-card` | `00880040` | card data presented on the bus, hex — **must have valid parity for `-bits`** |
| `-pin` | *(empty)* | PIN digits entered on the keypad just before each card |
| `-bits` | `26` | card bit count (26 = standard Wiegand, 34 = extended) |
| `-card-every` | `8s` | how often to badge; `0` disables |
| `-fault-after` | `15s` | delay before a time-based fault bites |
| `-slow-reply` | `400ms` | reply delay for the `slow` scenario |
| `-v` | off | log every frame in both directions |

Transport is **TCP only**. A virtual serial pair (`com0com` / `socat`) is deliberately not wired up
here: the serial transport gets written once, in `infra/access/osdp`, at build-order step 5 when
there is real hardware to verify it against. A second, unexercised copy in a tool would rot.

## The point is fault injection

Simulating the happy path is the easy half and the less valuable one. **You cannot make a real
reader misbehave on demand** — and every scenario below is a failure the CP must survive. Each one
is a row of the fault table in [`MYPINTUSAN_OSDP_PLAN.md` §4.1](../../docs/MYPINTUSAN_OSDP_PLAN.md).

| Scenario | What it proves |
| --- | --- |
| `happy` | `RAW 0x50` decode; a card read arriving on a poll reply |
| `multidrop` | Round-robin polling, per-PD sequence state, three readers on one bus |
| `addr-collision` | Two brand-new readers **both at factory address 0** — the out-of-box onboarding case |
| `busy` | `BUSY 0x79` is retried, not treated as a dead reader |
| `bad-sequence` | The §3.1 sequence trap, caught by a test instead of on site |
| `bad-crc` | Corrupt replies rejected without panicking |
| `garbage` | Frame **resynchronisation** — junk, a broken frame, more junk, then recovery |
| `silent` | Offline supervision + degraded-mode alert — badges a card first, so "no grant after" means something |
| `one-down` | One PD dies; the others on the bus keep working |
| `refuse-sc` | **Fail-closed**: reader out of service and alarmed, never a cleartext fallback — badges a card so the claim is actually demonstrated |
| `no-sc` | Reader cannot do Secure Channel at all (PDCAP drops the AES-128 bit), and badges anyway |
| `default-scbk` | Reader still on the well-known default base key → capped at `interior`, UI nag |
| `tamper` | `LSTATR` alarm path — badges throughout, so the alarm is on a reader known to be working |
| `contact-open` | Door-position contact opens with **no badge** and stays open → `door-forced`, then `door-held-open` |
| `contact-cycle` | Contact opens just after a grant and closes again → the shunt suppresses `door-forced`, and `door-held-open` still fires |
| `slow` | A near-timeout reply does not starve the other PDs on the bus |
| `secure` | Secure Channel happy path: handshake on the site key, then encrypted traffic |
| `sc-drop` | Session **establishes then drops** mid-conversation — the harder fail-closed case |
| `wrong-key` | Reader holds a different site key — must fail closed, not downgrade; badges a card |

`refuse-sc`, `no-sc` and `wrong-key` used to configure the fault and stop there — proving a
session was refused, which is the easy half. "Must fail closed" is a claim about what happens
**when somebody badges**, so all three now run the same `cardLoop` `secure`/`sc-drop`/
`default-scbk` already did, and a bench can assert on the resulting access decision instead of
just the handshake outcome. See `tools/fleetbench/bench_pintusan_securechannel.py`.

`silent` and `tamper` got the same treatment for the same reason. A reader that never came up
produces no grants and no alarms, which reads exactly like a reader that came up and whose alarm is
dead — so both now badge, and a bench can establish that the reader was in service *before* the
fault bit. `bad-sequence`, `bad-crc`, `garbage` and `addr-collision` still present no card.

`contact-open` and `contact-cycle` are new: the PD has always modelled `Inputs` and answered
`ISTAT`, and nothing had ever driven them. See `tools/fleetbench/bench_pintusan_alarms.py`, which
uses them to show that `door-forced` and `door-held-open` fire on a real opening and that a
legitimate entry does not raise a forced alarm.

`refuse-sc`, `sc-drop` and `wrong-key` are the security-critical ones. They are also precisely the
cases nobody tests, because a real reader will not do any of them on request.

### Secure Channel

Pass `-site-key` (default `a0a1…b6b7`) and give the CP the same key. `secure`, `sc-drop` and
`wrong-key` use it; every other scenario runs in the clear.

⚠️ **The Secure Channel crypto is structurally complete but cryptographically UNCONFIRMED.** The
session-key derivation, cryptogram operand order, MAC chaining and CBC IV in
[`securechannel.go`](../../infra/access/osdp/securechannel.go) were reconstructed from libosdp
without the source at hand. Because the simulator and the driver share those primitives, a green
handshake here proves the two halves agree with each other — *not* that either matches a real
reader. See the header comment in that file for what has to be confirmed before trusting hardware.

## Two things booting it caught that the unit tests did not

Recorded because both are the kind of modelling error that makes a simulator quietly useless.

1. **Identical readers did not collide.** The collision model was a byte-wise AND. Two
   factory-fresh PDs are *identical*, so AND-ing their replies returned that reply unchanged and the
   CP decoded a perfectly clean `PDID` — the exact case the scenario exists to catch. The unit test
   passed only because it gave the two PDs different serial numbers. Real transmitters are never
   bit-synchronised, so `collide` now applies a one-byte skew, which corrupts identical frames as
   readily as differing ones.

2. **`garbage` produced silence, not garbage.** Pure random noise essentially never contains a
   plausible length field, so a CP's scanner just buffers it and the reader presents as *silent* —
   a timeout test wearing a resync test's name. The junk now wraps a framed-but-CRC-broken reply, so
   the whole path runs: skip leading noise, latch onto a frame, reject it, skip trailing noise,
   recover before the next poll.

A note for `cp.go` (step 3) that falls out of the first: a collision and an *empty address* are
indistinguishable at the frame layer — both yield nothing. Telling "two readers fighting" from "no
reader here" during onboarding needs a **bytes-arrived-but-never-framed** signal, which only the
transport can see.
