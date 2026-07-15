# Module: infra/iot/modbus/regmap.go

## Purpose

How a **non-SunSpec** device is read: an explicit, site-authored list of which register holds
which value, at what integer type and scale. SunSpec devices are self-describing and need none of
this (`infra/iot/sunspec`); the many cheaper hybrids that expose a flat vendor register block (no
`SunS` marker, no model chain) need exactly this. It is the data-driven "don't code per model"
answer for the non-compliant case: a new device is a new map, not new code — and it is exactly the
shape `entities.TelemetryKey`'s Modbus binding fields carry (`Register`/`RegKind`/`ScaleFactor`/
`WordSwap`, P5) and `apps/myiotsan/services.registerMapFromKeys` builds from them
(`modbus_poller.go.md`).

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

## Key Function: clusters

```go
func (m RegisterMap) clusters() [][]Point
```

Groups the map's points into read windows each **no wider than `maxReadRegisters` (125)** — the
Modbus fn 3/4 per-read limit. A tightly packed map (a cheap inverter's flat block) yields ONE
cluster, the common case. A device that scatters its blocks across the address space — a Huawei
SUN2000 keeps its inverter block near register 32000 and its battery near 37000, ~5,700 registers
apart — yields a cluster per block, so it is read in a few bounded requests rather than one
5,700-register read the protocol forbids outright.

## Key Function: Read

```go
func (m RegisterMap) Read(r Reader) ([]codec.Sample, error)
```

Reads each cluster (`clusters()`) with its own bounded `ReadHolding` call — one round trip for a
tightly packed map, one per block for a scattered one — and decodes every point (`decodeRaw`)
applying its `Type`, optional `WordSwap`, and `Scale` (defaulting to `1` when unset — a `0` scale
would silently zero every reading, which is exactly the failure this default avoids). `span()` is
retained (and still covered by its own test) as the single-cluster case's span computation, but
`Read` itself now always goes through `clusters()`.

## Notes

- A point whose register falls outside its cluster's fetched window (a config error) is skipped,
  not padded with a zero — consistent with `infra/iot/codec`'s "absent is skipped, not zeroed"
  rule.
- Covered by `regmap_test.go` (`regmap_test.go.md`): a mixed-type decode with per-point scale, the
  single-round-trip span computation for a packed map, and `TestRegisterMapClusters` proving a
  scattered map (inverter block + battery block) is read as separate bounded requests via a
  `countingReader` that rejects any read wider than `maxReadRegisters`.
- Exercised against real (simulated) non-SunSpec devices by `integration_test.go`'s
  `TestLiveSimulator`: unit 3, a vendor inverter whose registers fit one cluster
  (`tools/sunspec-sim/devices.go`), and unit 4, a Huawei SUN2000 persona whose inverter/battery/
  meter blocks are deliberately spread far enough apart to force multiple clusters — the exact
  layout the shipped `huawei-sun2000` builtin profile binds
  (`apps/myiotsan/services/profile_catalog.go.md`).
