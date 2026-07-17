# Module: apps/myiotsan/services/scanner.go

## Purpose

The ACTIVE half of discovery: instead of waiting for a device to announce itself over MQTT (the
enrollment window, `services/enrollment.go.md`), `ScanService` sweeps the LAN for devices already
present and turns each `discover.Found` into the SAME quarantined `DiscoveredDevice` candidate an
announcing device becomes. **A scan never adds a device** — it only proposes candidates an admin
then adopts through the identical review step, so the two onboarding paths converge on one
pipeline rather than becoming two things an admin has to learn.

Deliberately conservative, mirroring the properties `Enrollment`'s window already has: opt-in (an
admin triggers it via `POST /api/discovery/scan`, admin-only — `apis/discovery.go.md`), bounded
(host cap + per-scan timeout via `infra/iot/discover`), READ-ONLY (nothing in `infra/iot/discover`
writes to a device), and audited (every scan publishes a `notification.CategorySystem` event, the
same posture opening an enrollment window has).

## Key Type: ScanService

```go
func NewScanService(db dbsql.IDbCrud, profiles *ProfileService, audit func(ctx context.Context, msg string, data map[string]any), logf func(string, ...any)) *ScanService
func (s *ScanService) Scan(ctx context.Context, req ScanRequest, actor int64) (ScanResult, error)
```

`ScanRequest.Types` selects which of the five scanners to run (`modbus`/`mdns`/`ssdp`/
`ethernetip`/`bacnet`, matched case-insensitively); the rest (`CIDR`/`ModbusPort`/
`ModbusTransport`/`Units`) are Modbus-sweep-only parameters. `Scan`:

1. If `modbus` was requested, expands `CIDR` via `discover.Hosts` (capped at `scanHostCap`, 1024)
   — an empty expansion is a hard error ("a Modbus scan needs a network range"), not a silent
   no-op, since a Modbus scan with no target is very likely a mistaken request rather than an
   intentional empty one.
2. Runs each requested scanner (`discover.ScanModbus`/`ScanMDNS`/`ScanSSDP`/`ScanEtherNetIP`/
   `ScanBACnet`), collecting every `discover.Found` into one slice.
3. Resolves the seeded `generic-sunspec-solar` profile id once (`profileIDBySlug`), then calls
   `recordCandidate` for every `Found`.
4. Audits the run (`s.audit`, wired in `app.go` to publish a `notification.CategorySystem`/Info
   event) and returns a `ScanResult{Found, ByType}` — the per-type counts the UI's toast reads;
   the actual devices are in the candidate list, not this response.

`scanHostCap` (1024), `scanTimeout` (4s, the mDNS/SSDP/EtherNet-IP/BACnet per-scan window), and
`scanModbusPerOp` (800ms, the Modbus per-host/per-unit operation timeout) are this service's
tuning constants.

## Key Function: recordCandidate

```go
func (s *ScanService) recordCandidate(ctx context.Context, f discover.Found, sunspecID int64) bool
```

Upserts a `discover.Found` as a `DiscoveredDevice`, deduping on a synthetic key (`scanDeviceKey`)
so re-running a scan **refreshes** a still-present device (bumping `LastSeenAt`/`MessageCount`)
rather than duplicating it — an admin who scans twice a day should see one row per device, not a
growing pile. An identified SunSpec Modbus device (`f.Vendor` non-empty) gets `sunspecID`
suggested; every other source (unidentified Modbus, mDNS, SSDP, EtherNet/IP, BACnet) is left
unsuggested for the admin to classify — there is no reliable signal yet to guess a profile from a
TV or a PLC's identity reply. Refuses (returns `false`, logs) once `maxCandidates` (the same cap
`Enrollment.Observe` enforces, `services/enrollment.go.md`) is hit — the two onboarding paths share
one cap on the one table they both write.

## Key Function: scanDeviceKey

```go
func scanDeviceKey(f discover.Found) string
```

Builds the stable dedup key `recordCandidate` upserts on. A Modbus `Found` keys on
`endpoint+unit` (the thing that actually identifies a poll target — two scans of the same
inverter must collide even if its friendly name somehow changed). mDNS/SSDP key on
`address + a short hash of the name` (one IP can advertise several distinct mDNS/SSDP services,
so address alone would wrongly collapse them). EtherNet/IP and BACnet key on address alone (one
identity reply per host).

## Notes

- `ScanRequest`/`ScanResult` are the wire shapes `apis/discovery.go.md`'s `runScan` decodes/
  returns directly — no separate DTO layer.
- Constructed in `app.go` right after the enrollment wiring (`app/app.go.md`), with `audit`
  bound to `notificationService.Publish` and `logf` to the app logger — the identical pattern
  `NewEnrollment` uses for its own `logf`.
- `splitList` is a tiny private helper splitting `CIDR` on comma/space/newline/tab, so an admin
  can type `192.168.1.0/24, 10.0.0.0/28` or one per line interchangeably.
- No dedicated unit test file for this service (unlike `enrollment_test.go`) — verified instead
  by the live-boot in `docs/DISCOVERY_SCANNING.md` (scan → SunSpec-identify → candidate with
  endpoint/unit prefilled and `generic-sunspec-solar` suggested → adopt → device).
