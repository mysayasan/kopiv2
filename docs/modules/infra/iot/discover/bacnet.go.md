# Module: infra/iot/discover/bacnet.go

## Purpose

The BACnet scanner: finds BACnet/IP devices (building-automation controllers, VAV boxes, meters) by
broadcasting the BACnet-standard `Who-Is` request on UDP 47808 and parsing `I-Am` replies.
Read-only — `Who-Is`/`I-Am` is BACnet's own discovery handshake; nothing here writes a property or
issues a command.

## Key Function: ScanBACnet

```go
func ScanBACnet(ctx context.Context, timeout time.Duration) []Found
```

Broadcasts one unconstrained `Who-Is` (BVLC Original-Broadcast-NPDU) to the IPv4 limited broadcast
address on port 47808, collects `I-Am` replies until `timeout` (default 3s) or `ctx` is done,
deduped by source IP. Each parsed reply yields a `Found` named `"BACnet device <instance>"`, with
`Detail` carrying both the device instance and the vendor id.

## Key Function: bacnetWhoIs / parseBACnetIAm

```go
func bacnetWhoIs() []byte
func parseBACnetIAm(pkt []byte) (instance uint32, vendorID uint32, ok bool)
```

`bacnetWhoIs` builds the fixed 8-byte unconstrained Who-Is frame: BVLC header (type `0x81`, function
`0x0b` = Original-Broadcast-NPDU, length 8), a plain NPDU (version 1, no special control flags), and
the APDU (unconfirmed-request, service Who-Is). `parseBACnetIAm` locates the APDU (falling back to
a raw byte-pattern search for the `0x10 0x00` unconfirmed-I-Am marker if the NPDU carried optional
fields this parser does not walk), then decodes the sequence of BACnet application-tagged
primitives that follow: the device's object identifier (tag 12, whose low 22 bits are the instance
number) and the LAST unsigned primitive (tag 2) in the fixed I-Am sequence, which is the vendor id.

## Notes

- **Parser-verified only, not hardware-verified** — the same honest limitation as
  `ethernetip.go`. `discover_test.go`'s `TestParseBACnetIAm` builds a synthetic I-Am
  byte-for-byte against the BACnet application-tag encoding and checks the instance/vendor id
  decode correctly; no real BACnet device was available in CI. Validate against your own
  hardware before relying on this in production (`docs/DISCOVERY_SCANNING.md`).
- `apduStart`/`indexOf`/`beUint` are small private helpers `parseBACnetIAm` uses to locate the
  APDU and read a big-endian unsigned primitive of arbitrary byte length (BACnet's application
  tags do not fix an unsigned's width).
- Broadcasts to the IPv4 limited broadcast address, reaching only the local L2 segment — the same
  LAN-local scope as `ethernetip.go`.
