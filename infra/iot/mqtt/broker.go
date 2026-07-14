// Package mqtt embeds an MQTT broker in the process.
//
// It is embedded rather than depended upon. Requiring the operator to install and run
// Mosquitto or EMQX alongside would break the single-binary, air-gapped promise that IS the
// product — an appliance you drop on an intranet and it works. A site that already runs a
// broker can point the app at it instead (Connect mode), but nobody is forced to.
package mqtt

import (
	"context"
	"fmt"
	"strings"
	"sync"

	mochi "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/listeners"
	"github.com/mochi-mqtt/server/v2/packets"
)

// Principal is who a connected client turned out to be.
type Principal struct {
	// DeviceId is the adopted device, or 0 for an enrolling client.
	DeviceId int64
	// Enrolling marks a QUARANTINED session: an unknown client admitted during an enrollment
	// window because it presented the window's key.
	//
	// A quarantined session may publish, and what it publishes is recorded as a CANDIDATE and
	// nowhere else — no telemetry is stored for it and no rule is evaluated against it. That is
	// the property that makes the enrollment hole safe to open: somebody who gets into the
	// window can create junk candidates for an admin to decline, but cannot forge a reading,
	// move a chart, or trigger or suppress an alert.
	Enrolling bool
}

// Authenticator decides whether a connecting client may talk to the broker.
//
// This is the seam that makes a device's INVENTORY RECORD its credential record. A device that
// is not in iot_device cannot connect — there is no separate credential store to drift out of
// sync with the device list, and deleting a device really does revoke it. The one exception is
// an enrollment window, which is deliberate, time-boxed, and quarantined (see Principal).
type Authenticator interface {
	// AuthenticateDevice verifies a client id and password, returning who the client is.
	// ok=false refuses the connection.
	AuthenticateDevice(ctx context.Context, clientId, password string) (Principal, bool)
	// AuthorizeTopic reports whether an authenticated client may publish to (or subscribe to) a
	// topic. A device may only ever touch its OWN topics: one compromised sensor must not be
	// able to publish readings on behalf of every other sensor in the building, which is exactly
	// what a broker with no ACL would allow.
	AuthorizeTopic(ctx context.Context, p Principal, clientId, topic string, write bool) bool
}

// MessageHandler receives every accepted publish.
type MessageHandler func(ctx context.Context, p Principal, clientId, topic string, payload []byte)

// Options configures the broker.
type Options struct {
	// Addr is the plaintext listener ("0.0.0.0:1883"). Empty disables it.
	Addr string
	// Auth verifies devices. REQUIRED: a broker with no authenticator would accept anything on
	// the network, so a nil one is refused rather than silently defaulting to allow-all.
	Auth Authenticator
	// OnMessage is called for each accepted publish.
	OnMessage MessageHandler
	// Logf is the app's logger.
	Logf func(format string, args ...any)
}

// Broker is the embedded MQTT server.
type Broker struct {
	server *mochi.Server
	opts   Options

	mu      sync.RWMutex
	clients map[string]Principal // mqtt client id -> who it authenticated as
}

// New builds the broker. It does not listen until Run.
func New(opts Options) (*Broker, error) {
	if opts.Auth == nil {
		return nil, fmt.Errorf("mqtt: an authenticator is required — refusing to run an open broker")
	}
	if opts.Logf == nil {
		opts.Logf = func(string, ...any) {}
	}
	b := &Broker{
		server:  mochi.New(&mochi.Options{InlineClient: true}),
		opts:    opts,
		clients: map[string]Principal{},
	}
	if err := b.server.AddHook(&hook{broker: b}, nil); err != nil {
		return nil, fmt.Errorf("mqtt: install auth hook: %w", err)
	}
	return b, nil
}

// Run starts the listener and blocks until ctx is cancelled.
func (b *Broker) Run(ctx context.Context) error {
	addr := strings.TrimSpace(b.opts.Addr)
	if addr == "" {
		return nil
	}
	tcp := listeners.NewTCP(listeners.Config{ID: "tcp", Address: addr})
	if err := b.server.AddListener(tcp); err != nil {
		return fmt.Errorf("mqtt: listen on %s: %w", addr, err)
	}
	if err := b.server.Serve(); err != nil {
		return fmt.Errorf("mqtt: serve: %w", err)
	}
	b.opts.Logf("mqtt broker listening on %s", addr)

	<-ctx.Done()
	return b.server.Close()
}

// Publish sends a message from the broker itself — the actuation path (P4) and any
// device-facing command.
func (b *Broker) Publish(topic string, payload []byte, retain bool, qos byte) error {
	return b.server.Publish(topic, payload, retain, qos)
}

// principalFor resolves an authenticated session.
func (b *Broker) principalFor(clientId string) (Principal, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	p, ok := b.clients[clientId]
	return p, ok
}

func (b *Broker) bind(clientId string, p Principal) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.clients[clientId] = p
}

func (b *Broker) unbind(clientId string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.clients, clientId)
}

// hook wires the broker's lifecycle into the app's authenticator and ingest path.
type hook struct {
	mochi.HookBase
	broker *Broker
}

func (h *hook) ID() string { return "myiotsan-auth" }

func (h *hook) Provides(bt byte) bool {
	switch bt {
	case mochi.OnConnectAuthenticate, mochi.OnACLCheck, mochi.OnPublished, mochi.OnDisconnect:
		return true
	}
	return false
}

// OnConnectAuthenticate is the gate. A client whose id is not a known device, or whose
// password does not verify, never gets a session.
func (h *hook) OnConnectAuthenticate(cl *mochi.Client, pk packets.Packet) bool {
	clientId := string(cl.Properties.Username)
	if clientId == "" {
		clientId = cl.ID
	}
	p, ok := h.broker.opts.Auth.AuthenticateDevice(context.Background(), clientId, string(pk.Connect.Password))
	if !ok {
		h.broker.opts.Logf("mqtt: refused connection from %q", clientId)
		return false
	}
	if p.Enrolling {
		h.broker.opts.Logf("mqtt: %q admitted for ENROLLMENT (quarantined: no telemetry stored, no rules evaluated)", clientId)
	}
	h.broker.bind(cl.ID, p)
	return true
}

// OnACLCheck confines a device to its own topics.
func (h *hook) OnACLCheck(cl *mochi.Client, topic string, write bool) bool {
	p, ok := h.broker.principalFor(cl.ID)
	if !ok {
		return false
	}
	clientId := string(cl.Properties.Username)
	if clientId == "" {
		clientId = cl.ID
	}
	return h.broker.opts.Auth.AuthorizeTopic(context.Background(), p, clientId, topic, write)
}

// OnPublished hands an accepted payload to the ingest pipeline.
func (h *hook) OnPublished(cl *mochi.Client, pk packets.Packet) {
	if h.broker.opts.OnMessage == nil {
		return
	}
	p, ok := h.broker.principalFor(cl.ID)
	if !ok {
		return
	}
	clientId := string(cl.Properties.Username)
	if clientId == "" {
		clientId = cl.ID
	}
	h.broker.opts.OnMessage(context.Background(), p, clientId, pk.TopicName, pk.Payload)
}

func (h *hook) OnDisconnect(cl *mochi.Client, err error, expire bool) {
	h.broker.unbind(cl.ID)
}
