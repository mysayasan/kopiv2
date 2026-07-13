# Module: apps/mymatasan/app/wire_fleet.go

## Purpose

`buildFleet` constructs the node side of the myseliasan control plane: the three
node-dialed channels (enrollment, control, media) this app maintains once adopted. Moved
out of `app.go` (Tier 2 phase D2).

## Responsibilities

- `type fleet struct { enrollment *services.EnrollmentManager; control
  *services.ControlChannelManager; media *services.MediaChannelManager; httpsPort int }`
  — `httpsPort` is the first configured TLS port, advertised to the control plane in
  discovery announces and used as the adoption-call origin.
- `buildFleet(api *mux.Router, deps, appVersion string, pairingService, cameraService,
  streamManager, notificationService) fleet`:
  - Builds `enrollment` (`services.NewEnrollmentManager`) — after adoption, enrolls with
    the control plane's fleet CA, serves the mutual-TLS management listener
    (heartbeat/release), and renews its certificate before expiry.
  - Builds `control` (`services.NewControlChannelManager`) — once paired and enrolled,
    dials the control plane's control listener over mTLS and maintains a persistent
    bi-directional channel (commander commands down, events up). The dispatcher
    (`apis.NewControlDispatcher(api)`) re-injects tunnelled commands into this app's OWN
    `/api` router, so the commander reaches the node's exact API surface, gated by the
    node's normal authorization via the synthetic principal the command carries — there is
    no second, weaker path into the node.
  - Registers `notificationService.Register(services.NewControlEventSink(control))` so this
    node's notifications are forwarded up the control channel to the control plane's
    unified feed, in addition to local delivery.
  - Builds `media` (`services.NewMediaChannelManager`) — a second node-dialed mTLS
    connection (separate port) that streams a camera's RTP up to the control plane on
    request, so myseliasan can re-broadcast full-frame-rate live view over WebRTC.
    Resolves each camera's RTSP source via the same `cameraService.SnapshotSource` path
    browser live view uses, and shares the stream manager's RTSP sessions rather than
    opening its own.

## Notes

- All three channels are **dialed BY the node**, never accepted from it — that is the
  whole security posture of the fleet: the control plane never needs a route to the node,
  so a node can sit behind NAT on a customer's network with no inbound ports open.
- Returned but not started: `buildFleet` only constructs the managers. They are started
  later, alongside the other background workers (see `wire_monitors.go.md`), so they share
  the monitor lifecycle (`monitorCtx`) and are all now `safego.Supervise`d — see the
  "Latent bug fix" section of `docs/modules/apps/mymatasan/app/app.go.md`.
- Pure move from `app.go`; no behavior change beyond the supervision fix noted above.
