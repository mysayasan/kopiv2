# Module: infra/iot/codec/codec.go

## Purpose

Turns a device's raw MQTT/HTTP payload into typed `Sample`s. Every convention worth
supporting (Zigbee2MQTT, Tasmota, Shelly, ESPHome) publishes JSON, so JSON with a dotted path
is the whole of the common case; a device that publishes a bare value on a per-key topic is the
"raw" case.

## Key Type: Sample

One decoded datapoint: `Key`, `Num` (numbers and booleans, booleans as 0/1), `Str` (strings),
and `IsNum` — whether the payload actually yielded a number. `IsNum` exists so a key declared
numeric whose payload is `"unavailable"` (which Zigbee2MQTT really does send) does not
silently become `0`: a `0` is a reading, and 0 degrees is not the same thing as "no reading".

## Key Type: Binding

Where one key lives in the payload: `Key`, `Path` (dotted, e.g. `"battery.level"`; empty means
the key name is the field name), `Numeric` (true when the key is declared `"number"` or
`"bool"`).

## Key Function: DecodeJSON

```go
func DecodeJSON(payload []byte, bindings []Binding) ([]Sample, error)
```

Pulls the bound keys out of a JSON payload. Two absences by design, both load-bearing:

- **A key that is absent is SKIPPED, not zeroed.** A device that reports temperature and
  battery in one message and only temperature in the next has not set its battery to 0% —
  returning a sample for it would write a false reading and could fire a low-battery alert on a
  healthy device.
- **A numeric key whose value fails to parse (e.g. `"unavailable"`, `null`, garbage) yields no
  sample either**, via `coerce` — see below.

## Key Function: DecodeRaw

```go
func DecodeRaw(payload []byte, b Binding) (Sample, bool)
```

Treats the entire payload as one value for one key — the device that publishes `"21.4"` to
`home/kitchen/temperature`.

## Key Function: coerce

Converts a decoded JSON value into a `Sample` per the binding's declared type:

- `bool` → `Num` 0/1, `Str` the formatted bool.
- `float64` → `Num` directly.
- `string`, non-numeric binding → kept as `Str` (e.g. an access-reader badge id, never averaged).
- `string`, numeric binding → tries `strconv.ParseFloat` first, then `boolWord` (the spellings
  devices actually use — see below); if neither parses, returns `(Sample{}, false)` rather than
  a fabricated 0.
- `nil` or an object/array where a scalar was expected → not a reading.

## Key Function: boolWord

Maps the boolean spellings real devices emit onto 1/0: `on`/`true`/`open`/`yes`/`1`/`detected`/
`alarm`/`wet` → 1; `off`/`false`/`closed`/`no`/`0`/`clear`/`normal`/`dry` → 0. Booleans become
1/0 (not `true`/`false`) because that is what makes a door contact chartable and comparable —
"open for 4 of the last 10 minutes" is a question about a number.

## Notes

- Pure functions, no I/O, no device/profile awareness — `services.Ingest`
  (`apps/myiotsan/services/ingest.go.md`) is the caller that resolves a device's profile into
  `[]Binding` and hands the result to the deadband gate and the rule engine.
- Covered by `infra/iot/codec/codec_test.go` (`codec_test.go.md`): absent-key skip, the
  `"unavailable"` case, dotted paths, the boolean-spelling table, malformed-payload error, and
  string keys staying strings.
