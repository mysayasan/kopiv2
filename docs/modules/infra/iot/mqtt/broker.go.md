# Module: infra/iot/mqtt/broker.go

## Purpose

Embeds an MQTT broker (`mochi-mqtt/server/v2`, a new dependency) in the myiotsan process.
Embedded, not depended upon: requiring the operator to install and run Mosquitto or EMQX
alongside would break the single-binary, air-gapped promise that IS the product — an appliance
you drop on an intranet and it works. A site that already runs a broker can point devices at it
instead (a future "connect" mode); nobody is forced to run this one.

## Key Type: Principal

```go
type Principal struct {
    DeviceId  int64 // the adopted device, or 0 for an enrolling client
    Enrolling bool  // marks a QUARANTINED session
}
```

Added in P3, replacing the bare `int64` device id the authenticator used to return. `Enrolling`
marks a session admitted through an open enrollment window rather than as a known device — see
`apps/myiotsan/services/enrollment.go.md`. A quarantined session may publish, and what it
publishes is recorded as a candidate and nowhere else: no telemetry is stored, no rule is
evaluated. That is the property that makes the enrollment hole safe to open at all: somebody who
gets into the window can create junk candidates for an admin to decline, but cannot forge a
reading, move a chart, or trigger or suppress an alert.

## Key Type: Authenticator

```go
type Authenticator interface {
    AuthenticateDevice(ctx context.Context, clientId, password string) (Principal, bool)
    AuthorizeTopic(ctx context.Context, p Principal, clientId, topic string, write bool) bool
}
```

The seam that makes a device's **inventory record its credential record**. A device not in
`iot_device` cannot connect at all — there is no separate credential store to drift out of sync
with the device list, and deleting a device really does revoke it (`services.DeviceService`
implements this; see `apps/myiotsan/services/device.go.md`). The one exception is an open
enrollment window, which is deliberate, time-boxed, and quarantined (see `Principal` above).
`AuthorizeTopic` confines an authenticated client to topics containing its own key, so one
compromised sensor cannot publish readings on behalf of every other sensor in the building; it
applies to an enrolling client exactly as it does to an adopted one, so a device announcing
itself cannot pollute another device's stream even as a candidate.

## Key Type: Broker

```go
func New(opts Options) (*Broker, error)
func (b *Broker) Run(ctx context.Context) error
func (b *Broker) Publish(topic string, payload []byte, retain bool, qos byte) error
```

- `Options.Auth` is **required** — `New` refuses to build with a nil authenticator rather than
  silently defaulting to allow-all; a broker with no authenticator would accept anything on the
  network.
- `Run` adds a plaintext TCP listener at `Options.Addr` (empty disables the listener entirely)
  and blocks until `ctx` is cancelled, then closes the mochi server.
- `Publish` is the broker-originated send path — the actuation path (P4) and any device-facing
  command will use it; unused in P1/P2.
- Internally tracks `clientId -> Principal` for authenticated sessions (`clients map`, mutex
  guarded) so the ACL check and the message handler can resolve who a given connection
  authenticated as without re-authenticating per message.

## Key Type: hook

Wires the broker's lifecycle into the authenticator and the ingest path via mochi's hook
interface:

- `OnConnectAuthenticate` — the gate. Resolves the client id from `cl.Properties.Username`
  (falling back to mochi's internal `cl.ID`), calls `Auth.AuthenticateDevice`; a client whose id
  is not a known device (and not admitted via an open enrollment window), or whose password does
  not verify, never gets a session. An enrolling admission is logged distinctly ("admitted for
  ENROLLMENT (quarantined: ...)") so the fact is visible in the broker log, not just the app's
  notification feed.
- `OnACLCheck` — resolves the bound `Principal` and delegates to `Auth.AuthorizeTopic`.
- `OnPublished` — hands the accepted payload and its `Principal` to `Options.OnMessage`
  (`MessageHandler`), which is `services.Ingest.Handle` in production
  (`apps/myiotsan/services/ingest.go.md`) — `Handle` is what turns `p.Enrolling` into the
  quarantine early-return.
- `OnDisconnect` — unbinds the client id from its `Principal`.

## Notes

- `mochi.New(&mochi.Options{InlineClient: true})` — the inline client flag lets the broker
  itself publish/subscribe (used by `Broker.Publish`).
- Wired in `apps/myiotsan/app/app.go`'s `RegisterAppRoutes`: `iotmqtt.New` with `Auth:
  deviceService`, `OnMessage: ingest.Handle`, then `broker.Run(bgCtx)` under `safego.Go` so a
  broker panic is recovered and logged rather than taking the process down.
- `Options.Addr` comes from `apps/myiotsan/config`'s `mqtt.addr` (default `0.0.0.0:1883`,
  `mqtt.enabled` toggles whether the app builds the broker at all in a future connect-mode).
- go.mod gained `github.com/mochi-mqtt/server/v2` and its transitive
  `github.com/rs/xid` dependency for this.
