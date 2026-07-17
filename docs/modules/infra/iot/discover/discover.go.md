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

Expands a list of CIDRs (and bare host/IP entries, passed through unchanged) into a flat IP list,
refusing outright — rather than silently truncating — once the count would exceed `max`. This is
the one load-bearing safety check that keeps a fat mask (a `/8` typed by mistake) from ever
launching millions of probes: `apps/myiotsan/services/scanner.go` calls this with `scanHostCap`
(1024) before `ScanModbus` ever dials anything.

## Notes

- `incIP` is the private big-endian IP increment `Hosts` walks a `/n` with; both are covered by
  `discover_test.go`'s `TestHostsExpandsAndCaps` (a `/30` expands to exactly 4 addresses plus a
  bare host passed through, and a `/8` under a 1000 cap is refused).
- Every other file in this package (`modbus.go`, `mdns.go`, `ssdp.go`, `ethernetip.go`,
  `bacnet.go`) implements one `ScanXxx(ctx, ...) []Found` function against this shared `Found`
  shape; none of them import each other.
