# Module: apps/myiotsan/entities/profile_command.go

## Purpose

Declares something a device TYPE can be told to do, and the bounds within which it may be told
to do it. A device can only be commanded in ways its own profile declares — there is no generic
"publish this arbitrary payload to that topic" endpoint anywhere in the app, and that is
deliberate: an escape hatch like that would turn the appliance into a remote shell for the
building's electrics, and every safety property below would be a suggestion rather than a rule.
See `services/commands.go.md` and `docs/MYIOTSAN_PLAN.md` §3.4.

## Fields

- `ProfileId` (indexed `profile`) — the device type this command belongs to.
- `Name` — the command's identifier (`"output"`, `"setpoint"`); `Label` — display text; `Kind` —
  decides how the value is validated and rendered:
  - `"switch"` — 0/1 only.
  - `"setpoint"` — a number bounded by `Min..Max`.
  - `"dimmer"` / `"position"` (home-automation) — a percentage, fixed `0..100` (brightness, blind
    travel).
  - `"cct"` (home-automation) — colour temperature in Kelvin; a setpoint bounded by `Min..Max` the
    same way `"setpoint"` is.
  - `"mode"` (home-automation) — one of the integer values enumerated in `Options`, turning the
    command into a named dropdown rather than a raw number.
  - `"color"` (home-automation) — an RGB colour packed into one integer (`0xRRGGBB`); `Value` still
    rides through the single-float `DeviceCommand`/audit model unchanged.
  - **An unrecognised `Kind` is REFUSED, not silently passed** (`services.validateValue`'s
    `default` case) — closes a hole where a misconfigured/unknown kind used to publish
    unvalidated.
- `Transport` — decides HOW the command reaches the device: `""`/`"mqtt"` (default) PUBLISHES the
  payload template below; `"modbus"` WRITES a holding register on the polled device instead — the
  write half of the same driver the poller reads with (`services/modbus_poller.go.md`). A Modbus
  command goes through the identical gates in `CommandService.Issue` as an MQTT one; only the send
  step (`services.sendModbus` vs. the MQTT publish) differs. See `services/commands.go.md`.
- `Register`/`RegKind`/`ScaleFactor` (Modbus only) — `Register` is the holding register written;
  `RegKind` (`"u16"`/`"i16"` — single-register writes only, `services.encodeRegister` refuses
  `u32`/`i32` rather than half-write one) is how the value is encoded; `ScaleFactor` is the SAME
  multiplier the read-side `TelemetryKey` binding uses, applied in reverse: `raw = round(value /
  ScaleFactor)`. Authored per the vendor's Modbus map exactly like the read bindings — getting it
  wrong writes a wrong number to real hardware.
- `TopicTemplate` — where the command is published (MQTT only), `{deviceKey}` substituted.
- `PayloadTemplate` — the message body (MQTT only). `{value}` is substituted for every kind; a `"color"`
  command additionally substitutes `{r}`/`{g}`/`{b}` with the unpacked 0..255 channels. JSON in
  practice, e.g. `{"method":"Switch.Set","params":{"id":0,"on":{value}}}`. An empty template sends
  the bare value — for a device whose command topic IS the instruction.
- `Options` (home-automation) — a JSON array of `{"value":<int>,"label":<string>}` enumerating a
  `"mode"` command's allowed values; empty for every other kind. It BOUNDS a mode server-side the
  way `Min`/`Max` bound a setpoint: a value not in the list is refused, and an empty/malformed
  `Options` refuses every value (`services.modeValues`) — the same "an omission means no" rule the
  unbounded setpoint follows.
- `Min`/`Max` — bound a setpoint (or a `cct`), **enforced server-side** (`services.validateValue`),
  not merely rendered as input attributes in a form. "The frontend validates it" is not a safety
  property; a thermostat that accepts 200°C because a UI slider was bypassed is a fire. A setpoint
  declaring `Min == 0 && Max == 0` (i.e. no range at all) refuses **every** value — an unbounded
  setpoint on a physical device is an omission, not permission, and the safe reading of an
  omission is no.
- `ConfirmKey` — the telemetry key the device reports the resulting state back on, and what
  actually confirms a command. Without it, "sent" is the best that can ever be said, and "sent"
  is not "it happened". A relay command whose device reports `output` back is confirmed only
  when a reading on `output` matches the value that was asked for.
- Audit fields: `CreatedBy`/`CreatedAt`/`UpdatedBy`/`UpdatedAt`.

## Notes

- `services.ProfileService.replaceCommands` rewrites a profile's whole command set on save
  (delete-then-insert, same pattern as `replaceKeys` in `profile.go.md`) — a profile is a small
  declarative document, and an edit that half-applies is worse than one that replaces.
- Most of the shipped catalog (`profile_catalog.go.md`) declares no commands at all, and that is
  the correct default: a sensor that cannot be commanded cannot be commanded wrongly. `smart-relay`
  and, since the home-automation kinds, `smart-lamp` (`switch`/`dimmer`/`cct`/`color`) are the MQTT
  profiles that declare any. `generic-sunspec-solar`, `huawei-sun2000`, and
  `eastron-sdm630-meter` remain read-only (a meter has nothing to actuate, and the SunSpec/Huawei
  profiles simply never declared a command). `sungrow-sh-hybrid` and `deye-hybrid` are the first
  built-in Modbus profiles to declare commands (`Transport: "modbus"`) — seven and four
  respectively, all `Kind: "mode"`/`"setpoint"`, and all INERT until an admin enables the device's
  `ActuationEnabled` and bench-verifies the register (sign/scale/units differ by firmware — see
  `apps/myiotsan/kb/solar/`).
- Bootstrap creates this table from the registered entity (`app/app.go`'s `Entities()`).
