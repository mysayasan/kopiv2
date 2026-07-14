# Module: infra/iot/mqtt/broker.go

## Purpose

Embeds an MQTT broker (`mochi-mqtt/server/v2`, a new dependency) in the myiotsan process.
Embedded, not depended upon: requiring the operator to install and run Mosquitto or EMQX
alongside would break the single-binary, air-gapped promise that IS the product — an appliance
you drop on an intranet and it works. A site that already runs a broker can point devices at it
instead (a future "connect" mode); nobody is forced to run this one.

## Key Type: Authenticator

```go
type Authenticator interface {
    AuthenticateDevice(ctx context.Context, clientId, password string) (int64, bool)
    AuthorizeTopic(ctx context.Context, deviceId int64, clientId, topic string, write bool) bool
}
```

The seam that makes a device's **inventory record its credential record**. A device not in
`iot_device` cannot connect at all — there is no separate credential store to drift out of sync
with the device list, and deleting a device really does revoke it (`services.DeviceService`
implements this; see `apps/myiotsan/services/device.go.md`). `AuthorizeTopic` confines an
authenticated device to topics containing its own key, so one compromised sensor cannot publish
readings on behalf of every other sensor in the building.

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
- Internally tracks `clientId -> deviceId` for authenticated sessions (`devices map`, mutex
  guarded) so the ACL check and the message handler can resolve which device a given connection
  belongs to without re-authenticating per message.

## Key Type: hook

Wires the broker's lifecycle into the authenticator and the ingest path via mochi's hook
interface:

- `OnConnectAuthenticate` — the gate. Resolves the client id from `cl.Properties.Username`
  (falling back to mochi's internal `cl.ID`), calls `Auth.AuthenticateDevice`; a client whose id
  is not a known device, or whose password does not verify, never gets a session.
- `OnACLCheck` — resolves the bound device id and delegates to `Auth.AuthorizeTopic`.
- `OnPublished` — hands the accepted payload to `Options.OnMessage` (`MessageHandler`), which is
  `services.Ingest.Handle` in production (`apps/myiotsan/services/ingest.go.md`).
- `OnDisconnect` — unbinds the client id from its device.

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
