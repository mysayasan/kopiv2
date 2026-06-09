# Module: infra/onvif/client.go

## Purpose

Provides a lightweight ONVIF client for local device discovery, manual device-service probing, and media stream URI resolution.

## Responsibilities

- Send WS-Discovery `Probe` messages to the ONVIF multicast address.
- Send probes from the default UDP socket and each active multicast-capable IPv4 interface.
- Read ProbeMatch responses until the configured timeout expires.
- Normalize discovered service `XAddr` values into host, port, scope, and type fields.
- Enrich discovered devices with best-effort unauthenticated device information, capabilities, stream URI, and snapshot URI data.
- Probe a manually supplied IP, host, or ONVIF device-service URL.
- Attempt unauthenticated `GetDeviceInformation` enrichment while still accepting authorization-required device services as reachable.
- Resolve service capability URLs with ONVIF `GetCapabilities`, including media and PTZ service addresses.
- Resolve RTSP stream URIs with ONVIF `GetCapabilities`, `GetProfiles`, and `GetStreamUri`.
- Resolve JPEG snapshot URIs with ONVIF `GetSnapshotUri`.
- Change camera-local ONVIF user passwords with Device Management `SetUser`.
- Move and stop PTZ cameras with ONVIF `ContinuousMove` and `Stop`.
- Add WS-Security UsernameToken digest headers when camera credentials are supplied.

## Notes

- The package uses the Go standard library plus the existing UUID dependency.
- `NormalizeDeviceServiceURL` maps plain host/IP input to `/onvif/device_service`.
- Discovery defaults to `239.255.255.250:3702` and a three-second timeout.
- Discovery sends both a typed ONVIF video-transmitter probe and a broad probe because some devices ignore strict type filters.
- Discovery enrichment is bounded per device and runs in parallel; failures are ignored so protected or slow cameras still appear from WS-Discovery.
- Capability lookup tries `All` first, then falls back to separate `Media` and `PTZ` categories because some cameras reject broad capability requests.
- Stream URI resolution, camera user updates, and PTZ SOAP behavior stay here; RTSP transport checks live in `infra/rtsp`.
