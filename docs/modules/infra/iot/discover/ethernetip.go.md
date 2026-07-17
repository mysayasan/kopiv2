# Module: infra/iot/discover/ethernetip.go

## Purpose

The EtherNet/IP scanner: finds CIP devices (Allen-Bradley/Rockwell PLCs and drives, and other
EtherNet/IP-speaking I/O) by broadcasting the CIP-standard `ListIdentity` request on UDP 44818 and
parsing the `Identity` replies. Read-only by protocol design — `ListIdentity` is the CIP "who are
you" discovery request; it changes nothing on the device.

## Key Function: ScanEtherNetIP

```go
func ScanEtherNetIP(ctx context.Context, timeout time.Duration) []Found
```

Broadcasts one `ListIdentity` request (command `0x0063`, empty body) to the IPv4 limited broadcast
address on port 44818, then collects replies until `timeout` (default 3s) or `ctx` is done,
deduped by source IP (one identity per host). Each valid reply is decoded by `parseENIPIdentity`
into a vendor id and product name, resolved to a human vendor name by `enipVendor`.

## Key Function: enipListIdentityRequest / parseENIPIdentity

```go
func enipListIdentityRequest() []byte
func parseENIPIdentity(resp []byte) (vendorID uint16, product string, ok bool)
```

`enipListIdentityRequest` builds the 24-byte encapsulation header for the request (command
`0x0063`, everything else zero — no session, no CIP payload needed for a broadcast discovery).
`parseENIPIdentity` walks the reply's CPF (Common Packet Format) item list looking for the
Identity item (type `0x000C`), then reads its fixed-offset fields: vendor id at a 2-byte offset
after the socket-address block, and the product name as a length-prefixed string further in. Bytes
are little-endian throughout, per the CIP encapsulation spec.

## Key Function: enipVendor

```go
func enipVendor(id uint16) string
```

Names a small, hardcoded set of common CIP vendor ids (Rockwell/Allen-Bradley = 1, Schneider = 5,
Festo = 26, Omron = 108, HMS/Anybus = 283); anything else is shown numerically (`"vendor %d"`)
rather than silently blank, so an unrecognised vendor is still visible, just not translated.

## Notes

- **Parser-verified only, not hardware-verified.** `discover_test.go`'s `TestParseENIPIdentity`
  builds a synthetic ListIdentity reply byte-for-byte (per the CIP spec's field layout) and checks
  `parseENIPIdentity` decodes it correctly; no real EtherNet/IP PLC was available in CI to confirm
  a live device's reply matches this byte-for-byte. See `docs/DISCOVERY_SCANNING.md`'s honesty
  note: validate against your own hardware before relying on this in production.
- Broadcasts to the IPv4 limited broadcast address (`255.255.255.255`), not a subnet-directed
  broadcast — reaches only the local L2 segment the app's host is on, same scope every other
  scanner in this package operates within (LAN-local).
