# Module: apps/myiotsan/app/wire_fleet.go

## Purpose

The node side of the fleet: `myiotsan` is adopted by `myseliasan` exactly the way `mymatasan`
is, on the same shared stack (`domain/shared/fleetnode`). Modeled directly on
`apps/mymatasan/app/wire_fleet.go`, with the one deliberate structural difference the type
below documents.

## TWO channels, not three

```go
type fleet struct {
    enrollment *fleetnode.EnrollmentManager
    control    *fleetnode.ControlChannelManager
    pairing    fleetnode.IPairingService
    httpsPort  int
}
```

`mymatasan` dials a THIRD channel — the media channel — to stream camera RTP up for live view
(see `docs/modules/apps/mymatasan/app/wire_fleet.go.md`). A sensor hub has no video, so
`myiotsan` does not open that port at all. An unused listener is an unused attack surface, and
the honest thing is not to have one.

## `buildFleet`

```go
func buildFleet(api *mux.Router, deps apphost.Dependencies, appVersion string, cipher *atrest.Cipher, notificationService *notification.Service) fleet
```

- Resolves `httpsPort` from the first configured TLS port (advertised in discovery announces).
- Builds `pairingService` via `fleetnode.NewPairingService(settingsRepo, cipher, "myiotsan", "", appVersion, fleetnode.KindIot)` — the node name defaults to the hostname; `fleetnode.KindIot` is the authoritative answer to "what did the control plane just adopt?", carried both as an unsigned discovery-announce hint and (authoritatively) in the adopt reply. See `docs/modules/domain/shared/fleetnode/pairing.go.md`.
- Builds `enrollment` (`fleetnode.NewEnrollmentManager`) from `deps.Config.Pairing.MTLSPort` / `RenewBeforeHours`.
- Builds `control` (`fleetnode.NewControlChannelManager`) with `dispatch = sharedapis.NewControlDispatcher(api, deps.AccessRoles)` — the dispatcher re-injects a tunnelled command into THIS app's own `/api` router, gated by the node's own authorization against the node's own roles. That matters more for a sensor hub than a camera: a tunnelled command that reached an unguarded path could switch a relay, and the node — not the parent — must decide who may do that.
- Registers `notificationService.Register(fleetnode.NewControlEventSink(control))` — every notification this node raises (a rule alert, a device gone silent, a relay command, a sign-in lockout) also flows up the channel into the control plane's unified feed. This is the line that makes the whole fourth app worth building: once `myseliasan` receives events from both camera and sensor nodes, it can correlate across them.

## `fleet.start`

Runs the discovery responder (answers authenticated probes while unpaired and a fleet key is
set, then goes silent once adopted — a node that keeps announcing itself after adoption is
advertising itself to whoever else is listening) and both channels, each
`safego.Supervise`d so a panic in either doesn't silently stop enrollment or the control
connection with nothing logged to say why.

## Notes

- Everything here is **node-dialed**. The control plane never needs a route back to the node — `myiotsan` reaches out, which is what lets it sit behind NAT on a building's LAN with no inbound firewall rule, and is also why adoption is a deliberate act rather than something a network scanner can do to you.
- `buildFleet` only constructs the managers; `RegisterAppRoutes` in `app.go` calls `f.start` after registering the pairing routes — see `docs/modules/apps/myiotsan/app/app.go.md` "Fleet (P6)".
