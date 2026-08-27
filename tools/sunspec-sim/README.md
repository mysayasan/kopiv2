# sunspec-sim — SunSpec solar system simulator

A **SunSpec-over-Modbus-TCP** simulator of a solar site, driven by a live physics loop over a
compressed day. It exists so myiotsan's Modbus / SunSpec driver (`infra/iot/sunspec`,
`infra/iot/modbus`) and the solar **system workspace** can be built and exercised **without real
hardware** — the same role the `-deaf` relay simulator played for the actuation path.

It is developer tooling. No runtime dependency on `apps/`, `domain/`, or `infra/`; it uses the root
module and the standard library only (no Modbus dependency — the TCP framing is implemented here).

## Three devices — the mixed-protocol case

By default the simulator serves **three devices on three Modbus unit ids**, because a real solar
"workspace" binds devices that speak different register conventions into one system:

| Unit | Device | How a driver reads it |
|-----:|--------|-----------------------|
| 1 | Hybrid inverter — full SunSpec chain (Common + 103 + 123 + 124 + 203) | SunSpec auto-discovery |
| 2 | Standalone SunSpec revenue meter (Common + 203) | SunSpec auto-discovery (a shorter chain) |
| 3 | **Non-SunSpec** vendor inverter — a flat vendor register block, no `SunS` | manual register map |

Units 1–2 exercise the driver's SunSpec walk; unit 3 exercises the manual-register-map path a
non-compliant device needs. The vendor register contract for unit 3 is documented at the top of
[`devices.go`](devices.go). Pass `-single` to serve only the unit-1 hybrid.

### Writable controls

A control register only tests something if the physics loop reads it back out — a register the
simulation ignores lets a client "confirm" a write that achieved nothing, which is precisely the
failure this simulator exists to make visible. The ones honoured:

| Unit | Register | Effect |
|-----:|----------|--------|
| 1 | `123.WMaxLimPct` + `123.WMaxLim_Ena` | export curtailment; the inverter clips to that percent of nameplate and reports operating state THROTTLED. **The enable must be set or the percent means nothing** — the same shape real deye/sungrow inverters have. |
| 1 | `124.StorCtl_Mod` | battery charge/discharge enable bitfield |
| 3 | `reg 16` `PowerLimitPct` | export limit on the NON-SunSpec vendor block (100 = unlimited) |

### `-tod`: start at a chosen hour

The simulation starts at **06:00** by default, where an inverter produces almost nothing. Anything
that needs real production — curtailment, export limiting, a meter with something to meter — should
pass `-tod 12`, or it will be comparing zero against zero:

```
go run ./tools/sunspec-sim -tod 12 -speed 1
```

`setTOD` is part of the `Device` interface rather than a separate one the server type-asserts. It
began as the latter, and three of the four devices simply did not implement it — so `-tod 12` wound
one unit, left the rest at dawn, and said nothing, because a failed type assertion is not an error.
A new device that forgets it now does not compile.

## Why SunSpec

SunSpec is not a wire protocol — it is a **self-describing data model that rides on plain Modbus**.
A compliant device places, at a well-known base register (40000 / 50000 / 0), the ASCII marker
`SunS` followed by a chain of **models**:

```
[ "SunS" ] [ modelID | length | …data… ] [ modelID | length | …data… ] … [ 0xFFFF ]
```

A reader walks the chain by length and decodes each block with the *standard* definition for that
model id. That is why **one driver reads any SunSpec inverter, meter, or battery** with no
per-model register map — the "don't code per model" goal. This simulator serves a real chain so the
driver is developed against the genuine article.

## Models served

| Model | Name | What it carries |
|------:|------|-----------------|
| 1   | Common | Nameplate: manufacturer, model, version, serial |
| 103 | Inverter (3-phase, int+SF) | AC power/current/voltage, frequency, energy (Wh), DC side, temperature, operating state |
| 123 | Immediate Controls | **Writable** export curtailment: `WMaxLimPct` + `WMaxLim_Ena` |
| 124 | Storage | Battery state-of-charge (`ChaState`), charge status, **writable** `StorCtl_Mod` (charge/discharge enable) |
| 203 | Meter (wye, int+SF) | Grid power (import +, export −), cumulative import/export energy |

Values move consistently: PV + battery + grid always balance the load. Production rises to a noon
peak, the battery charges on surplus and carries the evening load, and the meter swings between
import and export.

## Run

```sh
# from the repo root
go run ./tools/sunspec-sim -scenario sunny -speed 1800
# or build a stable exe (into this dir), then run it
make -C tools/sunspec-sim build
./tools/sunspec-sim/sunspec-sim.exe -scenario cloudy
```

Then point any Modbus TCP client at `127.0.0.1:1502` and read holding registers from the base
(`40000`).

### Flags

| Flag | Default | Meaning |
|------|---------|---------|
| `-addr` | `:1502` | Modbus TCP listen address (real SunSpec uses 502; 1502 avoids needing root) |
| `-base` | `40000` | base register of the `SunS` marker (40000 / 50000 / 0) |
| `-speed` | `1800` | simulated seconds per real second (1800 = a 24h day in 48s; `1` = realtime) |
| `-scenario` | `sunny` | `sunny` \| `cloudy` \| `storm` \| `night` \| `outage` |
| `-pv` | `10000` | PV / inverter nameplate, W |
| `-load` | `600` | baseline house load, W |
| `-batt` | `10000` | usable battery energy, Wh |
| `-soc` | `50` | initial state of charge, % |
| `-reserve` | `20` | battery reserve floor (won't discharge below), % |
| `-volt` / `-freq` | `230` / `50` | nominal per-phase voltage / frequency |
| `-mfg` / `-model` / `-serial` | — | Common-model nameplate strings |
| `-v` | off | log every Modbus request |
| `-quiet` | off | suppress the per-tick status line |

## Exercising control (guarded writes)

Control registers are honoured, so the **read-back a guarded write confirms against actually
changes**:

- Write `WMaxLimPct` (model 123, data offset 3) then set `WMaxLim_Ena=1` (offset 7) → the inverter
  curtails, operating state goes `THROTTLED`, and AC output drops to that percent of nameplate.
- Clear bit 1 of `StorCtl_Mod` (model 124, offset 3) → the battery stops discharging (holds) and the
  grid covers the evening load instead.

The simulator logs each control write with its model.point name.

## Notes / conventions

- **Scale factors matter.** Every value is stored against a SunSpec scale factor
  (`stored = round(actual / 10^SF)`). Powers use `SF=0` here (stored watts == real watts) to keep
  decoding obvious; currents `-2`, voltages `-1`, frequency `-2`. A reader that ignores the SF
  registers will be wrong — that is the point of testing against a real chain.
- **Meter sign convention:** model 203 `W` is **positive for import** (grid → site), negative for
  export. Battery `battW` in the status line is **positive for charging**. Sign is the classic
  footgun in solar integrations; it is fixed and documented here so the driver can be pinned to it.
- Inverters above ~32 kW would overflow the `int16` W field at `SF=0`; raise `W_SF` for those. The
  default 10 kW plant fits comfortably.
- Not-simulated points (per-phase meter accumulators, VAh/VARh) are present as reserved registers so
  the chain length still matches the spec and a length-walking reader is not derailed.

## Tests

```sh
go test ./tools/sunspec-sim/...
```

`models_test.go` pins each model's block length to the spec (a shifted field silently corrupts every
downstream value), walks the chain the way a reader does, and proves a curtailment write actually
throttles the simulated inverter.
