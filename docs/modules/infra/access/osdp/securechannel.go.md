# Module: infra/access/osdp/securechannel.go

## Purpose

OSDP Secure Channel: AES-128 session establishment (`CHLNG`→`CCRYPT`→`SCRYPT`→`RMAC_I`),
per-packet MAC, and payload encryption. Both `cp.go`/`bus.go` (CP side) and `pd.go` (PD side) share
these primitives.

> **UNCONFIRMED cryptography — read the file's header comment before trusting this against real
> hardware.** The protocol *structure* (handshake order, SCB layout, which SCS type goes on which
> packet, fail-closed enforcement) follows `MYPINTUSAN_OSDP_PLAN.md` §2.3, written against the spec
> and libosdp. The *cryptographic constructions* — session-key derivation constants, cryptogram
> operand order, MAC chaining rule, CBC IV — were reconstructed from libosdp's `osdp_sc.c` without
> the source at hand and are **not independently verified**. Because this package contains both
> sides of the handshake, a green test here proves only that the two halves agree with each other
> — including when both are wrong — not that either matches a real reader. Before a real reader is
> trusted, the constants must be confirmed against libosdp's `osdp_sc.c`/`osdp_phy.c` or a
> known-answer vector from OSDP Bench, and `securechannel_test.go`'s pinned vectors updated from
> that authority. The failure mode if the constants are wrong is loud and safe, not silent and
> unsafe: the PD rejects the cryptogram, the session never establishes, and — because this
> implementation is fail-closed — the reader goes out of service rather than opening a door
> insecurely.

## Key Type: SCBK / secureChannel

`SCBK [16]byte` is an AES-128 Secure Channel Base Key. `SCBKDefault` is the well-known install-mode
key (bytes `0x30`–`0x3F`) — a reader still using it is **not secure**, since the key is published
in every vendor's manual; `IsDefault()` compares in constant time. `secureChannel` holds one
session's derived keys (`sEnc`, `sMac1`, `sMac2`), the handshake randoms/cryptograms, and the
chaining MAC state (`cMac`, `rMac` — each direction's MAC seeds the other's next IV, so a replayed
or reordered packet fails even with an internally-consistent MAC of its own), plus a `scState`
(`scIdle` → `scChallenged` → `scCrypted` → `scActive`, or terminal `scFailed`).

## Responsibilities

- `deriveSessionKeys`/`clientCryptogram`/`serverCryptogram`/`initialRMAC` — the AES-ECB-based key
  schedule and cryptogram construction. `serverCryptogram` deliberately reverses the operand order
  from `clientCryptogram` (`B‖A` vs. `A‖B`) — identical order would let either side replay the
  other's cryptogram back at it and authenticate without holding the key.
- `DiversifySCBK(master, info) SCBK` — derives a per-reader key from a site master key and the
  reader's `PDID` (vendor/model/version/serial), so recovering one reader's key does not unlock the
  whole site. Only meaningful after `PDID` is read, which is why the CP state machine
  (`cp.go.md`) always identifies before handshaking.
- `computeMAC`/`cryptIV`/`encryptPayload`/`decryptPayload` — the rolling CBC-MAC (S-MAC1 for all
  but the last block, S-MAC2 for the last, IV from the *other* direction's last MAC) and CBC
  payload encryption (IV = bitwise complement of the MAC, keeping it distinct from the MAC value
  transmitted in the clear).
- CP-side handshake: `challenge` (sends `CHLNG` with a fresh random; fails closed on an RNG read
  error since a predictable random makes the whole session derivable by eavesdropping),
  `acceptCCRYPT` (constant-time-verifies the PD's cryptogram, sends `SCRYPT`),
  `acceptRMACI` (constant-time-verifies the initial R-MAC, session goes `scActive`).
- PD-side handshake: `answerCHLNG` (produces `CCRYPT`), `answerSCRYPT` (constant-time-verifies the
  CP's cryptogram, produces `RMAC_I`).
- `seal(f *Frame, isCmd bool) ([]byte, error)` — turns a plaintext frame into its on-the-wire secure
  form: SCS_15/16 (MAC only, no data — the common case, since `POLL`/`ACK` carry none) or SCS_17/18
  (encrypted payload + MAC), with the CRC recomputed over the sealed result. `isCmd` selects both
  the SCS type and which chaining MAC seeds the IV; swapping it produces a session that only works
  one direction.
- `unseal(raw, f *Frame, isCmd bool) (*Frame, error)` — verifies the MAC and decrypts. Refuses a
  cleartext packet on an established session outright (the downgrade attack an RS-485 tap wants —
  there is no path back to plaintext once secured) and calls `fail()` on every integrity error,
  since an established-session integrity failure means a fault or an attacker on the pair, not
  something to retry through.

## Notes

- `bus.go`'s `transact`/`handleReply` drive the CP-side calls; `pd.go`'s `Handle`/`command` drive
  the PD-side calls and `seal`/`unseal` for the simulator.
- `macLen = 4`: only 4 of the 16 MAC bytes ride on the wire.
- Covered by `securechannel_test.go`, including the pinned known-answer vectors referenced above.
