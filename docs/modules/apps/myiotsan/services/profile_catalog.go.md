# Module: apps/myiotsan/services/profile_catalog.go

## Purpose

The shipped device catalog — the device types a building/security install actually has,
expressed once so the hundredth door sensor is a name and a dropdown rather than a
configuration exercise. Topic templates follow the real conventions (Zigbee2MQTT, Tasmota,
Shelly), so a device flashed with stock firmware works without a custom profile.

**The deadbands in this file are the point of it** — each one is a claim about how much change
is worth a row, and collectively they are what makes a thousand samples a second collapse into
a manageable write rate.

## Key Function: builtinProfiles

```go
func builtinProfiles() []builtinProfile
```

Returns fourteen profiles, each a deliberate deadband/heartbeat choice — nine PUSH (MQTT) profiles
and, as of P5, five POLLED (Modbus) ones:

- **`door-contact`** — `contact` has NO deadband (every transition is the event; a handful a
  day), `battery`/`linkquality` deadbanded and heartbeated every 6h.
- **`pir-motion`** — `occupancy` bool, no deadband; `illuminance` deadbanded 50lx.
- **`temp-humidity`** — `temperature` deadband 0.2C (deliberately coarser than the sensor's own
  noise floor — the difference between a row a second and a row when the room actually
  changes), `humidity` deadband 1%.
- **`smoke-detector`** — a life-safety signal: `smoke` has **zero** deadband and a short (600s)
  heartbeat, because a smoke detector gone quiet is itself an emergency and the offline rule can
  only see that if the device is expected to speak often.
- **`water-leak`** — dry for years, then one transition that matters enormously; the 1800s
  heartbeat is what keeps "dry" distinguishable from "dead battery".
- **`power-meter`** — wide deadbands in watts/volts/amps (a compressor cycling is worth a row, a
  phone charger's ripple is not); `energy` (cumulative, monotonic) uses a coarse deadband that
  still captures the curve.
- **`access-reader`** — `badge` is a **string** key (compared by equality, never averaged: the
  credential presented), `granted`/`tamper` bools with no deadband. The "no badge swipe" half of
  the cross-domain intrusion rule described in `docs/MYIOTSAN_PLAN.md` §1.
- **`smart-relay`** (Shelly/Tasmota conventions, P4) — **the one profile in this catalog that can
  act on the world; every other profile only reports.** Declares one `ProfileCommand`
  (`"output"`, `Kind: "switch"`, publishing a Shelly-style `Switch.Set` RPC to `{deviceKey}/rpc`)
  whose `ConfirmKey` is the device's own `output` telemetry key — a command is confirmed only
  when the relay itself reports back that it changed, never merely when the app manages to
  publish. See `services/commands.go.md` for the gates that guard every device carrying this
  profile.
- **`smart-lamp`** (Zigbee2MQTT conventions, home-automation) — the worked example for the
  richer command kinds: `power` (`switch`), `brightness` (`dimmer`, 0..100), `color_temp`
  (`cct`, bounded `2200..6500` K) and `color` (`color`, packed `0xRRGGBB`, substituting
  `{r}`/`{g}`/`{b}` into its payload template). `power`/`brightness`/`color_temp` each declare a
  `ConfirmKey` against the bulb's own reported telemetry; `color` declares none — a bulb that
  reports colour back per-channel cannot be equality-confirmed against one packed float, so
  "sent, never confirmed" is the honest status for it. See `services/commands.go.md` for the
  kinds themselves.
- **`generic-sunspec-solar` (P5, `Transport: "modbus"`, `ModbusMode: "sunspec"`)** — declares NO
  register bindings; the driver walks the SunSpec model chain and DISCOVERS the keys, so this ONE
  profile reads any compliant inverter/meter/battery (SolarEdge, SMA, Fronius, most Sungrow, the
  SunSpec-103 block a Huawei exposes) with no per-model work. Its role-prefixed keys
  (`inv_`/`grid_`/`batt_`/`ctl_`) exist only to attach a deadband/heartbeat/range to the
  datapoints worth storing; a device lacking a block (a meter with no battery) simply never sends
  those keys.
- **`huawei-sun2000` (P5, `Transport: "modbus"`, `ModbusMode: "regmap"`)** — the vendor-map path
  for the world's most-installed inverter: NOT fully SunSpec, since its LUNA battery (SOC,
  charge/discharge power) and built-in power meter live in Huawei's own registers, so every key
  carries an explicit `Register`/`RegKind`/`ScaleFactor` binding per Huawei's published Modbus
  interface definitions. It is the worked example a site copies to onboard any other non-SunSpec
  hybrid: a new device is a new map, not new code. The battery/meter registers sit ~5,700 apart
  from the inverter block, which is exactly the scattered-map shape `infra/iot/modbus.RegisterMap`
  clusters into separate bounded reads (`regmap.go.md`).
- **`sungrow-sh-hybrid` (`Transport: "modbus"`, `ModbusMode: "regmap"`)** — Sungrow is #2 in global
  inverter shipments; its SH residential hybrids are common enough to be worth a dedicated map. Its
  telemetry lives in **INPUT registers** (`RegInput: true` on every key, fn 4) and its 32-bit
  values are **word-swapped** (`WordSwap: true`, low word first) — neither of which the Huawei map
  needed, and the reason both fields exist on the binding. It is also the first built-in profile to
  pre-declare Modbus **commands** (`ems_mode`, `batt_force`, `batt_force_power`, `export_limit`,
  `export_limit_enable`, `batt_min_soc`, `batt_max_soc`) — every one stays INERT until an admin
  turns the device's `ActuationEnabled` on AND bench-verifies the register: sign/scale/units
  genuinely differ by SH model and firmware (`batt_force_power`'s unit alone is watts on some
  models and percent on others). See `apps/myiotsan/kb/solar/sungrow-sh.md`.
- **`deye-hybrid` (`Transport: "modbus"`, `ModbusMode: "regmap"`)** — the OEM behind a large slice
  of the budget hybrid market; the SAME map answers for Deye, Sunsynk, and Sol-Ark rebadges.
  All-holding (no `RegInput`), signed 16-bit values, no word-swapping needed. Commands
  (`work_mode`, `solar_sell`, `grid_charge`, `max_sell_power`) carry the same bench-verify caveat —
  the register convention (A vs B) differs by model. See `apps/myiotsan/kb/solar/deye-hybrid.md`.
- **`eastron-sdm630-meter` (`Transport: "modbus"`, `ModbusMode: "regmap"`)** — the cheap 3-phase
  meter a site adds when the inverter itself cannot see the grid. **Read-only** (a meter has
  nothing to actuate) and the first profile needing **`RegKind: "f32"`** — its values are IEEE-754
  float32 over input registers, big-endian, no word swap. See
  `apps/myiotsan/kb/solar/eastron-sdm630.md`.

## Notes

- Consumed exclusively by `services.ProfileService.EnsureBuiltins`
  (`apps/myiotsan/services/profile.go.md`).
- `builtinProfile`/its `Keys []SaveTelemetryKey` and (P4) `Commands []SaveProfileCommand` reuse
  the same DTOs the profile CRUD API takes, so a builtin is inserted through the identical
  `replaceKeys`/`replaceCommands` path a user-authored profile would use. Most builtins declare
  no commands at all, and that is the correct default: a sensor that cannot be commanded cannot
  be commanded wrongly.
