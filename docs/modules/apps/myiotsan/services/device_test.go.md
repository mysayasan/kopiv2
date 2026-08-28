# Module: apps/myiotsan/services/device_test.go

## Purpose

Pins the protocol guard (`supportedProtocols`/`supportedProtocolList`, `services/device.go.md`)
down as a closed set, since it is the only thing standing between a mistyped `Protocol` and a
device that is provisioned, enabled, and permanently unable to report anything.

## Responsibilities

- `TestSupportedProtocols_RejectsATransportWithNoRouteBehindIt` — `"http"` must not be a
  supported protocol while this app has no HTTP ingest route; pinned by name because that is
  exactly the value the Add-device form used to offer.
- `TestSupportedProtocols_KeepsTheTwoThatWork` — `"mqtt"` and `"modbus"` stay supported.
- `TestSupportedProtocolList_IsStableAndReadable` — `supportedProtocolList()` returns
  `"modbus or mqtt"`, sorted rather than map-ordered, since it is read by the operator inside
  `Create`'s refusal message.

## Notes

- Pure unit tests against `device.go`'s package-level `supportedProtocols` map; no database.
