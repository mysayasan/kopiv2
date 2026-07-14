# Module: apps/mymatasan/services/fleetnode.go

## Purpose

Thin, behavior-preserving type/function aliases that keep `mymatasan`'s call sites unchanged
after the node-side fleet stack (pairing, mTLS enrollment, control channel, event sink) moved
to `domain/shared/fleetnode` (see `docs/MYIOTSAN_PLAN.md` §6/P6 and
`docs/modules/domain/shared/fleetnode/doc.go.md`).

## Responsibilities

- Type aliases: `IPairingService`, `PairingStatus`, `AdoptRequest`, `AdoptResult`, `Enrollment`, `EnrollmentManager`, `ControlChannelManager`, `ControlDispatcher` — all `= fleetnode.X`.
- Error aliases: `ErrPairingAlreadyPaired`, `ErrPairingBadAssertion`, `ErrPairingBadClaimCode`, `ErrPairingBadToken`, `ErrPairingFleetKeyUnset`, `ErrPairingFleetKeyShort` — all `= fleetnode.ErrX`.
- `NewPairingService(repo, cipher, name, version) IPairingService` — mymatasan's constructor, which calls `fleetnode.NewPairingService(repo, cipher, "mymatasan", name, version, fleetnode.KindCamera)`. This is the one place mymatasan states what kind of node it is: `KindCamera`, because a camera node has recordings and live views that a sensor node does not.
- `NewEnrollmentManager`, `NewControlChannelManager`, `NewControlEventSink` — `var` aliases directly to the `fleetnode` constructors.

## Notes

- Replaces the former standalone `apps/mymatasan/services/{pairing,node_enrollment,control_channel,control_event_sink}.go`, which now live at `domain/shared/fleetnode/{pairing,enrollment,control_channel,event_sink}.go`. See those docs for the full behavior.
- The media channel (`MediaChannelManager`) is NOT part of this alias set — it is camera-only (streams RTP for live view) and stays owned by `mymatasan`, wired in `apps/mymatasan/app/wire_fleet.go`. `myiotsan` deliberately does not have a media channel at all (see `docs/modules/apps/myiotsan/app/wire_fleet.go.md`).
- `apps/mymatasan/services/backoff.go` carries the small `sleepCtx`/`minDuration` helpers the media channel still needs, left behind when the control channel (which used to share them) moved out — see `backoff.go.md`.
