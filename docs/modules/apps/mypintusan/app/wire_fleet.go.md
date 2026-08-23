# Module: apps/mypintusan/app/wire_fleet.go

## Purpose

The node side of the fleet: `mypintusan` is adopted by `myseliasan` exactly the way `mymatasan`
and `myiotsan` are, on the same shared stack (`domain/shared/fleetnode`). Modeled directly on
`apps/myiotsan/app/wire_fleet.go`, with the same two-channel shape and the door-specific stakes
the comments in this file spell out.

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
(see `docs/modules/apps/mymatasan/app/wire_fleet.go.md`). A door controller has no video, so
`mypintusan` does not open that port at all, same as `myiotsan`. An unused listener is an unused
attack surface, and the honest thing is not to have one.

## `buildFleet`

```go
func buildFleet(api *mux.Router, deps apphost.Dependencies, appVersion string, cipher *atrest.Cipher, notificationService *notification.Service) fleet
```

- Resolves `httpsPort` from the first configured TLS port (advertised in discovery announces).
- Builds `pairingService` via `fleetnode.NewPairingService(settingsRepo, cipher, "mypintusan", "", appVersion, fleetnode.KindDoor)` — the node name defaults to the hostname; `fleetnode.KindDoor` is the authoritative answer to "what did the control plane just adopt?", carried both as an unsigned discovery-announce hint and (authoritatively) in the adopt reply. See `docs/modules/domain/shared/fleetnode/pairing.go.md`.
- Builds `enrollment` (`fleetnode.NewEnrollmentManager`) from `deps.Config.Pairing.MTLSPort` / `RenewBeforeHours`.
- Builds `control` (`fleetnode.NewControlChannelManager`) with `dispatch = sharedapis.NewControlDispatcher(api, deps.AccessRoles)` — the dispatcher re-injects a tunnelled command into THIS app's own `/api` router, gated by the node's own authorization against the node's own roles. That matters more here than anywhere else in the suite: a tunnelled command that reached an unguarded path on a door controller could open a door, and the node — not the parent — is the thing that must decide who may do that.
- Registers `notificationService.Register(fleetnode.NewControlEventSink(control))` — every notification this node raises (a forced door, a duress alarm, a badge decision, a sign-in lockout) also flows up the channel into the control plane's unified feed. The badge **decisions** (`services/alarm.go.md`'s `NotificationAlarmer.Decision`) are the reason the fifth app joins the fleet: once `myseliasan` holds events from camera nodes, sensor nodes AND door nodes, the flagship correlation rule becomes real — motion on a camera AND a door contact opening AND no badge accepted. Neither node can see that on its own.
- Calls `control.SetMetrics(deps.Metrics)` (W2-6, F-11) — `ForwardEvent`'s two silent-loss paths (channel down / write failed) are now counted as `kopiv2_control_events_dropped_total{kind,reason}`, with successful forwards counted too (`kopiv2_control_events_forwarded_total{kind}`), and the running drop count rides upstream on the node's next control-channel hello (`control.Frame.Dropped`). It matters most here: a door node's events are badge decisions and duress alarms. See `docs/modules/domain/shared/fleetnode/doc.go.md`'s `control_channel.go` row.

## `fleet.start`

Runs the discovery responder (answers authenticated probes while unpaired and a fleet key is
set, then goes silent once adopted — a node that keeps announcing itself after adoption is
advertising itself to whoever else is listening) and both channels, each
`safego.Supervise`d so a panic in either doesn't silently stop enrollment or the control
connection with nothing logged to say why.

## `appVersion` / `boolValue` / `openFleetSecretCipher`

- `appVersion(m)` resolves this build's version for the fleet handshake via
  `versioning.LoadDefault()` / `InfoForApp(m.Name())`; `mypintusan` has no entry in the version
  manifest's `apps` map yet (its changes ship under the `infra` core scope — see
  `docs/modules/infra/versioning/bump.go.md`), so this reads `"0.0.0"` until one is added —
  cosmetic in the fleet UI, and the honest value until the app cuts releases.
- `boolValue(v, fallback)` dereferences an optional `*bool` config flag, same helper shape as the
  other appliances' fleet wiring.
- `openFleetSecretCipher(deps)` resolves the at-rest key protecting the node's fleet secrets
  (fleet key, pairing token, mTLS private key) — identical fail-closed contract to
  `mymatasan`/`myiotsan`: a key that existed before and is now missing refuses to boot rather
  than silently minting a new one and orphaning the node's enrollment.

## Notes

- Everything here is **node-dialed**. The control plane never needs a route back to the node — `mypintusan` reaches out, which is what lets it sit behind NAT on a building's LAN with no inbound firewall rule, and is also why adoption is a deliberate act rather than something a network scanner can do to you.
- `buildFleet` only constructs the managers; `RegisterAppRoutes` in `app.go` calls `f.start` after registering the pairing routes, gated on `deps.Config.Pairing.Enabled` — see `docs/modules/apps/mypintusan/app/app.go.md`'s "Fleet adoption" section.
- Live-verified end-to-end on one machine: UDP discovery (kind hint `"door"` in the unsigned announce), claim-code adopt (`fleetnode.KindDoor` authoritative in the signed adopt reply), mTLS enrollment + cert issuance, the control channel, the embedded management UI over the tunnel (`docs/modules/apps/myseliasan/services/node_registry.go.md` / the `myseliasan` frontend's `nodedoor` embed), a badge decision landing in the parent's unified feed tagged `node:<id>`, a fleet rule with door-kind clauses arming→grace→firing Critical, and replay-on-reconnect (5 events missed while the control channel was down, zero duplicates on reconnect).
