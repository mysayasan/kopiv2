# Module: infra/iot/discover/discover.go

## Purpose

Package-level types and the shared CIDR-expansion helper for the ACTIVE half of discovery: instead
of waiting for a device to announce itself over MQTT (the enrollment window, `services/
enrollment.go.md`), a scanner in this package sweeps a LAN for devices already present. Every
scanner is a distinct protocol — there is no single "scan for all IoT" call — but each normalises
its findings to `Found`, so `apps/myiotsan/services/scanner.go` can turn any of them into the
identical quarantined adoption candidate the announce path already produces.

**Not to be confused with `infra/discovery`** (mymatasan's camera ssdp/mdns/portscan discovery,
`MethodSSDPUPnP`/`MethodMDNS`/`MethodPortScan`/`MethodSADP`) — a separate, older package for a
separate device family (IP cameras), predating this one and left untouched.

**Safety posture, binding on every scanner in this package:** LAN-local (a bounded subnet sweep or
link-local multicast — never anything routed to the internet), READ-ONLY (nothing here ever writes
to a discovered device — no scanner in this package issues a Modbus write, a control command, or
any other side-effecting call), bounded (a host cap, a per-operation timeout, a concurrency cap —
see `Hosts`), and cancellable via `context.Context`. Scanning an industrial network can perturb
fragile gear even with a read-only probe (a flood of connects can still wedge an old PLC's TCP
stack), so callers are expected to make scanning an opt-in, admin-triggered act — see
`apps/myiotsan/apis/discovery.go.md`'s `POST /api/discovery/scan`.

**LAN-local is ENFORCED by `Hosts`, not merely asserted in this comment** — see below. For the
multicast scanners (mDNS/SSDP/EtherNet-IP/BACnet) that was always true by construction; they take
no target and broadcast to a fixed address. The Modbus sweep takes its target from a string an
admin types, and until a live bench (`tools/fleetbench/bench_iotsan_discovery.py`) found the gap,
nothing checked it — a running appliance accepted and really ran a sweep of public address space
or of its own loopback.

## Key Type: Found

```go
type Found struct {
    Source    string // "modbus" | "mdns" | "ssdp" | "ethernetip" | "bacnet"
    Address   string // ip, or ip:port
    Name      string // best friendly label
    Vendor    string
    Model     string
    Detail    string // free-form evidence shown to the admin
    Endpoint  string // Modbus-only: pre-fills the device on adoption
    Unit      int
    Transport string // "tcp" | "rtutcp"
}
```

The one shape every scanner in this package returns, regardless of protocol. `Endpoint`/`Unit`/
`Transport` are populated only by `ScanModbus` — they exist so an adopted Modbus candidate polls
immediately with no manual re-typing (`services/scanner.go.md`'s `recordCandidate`,
`services/enrollment.go.md`'s `Adopt`).

## Key Function: Hosts

```go
func Hosts(cidrs []string, max int) ([]string, error)
```

Expands a list of CIDRs (and bare host/IP entries) into a flat IP list, refusing outright — rather
than silently truncating — once the count would exceed `max`, **and refusing any target outside LAN
address space**. This is the one funnel every sweep target passes through, so it is the one place
that has to hold both bounds: `apps/myiotsan/services/scanner.go` calls it with `scanHostCap`
(1024) before `ScanModbus` ever dials anything.

The LAN-local check runs before a CIDR is expanded (a wide public range is refused on sight, not
after building and discarding a million strings) via `checkLANLocal(ip.Mask(ipnet.Mask).String())`,
and again per host as the list is built via the internal `add` closure — so a bare host list with
one bad entry is refused whole rather than silently scanning the good half. A bare hostname is
resolved (`checkLANLocal` calls `net.LookupIP`) and every address it yields must qualify; a target
that cannot be resolved is refused rather than scanned — fail closed.

## Key Function: checkLANLocal / isLANLocal

```go
func checkLANLocal(host string) error
func isLANLocal(ip net.IP) bool
```

The enforcement `Hosts` calls. `isLANLocal` accepts an address only if it falls inside `lanRanges`
— RFC 1918 (`10/8`, `172.16/12`, `192.168/16`), RFC 6598 CGNAT (`100.64/10`), RFC 3927 IPv4
link-local (`169.254/16`), and IPv6 unique-local/link-local (`fc00::/7`, `fe80::/10`) — and refuses
loopback, unspecified and multicast addresses outright even though none of those fall inside
`lanRanges` anyway (the explicit check makes the exclusion self-documenting rather than incidental).
Loopback is deliberately excluded: the package doc's LAN-local claim is about what the appliance can
reach on its network, not about its own interior, which a Modbus sweep has no legitimate reason to
probe. Public address space and loopback can both still be added by hand — this only constrains
*discovery*, not manual provisioning.

## Notes

- `incIP` is the private big-endian IP increment `Hosts` walks a `/n` with; both are covered by
  `discover_test.go`'s `TestHostsExpandsAndCaps` (a `/30` expands to exactly 4 addresses plus a
  bare host passed through, and a `/8` under a 1000 cap is refused).
- The LAN-local guard is covered by `discover_test.go`'s
  `TestHostsRefusesPublicAddressSpace`/`TestHostsRefusesTheApplianceOwnInterior`/
  `TestHostsAllowsEveryRangeASiteActuallyNumbersEquipmentOut_Of`/
  `TestHostsRefusesAMixedListRatherThanScanningTheGoodHalf`/`TestHostsRefusesATargetItCannotCheck`,
  and live, against a real appliance, by `tools/fleetbench/bench_iotsan_discovery.py` — a sweep of
  `192.0.2.0/24` (public) took 6.4s before the fix and is refused in 0.0s after it.
- Every other file in this package (`modbus.go`, `mdns.go`, `ssdp.go`, `ethernetip.go`,
  `bacnet.go`) implements one `ScanXxx(ctx, ...) []Found` function against this shared `Found`
  shape; none of them import each other.
