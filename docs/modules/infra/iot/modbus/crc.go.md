# Module: infra/iot/modbus/crc.go

## Purpose

Modbus RTU CRC-16: RTU framing carries no length field and no TCP checksum (unlike MBAP, where TCP
guarantees delivery and the header carries the length), so every RTU frame must end in a CRC the
receiver can verify. This file is the whole of that: three small pure functions, no state, shared
by both RTU transports (`transport.go`'s `rtuTransport`, used by a serial line or a raw TCP
socket).

## Responsibilities

- `crc16(data []byte) uint16` — the classic Modbus RTU CRC: reflected 0xA001 polynomial, init
  0xFFFF, one bit at a time over every byte.
- `appendCRC(frame []byte) []byte` — appends the CRC-16 low-byte-first (the on-the-wire order),
  used when building an outgoing RTU frame.
- `crcOK(frame []byte) bool` — checks a full received frame (payload + trailing 2-byte CRC)
  against a freshly computed CRC of everything before it. `false` for anything shorter than 4
  bytes (too short to hold a CRC at all).

## Notes

- Pinned against the canonical CRC-16/MODBUS check value by `rtu_test.go`'s
  `TestCRC16CheckValue`: `crc16("123456789") == 0x4B37`. If this ever drifts, every RTU frame the
  app sends or checks is silently malformed.
- `rtuTransport.txn` (`transport.go.md`) calls `appendCRC` when framing a request and `crcOK` on
  every response variant (read, write-echo, exception) before trusting it.
