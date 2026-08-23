# Module: domain/shared/fleetnode/doc.go

## Purpose

Package overview for `fleetnode` — the NODE side of the fleet: how an appliance discovers a
control plane, gets adopted by it, enrolls for an mTLS certificate, holds open a control
channel, and pushes its events up.

## Responsibilities

- Documents why the package exists here rather than under `apps/mymatasan`: it was never
  camera-specific. `mymatasan` grew it first; `myiotsan` needed exactly the same thing, and
  `mypintusan` now uses it too (`apps/mypintusan/app/wire_fleet.go.md`). A second copy of a
  pairing/enrollment stack would be a second copy of a SECURITY PROTOCOL, and the copies would
  drift the first time one of them was fixed.
- States what stays per-app: only the app's NAME and its KIND (`fleetnode.KindCamera` /
  `fleetnode.KindIot` / `fleetnode.KindDoor`) — what the node reports so the control plane knows
  what it adopted.
- `isNoResultFoundErr(err)` — shared sentinel-message check ("no result found" /
  "total affected: 0") used across the package's repo lookups, since the generic repo signals
  "row not present" by message rather than a typed error.

## Package Contents

| File | Purpose |
|---|---|
| `pairing.go` | `IPairingService` — the node's single-parent lock state machine (fleet key, discovery announce, claim-code adopt/release/unpair, node identity). See `pairing.go.md`. |
| `enrollment.go` | `EnrollmentManager` — post-adoption mTLS certificate lifecycle (CSR, renewal, the mTLS management listener). See `enrollment.go.md`. |
| `control_channel.go` | `ControlChannelManager` — the persistent node-dialed control channel: dials the parent over mTLS, serves tunnelled parent→node commands via an injected `ControlDispatcher`, and forwards node events upstream (`ForwardEvent`). Reconnects with backoff; gated on paired+enrolled. **W2-6/F-11**: `ForwardEvent`'s two failure paths (channel down, write error) used to return silently with no record of the loss; both now call `noteDrop(kind, reason)` (`reason` = `disconnected` \| `write_failed`), and a successful forward is counted too via the optional `SetMetrics(telemetry.Metrics)` (nil-safe; a node without one behaves exactly as before). `SetMetrics` publishes both `kopiv2_control_events_forwarded_total{kind}` and `kopiv2_control_events_dropped_total{kind,reason}` at **zero** for the known kinds at startup, so "never dropped anything" and "not instrumented" don't look identical on the scrape. `DroppedSinceConnect()` reports the running count since the last successful hello; `connectAndServe` reads it into the new `Frame.Dropped` field on the outgoing Hello and clears it only after that hello succeeds, so a failed hello doesn't discard the number. The manager's `active` connection is now held as a narrow `frameWriter` interface (`WriteFrame(*control.Frame) error`), not `*control.Conn`, so both forwarding failure paths are unit-testable without a websocket. |
| `event_sink.go` | `NewControlEventSink` — a `notification.Channel` that forwards every published node notification up the control channel, so the control plane's unified feed receives node alerts in addition to the node's own local delivery. |
| `fake_repo_test.go`, `pairing_test.go`, `event_sink_test.go`, `control_channel_drops_test.go` | Test doubles and coverage for the above; not independently documented. |

## Notes

- `mymatasan`, `myiotsan` and `mypintusan` all consume this package directly — `mymatasan`
  through thin same-named aliases that preserve existing call sites (see
  `apps/mymatasan/services/fleetnode.go.md`); `myiotsan` and `mypintusan` call it straight
  (`apps/myiotsan/app/wire_fleet.go.md`, `apps/mypintusan/app/wire_fleet.go.md`).
- The one thing that is NOT shared here is what a node's events actually **mean** —
  `myseliasan`'s cross-domain correlator (`apps/myseliasan/services/correlate.go.md`) is what
  turns events pushed by this package into a fleet-wide conclusion; this package only gets
  them there.
