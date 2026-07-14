# Module: domain/shared/fleetnode/doc.go

## Purpose

Package overview for `fleetnode` — the NODE side of the fleet: how an appliance discovers a
control plane, gets adopted by it, enrolls for an mTLS certificate, holds open a control
channel, and pushes its events up.

## Responsibilities

- Documents why the package exists here rather than under `apps/mymatasan`: it was never
  camera-specific. `mymatasan` grew it first; `myiotsan` needs exactly the same thing. A
  second copy of a pairing/enrollment stack would be a second copy of a SECURITY PROTOCOL,
  and the two would drift the first time one of them was fixed.
- States what stays per-app: only the app's NAME and its KIND (`fleetnode.KindCamera` /
  `fleetnode.KindIot`) — what the node reports so the control plane knows what it adopted.
- `isNoResultFoundErr(err)` — shared sentinel-message check ("no result found" /
  "total affected: 0") used across the package's repo lookups, since the generic repo signals
  "row not present" by message rather than a typed error.

## Package Contents

| File | Purpose |
|---|---|
| `pairing.go` | `IPairingService` — the node's single-parent lock state machine (fleet key, discovery announce, claim-code adopt/release/unpair, node identity). See `pairing.go.md`. |
| `enrollment.go` | `EnrollmentManager` — post-adoption mTLS certificate lifecycle (CSR, renewal, the mTLS management listener). See `enrollment.go.md`. |
| `control_channel.go` | `ControlChannelManager` — the persistent node-dialed control channel: dials the parent over mTLS, serves tunnelled parent→node commands via an injected `ControlDispatcher`, and forwards node events upstream (`ForwardEvent`). Reconnects with backoff; gated on paired+enrolled. |
| `event_sink.go` | `NewControlEventSink` — a `notification.Channel` that forwards every published node notification up the control channel, so the control plane's unified feed receives node alerts in addition to the node's own local delivery. |
| `fake_repo_test.go`, `pairing_test.go`, `event_sink_test.go` | Test doubles and coverage for the above; not independently documented. |

## Notes

- Both `mymatasan` and `myiotsan` consume this package directly (`myiotsan`) or through thin
  same-named aliases that preserve existing call sites (`mymatasan`, see
  `apps/mymatasan/services/fleetnode.go.md`).
- The one thing that is NOT shared here is what a node's events actually **mean** —
  `myseliasan`'s cross-domain correlator (`apps/myseliasan/services/correlate.go.md`) is what
  turns events pushed by this package into a fleet-wide conclusion; this package only gets
  them there.
