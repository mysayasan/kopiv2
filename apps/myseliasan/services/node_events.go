package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/mysayasan/kopiv2/domain/notification"
	"github.com/mysayasan/kopiv2/infra/eventbus"
)

// Sharing node events between instances.
//
// A node holds its control channel to exactly one instance, so only that instance sees
// what the node reports. Two things break on that, and both are silent:
//
//   - The live notification bell only shows events that happened to arrive at the instance
//     a browser is connected to. Nothing looks broken; the feed is simply thinner than the
//     truth, and a refresh (which reads history from the database) quietly disagrees with
//     what was on screen a moment earlier.
//   - A fleet rule whose conditions span nodes attached to DIFFERENT instances never sees
//     them together, so it never arms and never fires. It goes quiet rather than firing
//     twice, which is the more dangerous direction: nobody notices an alert that does not
//     happen.
//
// Publishing every accepted event onto a shared bus fixes both, because each instance can
// then feed its own live stream and its own copy of the correlator.

// NodeEventTopic is the bus topic node events are published on.
const NodeEventTopic = "node-events"

// NodeEventMessage is what crosses the bus.
type NodeEventMessage struct {
	// Origin identifies the publishing instance so it can ignore the echo of its own
	// message. Without it every event would be handled twice at its origin.
	Origin string `json:"origin"`
	NodeID string `json:"nodeId"`
	Kind   string `json:"kind"`
	// Body is the node's raw event, carried so every instance's correlator sees exactly
	// what the origin's did. Deliberately the RAW event and not the control plane's own
	// re-published notification: correlating on our own output would let one fleet rule's
	// alert satisfy another rule's clause, and two rules could then trigger each other
	// forever.
	Body []byte `json:"body,omitempty"`
}

// NotificationTopic carries notifications for the LIVE FEED, separately from node events.
//
// Two topics rather than one because the two consumers want different things and from
// different sources. The correlator wants RAW NODE events and only those. The live feed
// wants every notification this control plane publishes — a node's, yes, but equally a
// node-lost alert the heartbeat raised, an anomaly, or the morning digest, none of which
// arrive on a control channel at all. Folding the feed into the node-event message would
// have quietly left every locally-generated notification invisible on the other instances.
const NotificationTopic = "notifications"

// NotificationMessage is a notification relayed for other instances' live feeds.
type NotificationMessage struct {
	Origin string `json:"origin"`
	// N is the row exactly as persisted, so every instance's feed shows the same entry
	// with the same id rather than a near-copy.
	N notification.Notification `json:"n"`
}

// NewInstanceID derives a stable-per-process publisher identity.
//
// It prefers the configured advertise URL, which is already unique per instance and makes
// a captured bus message legible to a human debugging one. Where that is unset (the
// single-instance default) a random value is used: identity only has to be unique, and a
// lone instance has nobody to be confused with.
func NewInstanceID(advertiseURL string) string {
	if u := strings.TrimSpace(advertiseURL); u != "" {
		return u
	}
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "instance"
	}
	return "instance-" + hex.EncodeToString(buf)
}

// PublishNodeEvent puts one node event on the bus for the other instances' correlators.
//
// A publish failure is logged and swallowed. The event has already been persisted and
// shown locally; failing the ingest path because a peer could not be told would trade a
// degraded feed for a lost event.
func PublishNodeEvent(
	ctx context.Context,
	bus eventbus.Bus,
	instanceID, nodeID, kind string,
	body []byte,
	logf func(string, ...any),
) {
	// Nothing to gain from a round trip when nobody else can be listening.
	if bus == nil || !bus.Distributed() {
		return
	}
	msg := NodeEventMessage{Origin: instanceID, NodeID: nodeID, Kind: kind, Body: body}
	payload, err := json.Marshal(msg)
	if err != nil {
		if logf != nil {
			logf("could not encode node event for the bus: %v", err)
		}
		return
	}
	if err := bus.Publish(ctx, NodeEventTopic, payload); err != nil && logf != nil {
		logf("could not publish node event to the bus: %v", err)
	}
}

// RulesChangedTopic signals that the fleet-rule set was edited.
//
// The correlator holds its rules in memory and reloads them after a save or delete — but
// only on the instance that served the request. Behind a load balancer an operator's edit
// lands on an arbitrary instance while the LEADER is the one that actually fires rules, so
// a new rule could sit unused, and a disabled one keep firing, until something restarted.
// Nothing about that looks wrong from the screen where the edit was made.
const RulesChangedTopic = "fleet-rules-changed"

type rulesChangedMessage struct {
	Origin string `json:"origin"`
}

// PublishRulesChanged tells the other instances to reload their rule cache.
func PublishRulesChanged(ctx context.Context, bus eventbus.Bus, instanceID string, logf func(string, ...any)) {
	if bus == nil || !bus.Distributed() {
		return
	}
	payload, err := json.Marshal(rulesChangedMessage{Origin: instanceID})
	if err != nil {
		return
	}
	if err := bus.Publish(ctx, RulesChangedTopic, payload); err != nil && logf != nil {
		logf("could not announce a fleet-rule change: %v", err)
	}
}

// SubscribeRulesChanged reloads this instance's rule cache when another instance edits one.
func SubscribeRulesChanged(ctx context.Context, bus eventbus.Bus, instanceID string, reload func(), logf func(string, ...any)) {
	if bus == nil || reload == nil || !bus.Distributed() {
		return
	}
	err := bus.Subscribe(ctx, RulesChangedTopic, func(payload []byte) {
		var msg rulesChangedMessage
		if err := json.Unmarshal(payload, &msg); err != nil {
			return
		}
		if msg.Origin == instanceID {
			return // this instance already reloaded inline
		}
		reload()
	})
	if err != nil && logf != nil {
		logf("could not subscribe to fleet-rule changes: %v", err)
	}
}

// NotificationRelayChannel publishes every notification THIS instance emits onto the bus,
// so the other instances can show it on their live feeds.
//
// It is a notification Channel rather than a call bolted onto the node-event path because
// the hub already invokes every registered channel on every publish — which is exactly the
// set that should be relayed. Wiring it anywhere narrower would have relayed a node's
// events and silently dropped everything the control plane raises by itself: node-lost
// alerts, certificate-expiry warnings, anomalies, the daily digest.
//
// There is no relay loop. A notification arriving FROM the bus is handed to the SSE stream
// directly (Service.RelayToStream), which does not go through the hub, so it can never come
// back out of this channel.
type NotificationRelayChannel struct {
	bus        eventbus.Bus
	instanceID string
	logf       func(string, ...any)
}

// NewNotificationRelayChannel builds the relay. Register it on the notification service.
func NewNotificationRelayChannel(bus eventbus.Bus, instanceID string, logf func(string, ...any)) *NotificationRelayChannel {
	return &NotificationRelayChannel{bus: bus, instanceID: instanceID, logf: logf}
}

func (c *NotificationRelayChannel) Name() string { return "cluster-relay" }

// Send publishes n to the other instances. Errors are returned for the hub to log, never
// escalated: a peer that cannot be told is a thinner live feed elsewhere, not a reason to
// disturb the publisher.
func (c *NotificationRelayChannel) Send(ctx context.Context, n notification.Notification) error {
	if c == nil || c.bus == nil || !c.bus.Distributed() {
		return nil
	}
	payload, err := json.Marshal(NotificationMessage{Origin: c.instanceID, N: n})
	if err != nil {
		return err
	}
	return c.bus.Publish(ctx, NotificationTopic, payload)
}

// SubscribeNotifications delivers notifications published by OTHER instances to handle,
// for relaying onto this instance's live feed.
func SubscribeNotifications(
	ctx context.Context,
	bus eventbus.Bus,
	instanceID string,
	handle func(notification.Notification),
	logf func(string, ...any),
) {
	if bus == nil || handle == nil || !bus.Distributed() {
		return
	}
	err := bus.Subscribe(ctx, NotificationTopic, func(payload []byte) {
		var msg NotificationMessage
		if err := json.Unmarshal(payload, &msg); err != nil {
			if logf != nil {
				logf("could not decode a relayed notification: %v", err)
			}
			return
		}
		if msg.Origin == instanceID {
			return
		}
		handle(msg.N)
	})
	if err != nil && logf != nil {
		logf("could not subscribe to relayed notifications: %v", err)
	}
}

// SubscribeNodeEvents delivers events published by OTHER instances to handle.
//
// Messages from this instance are dropped: it already did this work inline, and handling
// the echo would double-publish to its own live stream and feed its correlator twice.
func SubscribeNodeEvents(
	ctx context.Context,
	bus eventbus.Bus,
	instanceID string,
	handle func(NodeEventMessage),
	logf func(string, ...any),
) {
	if bus == nil || handle == nil || !bus.Distributed() {
		return
	}
	err := bus.Subscribe(ctx, NodeEventTopic, func(payload []byte) {
		var msg NodeEventMessage
		if err := json.Unmarshal(payload, &msg); err != nil {
			if logf != nil {
				logf("could not decode a node event from the bus: %v", err)
			}
			return
		}
		if msg.Origin == instanceID {
			return
		}
		handle(msg)
	})
	if err != nil && logf != nil {
		logf("could not subscribe to node events: %v", err)
	}
}
