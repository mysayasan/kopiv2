# Module: infra/iot/discover/ssdp.go

## Purpose

The SSDP/UPnP scanner: finds consumer/AV gear already on the LAN (Sonos, Roku, DLNA servers, some
smart plugs) by sending one M-SEARCH to the SSDP multicast group and collecting replies for a short
window. Read-only — a discovery probe, never a control action; SSDP's own spec makes M-SEARCH a
pure "who's listening" broadcast, so there is nothing here for a device to act on beyond replying.

## Key Function: ScanSSDP

```go
func ScanSSDP(ctx context.Context, timeout time.Duration) []Found
```

Opens an ephemeral UDP socket, writes one `M-SEARCH * HTTP/1.1` request to `239.255.255.250:1900`
with `ST: ssdp:all` (find everything, not one service type), then reads replies until `timeout`
(default 3s) or `ctx` is done. Dedupes on `(source IP, USN header)` so a device answering more than
once inside the window is reported once. `Name` prefers the `SERVER` header, falling back to `ST`,
then a generic `"UPnP device"` label; `Detail` carries the `ST` and, when present, the `LOCATION`
URL (where the device's UPnP description XML lives, for a human to inspect further — this scanner
does not fetch it).

## Key Function: parseSSDPHeaders

```go
func parseSSDPHeaders(resp string) map[string]string
```

A minimal, lower-cased HTTP-header-line parser for the M-SEARCH response (not a general HTTP
parser — SSDP responses are header-only, no body). Unit-tested directly by `discover_test.go`'s
`TestParseSSDPHeaders` against a synthetic Sonos-shaped reply.

## Notes

- `firstNonEmpty` (shared small helper) picks the first non-blank candidate label.
- Verified live: runs without error against a real LAN in the discovery scanning live-boot
  (`docs/DISCOVERY_SCANNING.md`), though the specific devices found depend on what is actually on
  that network.
- A device found here is not necessarily drivable — SSDP/UPnP finds a TV or a speaker, but
  actually commanding one needs a per-vendor integration this package does not attempt (see
  `docs/DISCOVERY_SCANNING.md`'s "Deferred" table, "Native TV/AV control").
