# Module: apps/myiotsan/entities/discovered_device.go

## Purpose

A candidate, not an inventory record: a device that announced itself during an enrollment
window but has not been adopted. It exists to resolve a real tension in the design — the
broker's security model is that a device not in `iot_device` cannot connect at all, but a
building with two hundred door contacts cannot be onboarded by typing two hundred device keys
by hand, and a device that does not exist yet obviously cannot announce itself. See
`services/enrollment.go.md` for the resolution (a time-boxed, key-gated, quarantined window).

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
- `MessageCount` — how many payloads this candidate has sent. A device that has spoken forty
  times is real; one that spoke once may have been a passing scan.
- `FirstSeenAt`/`LastSeenAt` — unix seconds.

## Notes

- Written only by `services.Enrollment.Observe`, which is reached only for a QUARANTINED
  (`Enrolling`) MQTT session — see `services/ingest.go.md`. A candidate row is never promoted to
  telemetry automatically; `Enrollment.Adopt` reads it to provision a real `IotDevice` and then
  deletes the candidate row.
- Capped at `maxCandidates` (500) so a flood during an open window cannot fill the disk.
- Bootstrap creates this table from the registered entity (`app/app.go`'s `Entities()`) when
  SQLite or another supported DB engine starts.
