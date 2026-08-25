# Module: apps/mymatasan/apis/standby.go

## Purpose

The appliance's HTTP surface for N+1 failover (W3-7). Design lives in
`services/standby.go.md`.

## Routes (mounted on the protected router at `/api/standby`)

| Method | Path | Runs on | Purpose |
|---|---|---|---|
| GET | `/api/standby` | either | What this appliance holds, and for whom |
| GET | `/api/standby/handoff-key` | the spare | The one-exchange key a peer seals a set to |
| POST | `/api/standby/handoff` | the protected recorder | Seal THIS appliance's cameras for a named spare |
| POST | `/api/standby/stage` | the spare | Accept a sealed set |
| POST | `/api/standby/{nodeId}/drill` | the spare | Can this appliance actually open those cameras? |
| POST | `/api/standby/{nodeId}/activate` | the spare | Take the set over and start recording |
| POST | `/api/standby/{nodeId}/release` | the spare | Hand it back; keep the footage |
| POST | `/api/standby/{nodeId}/forget` | the spare | Drop a set this appliance no longer covers |

## Authorization

**Administrator only at every level, and not in any grantable page.** Two independent
reasons, either sufficient: `handoff` emits this appliance's entire camera set — sealed, but
it is still the one call that moves credentials off the box — and `activate` starts recording
forty cameras belonging to another site. Both are decisions about the estate, neither is a
shift task.

It is listed in `services/rbac.go`'s `Policy()` with no grants for viewer or operator, so the
catalog stays a **complete** description of the API surface: an area missing from it is an
area nobody can see they are not granting.

In practice these are called by the control plane over the fleet tunnel, which asserts the
`admin` role name and has the node evaluate its **own** matrix
(`sharedapis.NewControlDispatcher`) — the same path fleet policy enforcement uses. The
control plane gets no private capability here.

## Auditing

Every call, success and failure, via `Auditor` with `TargetType: services.TargetStandby` and
`TargetId` = the other appliance's node id. One audit row per **set**, not per camera: a
takeover is one decision about forty cameras, and forty rows would bury the decision in its
own consequences. The per-camera outcomes ride in the metadata, because "took over 40
cameras" with six of them not recording is the entry that has to be answerable later.

The handoff records the camera **count**, never the contents — a trail that carried the set
would be a second copy of the thing the sealing exists to protect.

## Error mapping

`ErrStandbyNoSuchSet` → 404, not 400: the control plane's sweeper asks for sets it may not
have created yet, and a 400 reads as a malformed request to whoever is looking at the log
rather than as the ordinary answer it is. Everything else → 400 with the service's own
wording, which is written to be read.

Bodies are capped at 8 MiB (a sealed set for a large site is base64, bounded well above the
512-camera service cap) and decoded with `DisallowUnknownFields`.
