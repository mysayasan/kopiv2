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
- Resolve a preferred or selected RTSP stream URI with ONVIF `GetCapabilities`, `GetProfiles`, and `GetStreamUri`.
- Resolve all selectable RTSP stream options by running `GetStreamUri` for each media profile returned by `GetProfiles`.
- Resolve JPEG snapshot URIs with ONVIF `GetSnapshotUri`.
- Change camera-local ONVIF user passwords with Device Management `SetUser`.
- List, create, and delete camera-local ONVIF user accounts with Device Management `GetUsers`/`CreateUsers`/`DeleteUsers` (`GetUsers`, `CreateUser`, `DeleteUser`).
- Reboot a camera (`SystemReboot`) and restore factory defaults, Soft or Hard (`SetSystemFactoryDefault`).
- Read and write the camera clock: `GetSystemDateAndTime`/`SetSystemDateAndTime`, and NTP server configuration via `GetNTP`/`SetNTP`.
- Read and write a camera NIC's network configuration: `GetNetwork` (interfaces + gateway + DNS, via `GetNetworkInterfaces`/`GetNetworkDefaultGateway`/`GetDNS`) and `SetNetwork` (`SetNetworkInterfaces`, then optionally `SetNetworkDefaultGateway`/`SetDNS`).
- List the ONVIF services a device advertises with `GetServices`, used both for per-service capability chips and to read each service's version.
- Move and stop PTZ cameras with ONVIF `ContinuousMove` and `Stop`.
- Add WS-Security UsernameToken digest headers when camera credentials are supplied. The shared `soapEnvelope` declares the `tds`, `trt` (ver10 media), `tr2` (ver20 media), `tptz`, and `tt` namespaces; `tr2` supports the Media2 video-encoder set used by `encoder.go` for H.265 changes.

## Notes

- The package uses the Go standard library plus the existing UUID dependency.
- `NormalizeDeviceServiceURL` maps plain host/IP input to `/onvif/device_service`.
- Discovery defaults to `239.255.255.250:3702` and a three-second timeout.
- Discovery sends both a typed ONVIF video-transmitter probe and a broad probe because some devices ignore strict type filters.
- Discovery enrichment is bounded per device and runs in parallel; failures are ignored so protected or slow cameras still appear from WS-Discovery.
- Capability lookup tries `All` first, then falls back to separate `Media` and `PTZ` categories because some cameras reject broad capability requests.
- Stream URI resolution, stream-option enumeration, camera user updates, and PTZ SOAP behavior stay here; RTSP transport checks live in `infra/rtsp`.
- Device-Management operations (users, reboot, factory default, date/time, network) all live in the one mandatory device service, so `GetServices` cannot report per-operation support; callers instead probe each read call directly and treat success as "supported" (see `services.GetCameraCapabilities` in `apps/mymatasan`).
- `SetNetwork` defaults an out-of-range or unset `PrefixLength` to `/24`.
- `SetNTP` picks the ONVIF NTP entry type (`IPv4` vs `DNS`) per server by attempting to parse it as an IPv4 address.
