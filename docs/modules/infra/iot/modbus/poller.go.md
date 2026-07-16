# Module: infra/iot/modbus/poller.go

## Purpose

The two ways myiotsan reads a Modbus device — SunSpec auto-discovery or a manual register map —
normalised to the identical `codec.Sample` stream `services.Ingest`'s MQTT path already produces,
plus the guarded-write primitive a Modbus control action needs. This was the driver foundation for
P5 (`docs/MYIOTSAN_PLAN.md` §8g); it is now **wired in**: `apps/myiotsan/services.ModbusPoller`
(`modbus_poller.go.md`) resolves a `DeviceConf` from a device's `entities.IotDevice`
(`Endpoint`/`Unit`) and `entities.DeviceProfile` (`Transport`/`ModbusMode`/`ModbusBase`/
`PollSeconds`) and drives `Run` under `safego.Supervise` in `app.go`, feeding
`Ingest.HandlePolled` — the same back half an MQTT publish takes.

## Key Type: Mode / Transport / DeviceConf

```go
type Mode int
const (
    ModeSunSpec Mode = iota // self-describing: walk the model chain
    ModeRegMap              // non-SunSpec: use the site-authored register map
)

type Transport int
const (
    TransportTCP    Transport = iota // MBAP over TCP (the default)
    TransportRTUTCP                  // RTU frames over TCP (a transparent RS485->TCP gateway)
    TransportSerial                  // RTU frames over a serial line (RS485/RS232)
)

type DeviceConf struct {
    Key       string
    Endpoint  string // host:port for TCP/RTU-over-TCP; the port name (COM3, /dev/ttyUSB0) for serial
    Unit      byte
    Mode      Mode
    Base      int           // SunSpec base register; 0 = auto-discover (40000/50000/0)
    Map       RegisterMap   // used when Mode == ModeRegMap
    Transport Transport     // TCP / RTU-over-TCP / Serial
    Serial    SerialParams  // line settings when Transport == TransportSerial
    Timeout   time.Duration
}
```

`Base == 0` means auto-discover via `sunspec.Discover`; a nonzero value skips discovery and walks
that base directly (a device known in advance not to need the probe). `Transport` defaults to
`TransportTCP` (the zero value), so every `DeviceConf` built before transports existed keeps
behaving exactly as it did.

## Key Function: DeviceConf.dial

```go
func (d DeviceConf) dial() (*Client, error)
```

Picks the right `Client` constructor for `d.Transport` — `Dial` (TCP/MBAP, the default),
`DialRTUTCP`, or `DialSerial` with `d.Serial` — so `PollOnce` and `WriteConfirm` below don't need
to know which transport a device uses. SunSpec walk and register-map read work identically over
any of them, since `Client` hides the framing (`client.go.md`).

## Key Function: PollOnce

```go
func PollOnce(d DeviceConf) ([]codec.Sample, *sunspec.Common, error)
```

Dials the device (via `d.dial()`), reads it once, and returns decoded samples (plus the SunSpec
nameplate, `nil` for a register-map device since it isn't self-describing). Dials and closes per
call — see `client.go.md`'s Notes for why a persistent connection isn't used.

## Key Function: Run

```go
func Run(ctx context.Context, d DeviceConf, interval time.Duration,
    emit func(key string, s []codec.Sample), onErr func(error))
```

Polls on a ticker until `ctx` is cancelled. A failed poll is reported to `onErr` and retried on
the next tick — a device that briefly drops off the bus must not kill the poller. This is the
per-device goroutine shape `apps/myiotsan/services.ModbusPoller` runs one of per Modbus device
(`modbus_poller.go.md`), reconciled against the device inventory rather than started once at boot.

## Key Function: WriteConfirm

```go
func WriteConfirm(d DeviceConf, reg int, value uint16, timeout time.Duration) error
```

A **guarded** write: dials via `d.dial()` (so a control write inherits the device's transport just
like a poll does), writes `value` to `reg`, then re-reads that register until it reports `value`
or `timeout` elapses. It **never re-issues the write** — a Modbus write to an
inverter/battery is a physical action, and resending it because a confirmation was slow would be
a *second* physical action; nothing at this layer can distinguish "the write was lost" from "the
confirmation was lost", so on doubt it fails and a human decides. This mirrors the rule the MQTT
actuation path already enforces (`apps/myiotsan/services/commands.go` — a command is never
auto-retried). Confirmation is by re-reading, which is safe to repeat.

## Notes

- `PollOnce`'s SunSpec branch also captures the model-1 `Common` block if present, for a future
  device-twin/nameplate display.
- Exercised live (not just against synthetic banks) by `integration_test.go`'s
  `TestLiveSimulator`, which drives all three of `tools/sunspec-sim`'s personas through this exact
  `PollOnce`/`WriteConfirm` path over real Modbus TCP.
