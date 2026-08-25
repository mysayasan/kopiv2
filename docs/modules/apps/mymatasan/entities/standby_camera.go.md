# Module: apps/mymatasan/entities/standby_camera.go

## Purpose

`StandbyCamera` — one camera belonging to **another** appliance that this one is prepared to
record if that appliance stops (W3-7, N+1 failover). Table `standby_camera`, created by the
auto-migrator (a new table, so no hand-written migration is needed).

## The two decisions the shape encodes

**It is a COPY of everything needed to open the camera, not a reference to anything.** At
the moment failover matters the appliance that held the original is unreachable — stolen,
dead power supply, failed switch — and nothing can be fetched from it. Whatever this node
did not already have, it never will. So the copy is taken while the other appliance is
healthy, and `StagedAt` is reported next to it, because a camera set copied three months ago
is a promise about a building that may since have gained cameras.

**It is NOT a camera row.** A staged camera does not appear in the camera list, is not
probed by the health monitor, is not recorded and is not on the wall. A spare covering four
recorders would otherwise show four sites' worth of cameras it is not watching, in a control
room, permanently. The camera row is created at **activation** and only then — see
`services/standby.go.md`.

## Fields

- Identity: `SourceNodeId` + `SourceCameraId`, a composite unique key (`ukey:"standby_src"`),
  so re-staging an unchanged fleet updates rows instead of accumulating duplicates.
  `SourceNodeName` is carried denormalized so this node can name the source on screen
  without asking a control plane it may not be able to reach.
- Camera: `Name`, `Description`, `Host`, `Port`, `RTSPUrl`, `SnapshotURI`, `RTSPTransport`,
  `XAddr`, `MediaXAddr`, `PTZXAddr`, `PTZSupported`, `ProfileToken`, `Username`.
- `Password` — `json:"-"` (never serialized, exactly as the camera table's own password is)
  and encrypted at rest with this appliance's own key. A staged set is somebody else's
  credentials on our disk; it gets the stronger treatment of the two, not the weaker.
- Recording intent: `RecordingWanted`, `RetentionDays`, `SegmentMinutes`, `PreRollSec`,
  `PostRollSec`. **`StoragePath` is deliberately not copied** — the other appliance's disk
  layout is not ours, and inheriting a path that does not exist here is how a takeover
  records nothing while reporting success. `RecordingWanted` carries whether the source was
  actually recording that camera: taking over continues what was happening rather than
  changing the site's policy.
- `LocalCameraId` — the camera row this appliance created when it took over, 0 before that
  ever happened. **Kept after a fail-back on purpose**: the footage recorded during the
  outage belongs to that row, and deleting the row would take the footage with it.
- `State` — `StandbyStateStaged` / `StandbyStateActive` / `StandbyStateReleased`.
- Drill result: `CheckStatus` (`StandbyCheckOK` / `Unauthorized` / `Unreachable`),
  `CheckDetail`, `CheckedAt`. A staged set that has never been drilled is a filing cabinet,
  not a failover — the spare may be on a different VLAN, the camera may reject a second
  concurrent login, the credentials may have been rotated on the camera and updated only on
  the appliance that owns it.

The unauthorized/unreachable split mirrors the camera service's own credential verdicts and
keeps the same distinction: a camera that **rejected** the login is a fact about the camera,
one that could not be reached is a fact about the network. Rolling the second into the first
sends an engineer to re-type a password that was always right.

## Related

`services/standby.go.md`, `apis/standby.go.md`, `apps/myseliasan/entities/failover_plan.go.md`.
