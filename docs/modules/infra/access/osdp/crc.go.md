# Module: infra/access/osdp/crc.go

## Purpose

The CRC every OSDP frame ends in. Pure arithmetic, no I/O, no state — the same shape as
`infra/iot/modbus/crc.go`, but a genuinely different algorithm underneath, and the file's own
header comment is deliberate about saying so: this is CRC-16-CCITT (a.k.a. CRC-16/AUG-CCITT),
polynomial `0x1021`, MSB-first (not reflected), initial value `0x1D0F` — not the Modbus RTU CRC
(reflected `0xA001`, init `0xFFFF`). Nothing is shared between the two packages; reusing the wrong
one produces a bus where every frame NAKs and the symptom looks exactly like miswired A/B lines.

## Responsibilities

- `CRC16(data []byte) uint16` — the OSDP frame CRC, pinned against libosdp's
  `crc16_itu_t(0x1D0F, buf, len)`.
- `AppendCRC(frame []byte) []byte` — appends the CRC low-byte-first, per the spec and libosdp.
  **The byte order here is UNCONFIRMED against real hardware** (`MYPINTUSAN_OSDP_PLAN.md` §2.1 /
  §6) — if a bench reader NAKs every frame while the simulator is happy, swap these two bytes
  before suspecting anything else.
- `CRCOK(frame []byte) bool` — checks a full frame (header + payload + trailing 2-byte CRC)
  against a freshly computed CRC of everything before it; `false` for anything shorter than 3
  bytes.
- `checksum8(data []byte) byte` — the legacy 8-bit checksum used when the CTRL byte's CRC bit is
  clear. Never transmitted by this package (`frame.go` always sets the CRC bit), but decoded on
  receipt so a legacy/misconfigured PD reads as "checksum mode" rather than a bare CRC failure.

## Notes

- `frame.go`'s `Marshal`/`Unmarshal`/`ScanFrames` are the only callers.
- Covered by `frame_test.go`, which pins the published CRC-16/AUG-CCITT check value so a future
  "optimisation" cannot quietly change the algorithm.
