# Module: infra/iot/discover/discover_test.go

## Purpose

Unit tests for the pure, deterministic pieces of this package: CIDR expansion/capping and the
byte-level parsers for the two protocols that could not be verified against real hardware
(EtherNet/IP, BACnet) plus the SSDP header parser. Nothing here needs a network — every test
builds synthetic input in memory, which is why this package's live-boot verification
(`docs/DISCOVERY_SCANNING.md`) is a separate, manual step rather than part of `go test`.

## Responsibilities

- `TestHostsExpandsAndCaps` — `Hosts` expands a `/30` to its 4 addresses plus a bare host passed
  through unchanged (5 total, in order), and refuses (does not silently truncate) a `/8` under a
  1000-host cap.
- `TestParseENIPIdentity` — builds a synthetic ListIdentity reply byte-for-byte per the CIP
  encapsulation spec (protocol version, socket address, vendor id, device type, product code,
  revision, status, serial, length-prefixed product name, state) and asserts
  `parseENIPIdentity` decodes the vendor id and product name correctly, plus that `enipVendor`
  names vendor id 1.
- `TestParseBACnetIAm` — builds a synthetic I-Am (BVLC + NPDU + APDU + a BACnet object-identifier
  application-tag primitive for a device instance, plus max-APDU/segmentation/vendor-id
  primitives) and asserts `parseBACnetIAm` recovers the instance and vendor id.
- `TestParseSSDPHeaders` — a synthetic Sonos-shaped M-SEARCH response asserts the lower-cased
  header map carries `server`/`st`/`usn`/`location`.

## Notes

- No test exercises `ScanModbus`/`ScanMDNS`/`ScanSSDP`/`ScanEtherNetIP`/`ScanBACnet` themselves
  (the network-facing entry points) — those were verified by live-booting instead (Modbus/mDNS/
  SSDP against `tools/sunspec-sim` and a real LAN; EtherNet/IP and BACnet have no real device to
  live-boot against in CI, hence the parser-only tests here). See `docs/DISCOVERY_SCANNING.md`'s
  "How verified" column for the honest per-scanner breakdown.
- Table-stakes coverage, not exhaustive: e.g. `parseBACnetIAm`'s optional-NPDU-fields fallback
  path (the raw `0x10 0x00` byte search) is not separately exercised, since the constructed test
  packet uses a plain NPDU that hits the primary path.
