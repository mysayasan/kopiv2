# Module: infra/iot/modbus/serial.go

## Purpose

Opens a Modbus RTU connection over a real serial port (`COM3`, `/dev/ttyUSB0`) and adapts it to
the `rtuTransport` (`transport.go.md`) the same way `DialRTUTCP` adapts a raw TCP socket. The one
external dependency the whole `infra/iot/modbus` driver needs (`go.bug.st/serial` v1.8.0) is pure
Go — no cgo — which fits the single-binary/air-gapped posture the rest of the driver already
keeps.

## Key Type: SerialParams

```go
type SerialParams struct {
    Baud     int    // e.g. 9600, 19200
    DataBits int    // usually 8
    Parity   string // "N" (none, default), "E" (even), "O" (odd)
    StopBits int     // 1 (default) or 2
}
```

Zero values fill in to the near-universal RS485 default, 9600 8N1 (`mode()` applies the defaults
and translates to `go.bug.st/serial`'s own `Mode` type). Carried on `poller.go`'s `DeviceConf` as
`Serial`, and on `apps/myiotsan/entities.IotDevice` as `Baud`/`Parity`/`DataBits`/`StopBits`.

## Key Function: DialSerial

```go
func DialSerial(portName string, unit byte, timeout time.Duration, sp SerialParams) (*Client, error)
```

Acquires the port's lock (see below), opens the port with `sp.mode()`, sets a read timeout, wraps
it in a `serialConn`, and builds a `Client` whose transport is an `rtuTransport` over that
connection. The caller MUST `Close()` the returned `Client` to release the port lock — the poller
does, every tick (`poller.go.md`'s `PollOnce`/`WriteConfirm` dial-do-close-per-call pattern).

## The per-port lock: an RS485 bus is multi-drop, a serial port is not

A physical serial port is an exclusive OS resource, but an RS485 bus routinely has several devices
(unit ids) sharing one port, each with its own poller goroutine (`apps/myiotsan/services.
ModbusPoller` runs one goroutine per Modbus device). `portLock(name)` hands out one `*sync.Mutex`
per port name from a package-level registry (`portLocks`, guarded by `portLocksMu`); `DialSerial`
locks it before opening the port and `serialConn.Close` (via `sync.Once`, so a double-close can't
double-unlock) releases it. A device holds the port for exactly one poll (open → reads → close);
the others wait their turn — never a torn frame from two devices writing at once.

## Key Type: serialConn

```go
type serialConn struct {
    p      serial.Port
    unlock func()
    once   sync.Once
}
```

Adapts `serial.Port` to `io.ReadWriteCloser` for `rtuTransport`, plus one behavioral fix:
`go.bug.st/serial` signals a read timeout as `(0, nil)`, not an error, which would make
`io.ReadFull` spin forever waiting for bytes that are never coming. `serialConn.Read` turns that
into `os.ErrDeadlineExceeded`, so a timed-out read fails the poll cleanly (and is retried next
tick, the same as any other transient Modbus failure) instead of hanging the poller goroutine.

## Notes

- `SetReadTimeout` is called once at open with the device's configured timeout — there is no
  per-transaction deadline reset the way `mbapTransport`/`DialRTUTCP` get via `net.Conn.
  SetDeadline`, which is why `rtuTransport.setDeadline` is left `nil` for a serial-backed
  transport (`transport.go.md`).
- Covered indirectly by `rtu_test.go` (`rtu_test.go.md`), which exercises the shared `rtuTransport`
  framing `serialConn` also uses, via an in-memory fake rather than a real port; live-boot verified
  with a `transport=rtutcp` device against a raw mock, proving the same framing path.
