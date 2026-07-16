# Module: infra/iot/modbus/rtu_test.go

## Purpose

Pins the RTU framing (`transport.go`'s `rtuTransport`) and the CRC-16 it depends on (`crc.go`)
against an in-memory fake peer — no real serial port or socket needed, so `go test
./infra/iot/modbus/...` stays hermetic for this transport the same way it already is for MBAP/TCP.

## Responsibilities

- `TestCRC16CheckValue` — `crc16` against the canonical CRC-16/MODBUS check value (`crc16("123456789")
  == 0x4B37`), plus `crcOK` accepting a frame it just CRC'd and rejecting one with a single
  corrupted payload byte.
- `fakeRTU` — an in-memory RTU peer: every `Write` (a request frame) enqueues the correctly-shaped
  RTU response into a buffer the subsequent `Read`s drain, mirroring how a real serial device or
  RTU-over-TCP gateway replies. Answers read (fn 3/4), write-single (fn 6), or — if `exc` is set —
  a Modbus exception, to every request.
- `TestRTUReadWriteRoundTrip` — drives a full `ReadHolding`, `ReadInput`, and `WriteSingle` through
  the RTU framing against `fakeRTU`, asserting both the decoded values AND that the outgoing
  request frame itself is well-formed (right unit id, right function code, valid CRC).
- `TestRTUExceptionSurfaces` — a Modbus exception response becomes a Go error from `ReadHolding`,
  not a bad decode of the exception bytes as if they were data.

## Notes

- Pure unit tests against `transport.go`/`crc.go` using `rtuClient`, a small helper that builds a
  `*Client` directly around an `rtuTransport{rw: fakeRTU}` — no `DialRTUTCP`/`DialSerial` involved,
  so these tests exercise the shared framing layer both real RTU transports sit on top of.
- The live-hardware-shaped equivalent for this transport is the manual live-boot verification
  noted in `docs/MYIOTSAN_PLAN.md` §8g item 7: a `transport=rtutcp` device read a correct value off
  a raw RTU-over-TCP mock. `integration_test.go`'s `TestLiveSimulator` (`integration_test.go.md`)
  covers only the MBAP/TCP transport, since `tools/sunspec-sim` serves Modbus TCP, not RTU.
