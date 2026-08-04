# Module: infra/access/osdp/transport.go

## Purpose

The byte pipe a `Bus` (`bus.go.md`) owns for the life of the process, plus a small counting
wrapper used to distinguish two different bus faults that look identical at the frame layer.

## Key Type: Transport

```go
type Transport = io.ReadWriteCloser
```

Deliberately just that — no read-deadline method. `Bus` runs a dedicated reader goroutine and
applies timeouts with a `select`, so TCP, serial, and an in-memory test pipe are all equally
simple; pushing `net.Conn` vs. a serial port's very different deadline semantics into the
interface would buy nothing.

## Responsibilities

- `DialTCP(ctx, addr) (Transport, error)` — connects to an OSDP endpoint over TCP: the simulator
  (`tools/osdp-sim`) during development, or a serial-to-Ethernet gateway in the field. The only
  transport implemented so far.
- `countingReader` — wraps the port's reader and tallies bytes actually read. Exists for one
  otherwise-impossible diagnosis: when two readers share an address (the out-of-box case, since
  PDs ship at address 0), their replies collide and almost never survive framing — which at the
  frame layer is indistinguishable from an empty address. Comparing bytes-read against
  frames-decoded (`bus.go`'s `awaitReply`) is the only signal that tells "two readers fighting"
  from "no reader here" apart.
- `sleepCtx(ctx, d) bool` — a context-aware sleep used by `bus.go`'s poll loop between slots.

## Notes

- **Serial (`go.bug.st/serial`, already vendored for Modbus) is deferred to build-order step 5**
  (`MYPINTUSAN_OSDP_PLAN.md` §5) — the line settings and the CRC byte order (`crc.go.md`) are
  things to confirm against real hardware rather than guess at now. When it lands it carries over
  two things from `infra/iot/modbus/serial.go` (the `(0, nil)` read-timeout quirk, and that a
  multi-drop bus is one talker at a time) and must NOT carry over a third: `DialSerial`'s
  open→poll→close lifetime. A CP holds the port open permanently — tearing it down every tick
  would kill the Secure Channel session and add latency to every badge.
- `Bus.Run` (`bus.go.md`) is the sole owner of the `Transport` it is given; only `Run`'s
  `readLoop` reads from it and only `transact` writes to it.
