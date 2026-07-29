# Module: infra/iot/discover/modbus.go

## Purpose

The Modbus scanner. Sweeps a set of hosts for a Modbus device and, for each one found, tries to
identify it via SunSpec — the same self-describing walk `infra/iot/sunspec.Discover` already does
for the poller (`infra/iot/modbus/poller.go.md`), reused here rather than reimplemented so the scan
and the poller agree on what "identified" means.

**Deliberately gentle.** A subnet sweep against an industrial network is not free: every failed
connect costs the far side something, and a full unit-id walk against every host would be far
worse. So this scanner does the cheapest possible check first — a bare TCP connect to the port —
and only walks unit ids on a host that actually answered. A silent subnet costs one failed connect
per host, not a full walk; a responding host costs a handful of read-only SunSpec discovery
attempts, never a write.

## Key Function: ScanModbus

```go
func ScanModbus(ctx context.Context, hosts []string, port int, transport string, units []int, perOp time.Duration, concurrency int) []Found
```

For each host (bounded by `concurrency`, default 32, via a semaphore):

1. `net.DialTimeout("tcp", host:port, perOp)` — a bare connect probe, closed immediately.
   Anything that fails (refused, timed out, filtered) drops the host with **zero** further
   traffic sent to it — this is what makes a silent /24 cheap.
2. For each candidate `unit` id (default `[1]`), `probeModbusUnit` dials the real Modbus
   transport (`modbus.Dial` for `tcp`, `modbus.DialRTUTCP` for `rtutcp`) and calls
   `sunspec.Discover` — a couple of register reads, no writes, the safest possible
   identification available.
3. A unit that answers SunSpec yields a fully-identified `Found` (`Vendor`/`Model`/`Name` from
   the model-1 common block via `sunspec.DecodeCommon`, `Detail` naming the SunSpec base and
   unit, `Serial` appended when present).
4. A host that answered the TCP probe but no unit answered SunSpec still yields ONE `Found` —
   `"Modbus device (unidentified)"`, `Detail` pointing the admin at assigning a vendor
   register-map profile — because "something is there" is real information even when it cannot
   be auto-identified; silently dropping it would look like nothing was on that port at all.

`port` defaults to 502, `perOp` to 800ms, `concurrency` to 32, `units` to `[1]` when zero/empty/
unset.

## Key Function: probeModbusUnit / normTransport

```go
func probeModbusUnit(addr, transport string, unit byte, timeout time.Duration) (Found, bool)
func normTransport(t string) string // "" -> "tcp"; anything else case-insensitive-matched against "rtutcp"
```

`probeModbusUnit` is the per-unit worker: dial, `sunspec.Discover`, decode the common block if
model 1 is present, close. Returns `ok=false` (not an error) for "no SunSpec here" — the caller
(`ScanModbus`) treats that as "try the next unit", not a scan failure.

## Notes

- Read-only end to end: `sunspec.Discover`/`Walk` only ever calls `ReadHolding`/`ReadInput`
  (`infra/iot/modbus/client.go.md`) — nothing in this file, or anything it calls, ever writes a
  register.
- Verified live (not just unit-tested): a scan against `tools/sunspec-sim` correctly identified a
  SunSpec unit (vendor/model/serial) and prefilled its endpoint/unit/transport through adoption
  into a working, immediately-polling device (`docs/DISCOVERY_SCANNING.md`).
- `transport` here is `"tcp"` (Modbus TCP/MBAP) or `"rtutcp"` (RTU framing over a transparent
  TCP gateway) — the same two non-serial values `entities.IotDevice.Transport` accepts. A serial
  scan is out of scope: a serial sweep can only ever address one port at a time (no network to
  sweep), so there is nothing here to search for on a bus you have to already know the port name
  of.
- Called from `apps/myiotsan/services/scanner.go.md`'s `Scan`, with hosts already expanded and
  capped by `discover.Hosts` before this function ever runs.
- The dial target is built with `net.JoinHostPort(host, port)`, not `fmt.Sprintf("%s:%d", ...)` —
  the latter mis-forms an IPv6 literal host (`fe80::1:502` is ambiguous; it needs to be
  `[fe80::1]:502`), which `net.JoinHostPort` handles correctly for both address families.
