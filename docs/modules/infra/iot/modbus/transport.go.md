# Module: infra/iot/modbus/transport.go

## Purpose

The `transport` seam that lets `Client` (`client.go.md`) speak Modbus over three different wire
shapes — MBAP/TCP, RTU-over-TCP, and RTU-over-serial — with zero change to any caller. Every
read/write method on `Client` goes through one bare-PDU-in/bare-PDU-out transaction, so the
decoders that sit above it (SunSpec walk, `RegisterMap`) work identically regardless of transport.

## Key Type: transport

```go
type transport interface {
    txn(unit byte, pdu []byte, timeout time.Duration) ([]byte, error)
    Close() error
}
```

`txn` takes a request PDU (`[function, ...args]`) and returns the response PDU (`[function,
...data]`) with the unit id and any framing/CRC already stripped, or a non-nil error for a
transport failure or a Modbus **exception**. This is exactly the shape the pre-transport client's
`request` method returned, so introducing transports changed nothing downstream.

## Key Type: mbapTransport (MBAP over TCP — the original transport)

```go
type mbapTransport struct {
    conn  net.Conn
    txnID uint16
}
```

Moved out of `client.go` unchanged: builds the 7-byte MBAP header (incrementing transaction id,
protocol id `0`, length, unit id) around the PDU, writes it, reads the 6-byte response header to
get the declared length, reads exactly that many more bytes, and surfaces a Modbus exception
(high bit of the function code) as a Go error. `Dial` (`client.go.md`) constructs this.

## Key Type: rtuTransport (RTU — serial line or a transparent TCP gateway)

```go
type rtuTransport struct {
    rw          io.ReadWriteCloser
    setDeadline func(time.Time) error // nil for a serial stream (which has a per-read timeout instead)
}
```

Speaks Modbus RTU over any byte stream — a serial port (`serial.go.md`'s `serialConn`) or a raw
TCP socket (`DialRTUTCP`, `client.go.md`) — identically. RTU has no length header, so `txn` infers
the response size from the **request's** function code:

- fn `0x03`/`0x04` (read holding/input): read a 2-byte header (`[unit, fn]`), then a byte-count
  byte, then that many data bytes plus a 2-byte CRC.
- fn `0x06`/`0x10` (write single/multiple): the response echoes a fixed 4-byte
  addr/value-or-quantity field plus CRC.
- Any other request function code is refused (`modbus rtu: unsupported function`) rather than
  guessing a response length.
- A response with the exception bit set (`fn&0x80`) is read as a fixed 3-byte tail
  (`[excCode][crc-lo][crc-hi]`) and surfaced as the same `modbus: exception fn=... code=...` error
  the MBAP transport produces, so a caller cannot tell which transport it is on from an error
  string.

Every response variant is CRC-checked (`crcOK`, `crc.go.md`) before its payload is trusted;
a bad CRC is a distinct error (`modbus rtu: bad CRC on ...`) rather than a silently corrupted
decode. `setDeadline` is the stream's whole-transaction deadline where the stream has one
(`net.Conn.SetDeadline`, used by `DialRTUTCP`); a serial stream instead gets its timeout applied
once, as a per-read timeout, at `DialSerial` time, so `setDeadline` is `nil` there and `txn` skips
it.

## Notes

- `DialRTUTCP` (`client.go.md`) and `DialSerial` (`serial.go.md`) are the two constructors that
  build an `rtuTransport`; only their underlying `io.ReadWriteCloser` and `setDeadline` differ.
- Covered by `rtu_test.go` (`rtu_test.go.md`): a canonical CRC check value, a full RTU
  read/write round trip against an in-memory fake peer, and exception surfacing — all without a
  real serial port or socket.
