# Module: infra/onvif/parse.go

## Purpose

Parses ONVIF SOAP XML into small normalized data structures.

## Responsibilities

- Parse WS-Discovery `ProbeMatches`.
- Parse ONVIF `GetDeviceInformationResponse`.
- Extract human-friendly hints such as device name and hardware ID from ONVIF scopes.
- Parse every ONVIF media profile token, name, codec, and resolution from `GetProfiles`.
- Pick a preferred media profile for live view, favoring H264 profiles over MJPEG/H265 when multiple profiles are available.
- Keep namespace handling simple by matching XML local names.
- Parse local ONVIF user accounts from `GetUsersResponse` (`ParseUsers` → `[]User`).
- Parse the device's `SystemRebootResponse` message (`ParseRebootMessage`, best-effort).
- Parse `GetSystemDateAndTimeResponse` into `SystemDateTime` (`ParseSystemDateTime`), including the UTC date/time as an RFC3339 string.
- Parse `GetNetworkInterfacesResponse` into `NetworkConfig`/`NetworkInterface` (`ParseNetworkInterfaces`), including each NIC's MAC address (`Info.HwAddress`) and DHCP vs manual IPv4 config.
- Parse `GetNetworkDefaultGatewayResponse` (`ParseNetworkGateway`) and `GetDNSResponse` (`ParseDNS`, returning servers + whether DNS is DHCP-sourced).
- Parse `GetServicesResponse` into `Service` entries (namespace, XAddr, version) via `ParseServices`.

## Notes

- Discovery responses may include multiple `XAddrs`; the first address is used as the canonical device-service URL.
- Scope strings are stored as raw tokens so the app can persist original device metadata.
- The best-effort parsers (`ParseRebootMessage`, `ParseNetworkGateway`, `ParseDNS`) swallow XML unmarshal errors and return zero values rather than propagating an error, since callers treat them as optional enrichment on top of a primary call that already succeeded.
