# Module: apps/mymatasan/entities/onvif_device.go

## Purpose

Defines the persisted ONVIF device record for standalone `mymatasan`.

## Fields

- Service identity: `XAddr`, `Host`, `Port`, `Name`.
- Device metadata: `Types`, `Scopes`, `HardwareID`, `Manufacturer`, `Model`, `FirmwareVersion`, `SerialNumber`.
- ONVIF media state: `MediaXAddr`, `ProfileToken`, `RTSPUrl`, `SnapshotURI`.
- ONVIF PTZ state: `PTZXAddr`, `PTZSupported`.
- Camera credentials: `Username`, `Password`.
- Runtime state: `RTSPStatus`, `RTSPTransport`, `RTSPTracks`, `LastStreamCheckAt`, `LastSeenAt`, `IsActive`.
- Audit fields: created/updated user and timestamps.

## Notes

- `XAddr` is the unique key used for idempotent save/upsert behavior.
- `PTZSupported` is refreshed from ONVIF capabilities so the live-view UI can show directional controls only when the camera exposes a PTZ service.
- Bootstrap creates this table from the registered entity when SQLite or another supported DB engine starts.
