# Module: infra/iot/codec/codec_test.go

## Purpose

Pins the codec's field-reality behaviours down so nobody "fixes" them into something more
naive later.

## Responsibilities

- `TestDecodeJSON_Zigbee2MQTTShape` — a realistic multi-key payload decodes cleanly.
- `TestDecodeJSON_AbsentKeyIsSkippedNotZeroed` — an absent key produces no sample at all.
- `TestDecodeJSON_UnparseableNumberIsNotZero` — `"unavailable"` on a numeric key yields no
  sample rather than `0`.
- `TestDecodeJSON_DottedPath` — a nested field (`aenergy.total`) resolves via `Binding.Path`.
- `TestDecodeJSON_BooleanSpellings` — every spelling `boolWord` accepts (`true`/`"ON"`/`"off"`/
  `"open"`/`"closed"`/`"1"`/`"0"`) coerces to the right 1/0.
- `TestDecodeJSON_MalformedPayloadIsAnError` — non-JSON input is a reported error, not silently
  ignored.
- `TestDecodeJSON_StringKeyStaysAString` — a string-typed key (e.g. a badge id) is never
  coerced into a number.
- `TestDecodeRaw_BareValueOnItsOwnTopic` — the raw/per-key-topic decode path.

## Notes

- Pure unit tests against `codec.go`; no MQTT, no database, no device state.
