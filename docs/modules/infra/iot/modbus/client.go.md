# Module: infra/iot/modbus/client.go

## Purpose

A minimal, stdlib-only Modbus TCP client: exactly the function codes a SunSpec/solar device
needs (read holding fn 3, read input fn 4, write single fn 6, write multiple fn 16), framed with
the 7-byte MBAP header. No external Modbus dependency is pulled into the appliance for this —
the framing is short enough (this file) that depending on a library would buy little and cost a
supply-chain dependency in a single-binary product.

## Key Type: Reader

```go
type Reader interface {
    ReadHolding(addr, qty int) ([]uint16, error)
}
```

The read surface `infra/iot/sunspec` and `RegisterMap.Read` (`regmap.go`) both depend on;
`*Client` satisfies it, so either decode path (SunSpec walk or manual map) works against a real
socket or a test fake without caring which.

## Key Type: Client

```go
func Dial(addr string, unit byte, timeout time.Duration) (*Client, error)
func (c *Client) ReadHolding(addr, qty int) ([]uint16, error)  // fn 3
func (c *Client) ReadInput(addr, qty int) ([]uint16, error)    // fn 4
func (c *Client) WriteSingle(addr int, v uint16) error         // fn 6
func (c *Client) WriteMultiple(addr int, vals []uint16) error  // fn 16
```

One TCP connection to one Modbus **unit id** — a single Modbus TCP endpoint can multiplex several
unit ids (`tools/sunspec-sim` serves three devices on units 1-3 over one listener), so the unit id
is part of the dial, not the address. `Dial` defaults the timeout to 3s. Every call sets a fresh
write/read deadline (`request`) so a wedged device cannot hang a poll forever.

## Key Function: request

Builds the MBAP frame (transaction id, protocol id `0`, length, unit id) around a PDU, writes it,
reads the 6-byte response header to get the declared length, reads exactly that many more bytes,
and surfaces a Modbus **exception** response (high bit of the function code set) as a Go error
rather than returning it as if it were data — a caller that forgot to check would otherwise decode
an exception's function/code byte pair as a bogus register value.

## Notes

- `readRegs` validates the requested quantity (1-125, the Modbus TCP register-read ceiling) and
  the returned byte count against `qty*2` before trusting the payload.
- `WriteMultiple` validates 1-123 registers (the write ceiling, one less than the read ceiling
  because of the extra byte-count field in the write PDU).
- No connection pooling or keep-alive: `infra/iot/modbus.Poller`'s `PollOnce`/`WriteConfirm` dial,
  do one operation, and close (`poller.go.md`) — at a poll cadence of seconds this is negligible
  and avoids stale-socket handling a long-lived connection would need.
- Exercised against a real listener by `tools/sunspec-sim`'s own Modbus server (`modbus.go` there)
  and by `integration_test.go`'s `TestLiveSimulator`.
