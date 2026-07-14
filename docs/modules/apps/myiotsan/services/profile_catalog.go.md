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

Returns eight profiles, each a deliberate deadband/heartbeat choice:

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

## Notes

- Consumed exclusively by `services.ProfileService.EnsureBuiltins`
  (`apps/myiotsan/services/profile.go.md`).
- `builtinProfile`/its `Keys []SaveTelemetryKey` and (P4) `Commands []SaveProfileCommand` reuse
  the same DTOs the profile CRUD API takes, so a builtin is inserted through the identical
  `replaceKeys`/`replaceCommands` path a user-authored profile would use. Most builtins declare
  no commands at all, and that is the correct default: a sensor that cannot be commanded cannot
  be commanded wrongly.
