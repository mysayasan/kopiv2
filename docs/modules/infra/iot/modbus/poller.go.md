# Module: infra/iot/modbus/poller.go

## Purpose

The two ways myiotsan will read a Modbus device — SunSpec auto-discovery or a manual register
map — normalised to the identical `codec.Sample` stream `services.Ingest`'s MQTT path already
produces, plus the guarded-write primitive a Modbus control action needs. This is the driver
foundation for P5 (`docs/MYIOTSAN_PLAN.md` §8g); nothing in `apps/myiotsan/app` calls it yet —
`DeviceConf` documents the runtime shape a future `device_profile`/`iot_device` Modbus binding will
carry, not a wired config path.

## Key Type: Mode / DeviceConf

```go
type Mode int
const (
    ModeSunSpec Mode = iota // self-describing: walk the model chain
    ModeRegMap              // non-SunSpec: use the site-authored register map
)

type DeviceConf struct {
    Key      string
    Endpoint string
    Unit     byte
    Mode     Mode
    Base     int           // SunSpec base register; 0 = auto-discover (40000/50000/0)
    Map      RegisterMap   // used when Mode == ModeRegMap
    Timeout  time.Duration
}
```

`Base == 0` means auto-discover via `sunspec.Discover`; a nonzero value skips discovery and walks
that base directly (a device known in advance not to need the probe).

## Key Function: PollOnce

```go
func PollOnce(d DeviceConf) ([]codec.Sample, *sunspec.Common, error)
```

Dials the device, reads it once, and returns decoded samples (plus the SunSpec nameplate, `nil`
for a register-map device since it isn't self-describing). Dials and closes per call — see
`client.go.md`'s Notes for why a persistent connection isn't used.

## Key Function: Run

```go
func Run(ctx context.Context, d DeviceConf, interval time.Duration,
    emit func(key string, s []codec.Sample), onErr func(error))
```

Polls on a ticker until `ctx` is cancelled. A failed poll is reported to `onErr` and retried on
the next tick — a device that briefly drops off the bus must not kill the poller. This is the
per-device goroutine shape a future poller service in `app.go` will run one of per Modbus device
(§8g point 2).

## Key Function: WriteConfirm

```go
func WriteConfirm(d DeviceConf, reg int, value uint16, timeout time.Duration) error
```

A **guarded** write: writes `value` to `reg`, then re-reads that register until it reports
`value` or `timeout` elapses. It **never re-issues the write** — a Modbus write to an
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
