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

## Notes

- Discovery responses may include multiple `XAddrs`; the first address is used as the canonical device-service URL.
- Scope strings are stored as raw tokens so the app can persist original device metadata.
