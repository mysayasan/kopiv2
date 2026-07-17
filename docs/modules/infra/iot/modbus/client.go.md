# Module: infra/iot/modbus/client.go

## Purpose

A minimal Modbus client: exactly the function codes a SunSpec/solar device needs (read holding
fn 3, read input fn 4, write single fn 6, write multiple fn 16), speaking any of **three
transports** through a shared `transport` interface (`transport.go.md`) — MBAP over TCP (the
default), RTU over a "transparent" TCP gateway, and RTU over a real serial line (`serial.go.md`).
Stdlib-only except for the one serial dependency: no Modbus library is pulled in for the framing
itself, since it is short enough (`client.go`/`transport.go`/`crc.go`) that depending on one would
buy little and cost a supply-chain dependency in a single-binary product.

## Key Type: Reader

```go
type Reader interface {
    ReadHolding(addr, qty int) ([]uint16, error)
}
```

The read surface `infra/iot/sunspec` and `RegisterMap.Read` (`regmap.go`) both depend on;
`*Client` satisfies it, so either decode path (SunSpec walk or manual map) works against a real
socket, a real serial line, or a test fake without caring which.

## Key Type: Client

```go
func Dial(addr string, unit byte, timeout time.Duration) (*Client, error)       // Modbus TCP/MBAP
func DialRTUTCP(addr string, unit byte, timeout time.Duration) (*Client, error) // RTU over TCP
func (c *Client) ReadHolding(addr, qty int) ([]uint16, error)  // fn 3
func (c *Client) ReadInput(addr, qty int) ([]uint16, error)    // fn 4
func (c *Client) WriteSingle(addr int, v uint16) error         // fn 6
func (c *Client) WriteMultiple(addr int, vals []uint16) error  // fn 16
```

`Client` holds a `transport` (not a raw `net.Conn` any more) plus the unit id and timeout; every
read/write method is transport-blind, calling `request` → `c.tr.txn`. One connection addresses one
Modbus **unit id** — a single Modbus TCP endpoint can multiplex several unit ids
(`tools/sunspec-sim` serves three devices on units 1-3 over one listener), so the unit id is part
of the dial, not the address. `Dial` opens the original MBAP-over-TCP transport (`mbapTransport`)
and is unchanged in name/behaviour so existing callers needed no change when transports were
introduced. `DialRTUTCP` opens the RTU transport (`rtuTransport`) over a plain TCP socket instead —
for a "transparent" RS485→TCP gateway that speaks bare RTU frames, not real Modbus TCP. A serial
line uses `DialSerial` (`serial.go.md`) instead, which builds the same `rtuTransport` over a
`serialConn`. Both `Dial` and `DialRTUTCP` default the timeout to 3s. Every call sets a fresh
write/read deadline through the transport (`mbapTransport.txn`'s `conn.SetDeadline`, or the serial
port's read timeout set at open) so a wedged device cannot hang a poll forever.

## Key Function: request

Delegates to `c.tr.txn(c.unit, pdu, c.timeout)` — the transport does the actual framing (MBAP
header + length for TCP, unit+PDU+CRC-16 for RTU) and returns a bare response PDU, surfacing a
Modbus **exception** response (high bit of the function code set) as a Go error rather than
returning it as if it were data — a caller that forgot to check would otherwise decode an
exception's function/code byte pair as a bogus register value. This is the exact PDU-in/PDU-out
shape the pre-transport client returned, so nothing downstream (`readRegs`, `regmap.go`,
`infra/iot/sunspec`) changed when transports were introduced.

## Notes

- `readRegs` validates the requested quantity (1-125, the Modbus register-read ceiling) and the
  returned byte count against `qty*2` before trusting the payload.
- `WriteMultiple` validates 1-123 registers (the write ceiling, one less than the read ceiling
  because of the extra byte-count field in the write PDU).
- No connection pooling or keep-alive, on any transport: `infra/iot/modbus.Poller`'s
  `PollOnce`/`WriteConfirm` dial, do one operation, and close (`poller.go.md`) — at a poll cadence
  of seconds this is negligible and avoids stale-socket handling a long-lived connection would
  need. For serial this also means the per-port lock (`serial.go.md`) is held only for the
  duration of one poll, not the process lifetime.
- Exercised against a real listener by `tools/sunspec-sim`'s own Modbus server (`modbus.go` there)
  and by `integration_test.go`'s `TestLiveSimulator` (both over MBAP/TCP), and against the RTU
  transport by `rtu_test.go`'s in-memory `fakeRTU` peer (`rtu_test.go.md`).
