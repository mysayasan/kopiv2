# Module: infra/iot/discover/mdns.go

## Purpose

The mDNS/DNS-SD scanner: browses the LAN over multicast DNS for a curated list of common service
types (`mdnsServices`) — Chromecast, AirPlay/HomeKit, Sonos, Spotify Connect, printers, Shelly,
ESPHome, and a generic `_http._tcp` catch-all — and returns each responder as a `Found`. Built on
`github.com/hashicorp/mdns` (a new, pure-Go dependency this feature pulls in, along with its own
dependency `github.com/miekg/dns`), rather than hand-rolling mDNS framing.

## Key Function: ScanMDNS

```go
func ScanMDNS(ctx context.Context, timeout time.Duration) []Found
```

Splits `timeout` (default 4s) evenly across every service type in `mdnsServices` (floored at
350ms each, so a short overall timeout still gives each type a fair, non-zero window), and for
each one runs `mdns.Query` with a buffered entry channel: a collector goroutine drains entries
into a deduped map (`address|instance name`) while `Query` blocks for that slice's timeout, then
the channel is closed and the collector allowed to finish before moving to the next service type
— sequential per-type, not one giant concurrent blast, so results stay ordered. IPv4 only
(`DisableIPv6: true`); an entry with no `AddrV4` is skipped as unusable for reaching the device.

## Key Function: friendlyMDNS

```go
func friendlyMDNS(name, service string) string
```

Turns a raw DNS-SD instance name like `"Living Room._googlecast._tcp.local."` into `"Living Room
(_googlecast._tcp)"` — the service-type suffix is DNS plumbing, not something an admin reviewing
candidates needs to parse themselves.

## Notes

- Read-only: `mdns.Query` only sends the standard DNS-SD query records and reads replies: nothing
  here is a control command.
- Many results here (a TV, a Chromecast) are discoverable but not controllable without a
  per-vendor driver this package does not build — the caller is expected to surface them
  honestly as "found" rather than implying they can be driven (see `apps/myiotsan/services/
  scanner.go.md`: only a SunSpec-identified Modbus device gets a profile suggested).
- Verified live against a real LAN in the discovery scanning live-boot (finding a mix of
  Chromecast/HomeKit/Shelly/printer-class devices, per `docs/DISCOVERY_SCANNING.md`) rather than
  only against synthetic data — mDNS multicast is hard to fake convincingly in a unit test, so
  none of the parsing here (beyond `friendlyMDNS`, which is pure string manipulation) has a
  dedicated unit test.
- `mdnsServices` is a fixed, curated list, not an exhaustive DNS-SD service-type registry —
  extending device-family coverage means adding an entry here, not rearchitecting the scan loop.
