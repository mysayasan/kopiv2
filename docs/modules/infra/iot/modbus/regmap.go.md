# Module: infra/iot/modbus/regmap.go

## Purpose

How a **non-SunSpec** device is read: an explicit, site-authored list of which register holds
which value, at what integer type and scale. SunSpec devices are self-describing and need none of
this (`infra/iot/sunspec`); the many cheaper hybrids that expose a flat vendor register block (no
`SunS` marker, no model chain) need exactly this. It is the data-driven "don't code per model"
answer for the non-compliant case: a new device is a new map, not new code — and the shape a
future `device_profile`'s Modbus binding will carry (`telemetry_key.go` today only knows a JSON
path).

## Key Type: PType / Point / RegisterMap

```go
type PType int
const (PU16, PI16, PU32, PI32 PType = iota, iota, iota, iota)

type Point struct {
    Key      string
    Register int
    Type     PType
    Scale    float64 // e.g. 0.1 for a 0.1 W device
    WordSwap bool    // true if a 32-bit value is low-register-first
    Unit     string
}

type RegisterMap struct { Points []Point }
```

`WordSwap` exists because vendors disagree on 32-bit register word order (hi-then-lo vs
lo-then-hi); it is a per-point flag rather than a per-map one because a single vendor's block has
been seen mixing both within the same device.

## Key Function: Read

```go
func (m RegisterMap) Read(r Reader) ([]codec.Sample, error)
```

Computes the map's register **span** (`span()`, lowest register to highest register+width) and
fetches it in **one Modbus round trip** rather than one read per point — the reason a vendor map
is a `[]Point` list up front instead of per-point reads. Decodes each point (`decodeRaw`) applying
its `Type`, optional `WordSwap`, and `Scale` (defaulting to `1` when unset — a `0` scale would
silently zero every reading, which is exactly the failure this default avoids).

## Notes

- A point whose register falls outside the fetched span (a config error) is skipped, not padded
  with a zero — consistent with `infra/iot/codec`'s "absent is skipped, not zeroed" rule.
- Covered by `regmap_test.go` (`regmap_test.go.md`): a mixed-type decode with per-point scale, and
  the single-round-trip span computation.
- Exercised against a real non-SunSpec device by `integration_test.go`'s `TestLiveSimulator`,
  which reads `tools/sunspec-sim`'s unit-3 vendor inverter through a `RegisterMap` mirroring that
  simulator's documented vendor register contract (`tools/sunspec-sim/devices.go`).
