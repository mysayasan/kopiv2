# Module: apps/myiotsan/entities/discovered_device.go

## Purpose

A candidate, not an inventory record: a device that announced itself during an enrollment
window, OR was found by an active network scan, but has not been adopted. It exists to resolve a
real tension in the design — the broker's security model is that a device not in `iot_device`
cannot connect at all, but a building with two hundred door contacts cannot be onboarded by
typing two hundred device keys by hand, and a device that does not exist yet obviously cannot
announce itself. See `services/enrollment.go.md` for the announce-path resolution (a time-boxed,
key-gated, quarantined window) and `services/scanner.go.md` for the active-scan path that now
feeds this same table.

## Fields

- `Id`.
- `DeviceKey` (`ukey`) — the client id the device announced itself as; one row per candidate.
- `Topic` — where it published, half of what identifies its type (the profile's topic template
  is the other half).
- `Payload` — ONE observed message sample, truncated to `maxPayloadSample` (2000 bytes) — enough
  for an admin to see what the thing actually sends, not a telemetry store.
- `ObservedKeys` — comma-separated top-level field names seen in the payload; what the profile
  suggester matches against.
- `SuggestedProfileId` — the best-matching profile, or `0` when nothing matched well enough (see
  `Enrollment.suggestProfile`'s 0.6 floor).
- `MessageCount` — how many payloads this candidate has sent, or how many times a scan has seen
  it. A device that has spoken forty times is real; one that spoke once may have been a passing
  scan.
- `FirstSeenAt`/`LastSeenAt` — unix seconds.
- `Source` — how this candidate was found: empty or `"mqtt"` for the original announce path, or
  one of `"modbus"`/`"mdns"`/`"ssdp"`/`"ethernetip"`/`"bacnet"` for an active-scan hit
  (`services/scanner.go.md`). A scan candidate never connected to the broker; it was found on the
  network and is quarantined the same way — nothing is stored or actuated until an admin adopts
  it.
- `Address` — where a scanned device lives (an IP, or `ip:port`). Empty for an MQTT candidate.
- `Endpoint`/`Unit`/`Transport` — pre-fill the created device's connection when a MODBUS-scan
  candidate is adopted (`services/enrollment.go.md`'s `Adopt`), so "found → adopt → polling"
  needs no manual re-typing. Empty/zero for every non-Modbus candidate.

## Notes

- Written by `services.Enrollment.Observe` for a QUARANTINED (`Enrolling`) MQTT session — see
  `services/ingest.go.md` — and by `services.ScanService.recordCandidate` for a network-scan hit
  (`services/scanner.go.md`), deduped by a different synthetic key per source (device key for
  MQTT, `scanDeviceKey` for a scan). A candidate row is never promoted to telemetry
  automatically; `Enrollment.Adopt` reads it to provision a real `IotDevice` and then deletes the
  candidate row, whichever path created it.
- Capped at `maxCandidates` (500) so a flood during an open window OR a large scan cannot fill
  the disk — both write paths share the one cap.
- Bootstrap creates this table from the registered entity (`app/app.go`'s `Entities()`) when
  SQLite or another supported DB engine starts.
