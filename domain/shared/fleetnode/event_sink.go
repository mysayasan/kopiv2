package fleetnode

import (
	"context"
	"encoding/json"

	"github.com/mysayasan/kopiv2/domain/notification"
)

// controlEventForwarder is the slice of the control channel the sink needs: push a
// node event upstream (a no-op when the channel is not connected).
type controlEventForwarder interface {
	ForwardEvent(kind string, body []byte)
}

// controlEventSink is a notification.Channel that forwards every node notification
// up the control channel as an event frame, so the control plane's unified feed
// receives node alerts / health / system events live — in addition to the node's
// own local delivery (store, SSE, webhook, etc.). The notification's binary
// Attachment is json:"-" and so never crosses the channel; only metadata does.
type controlEventSink struct {
	fwd controlEventForwarder
}

// NewControlEventSink builds the forwarding channel. Register it on the node's
// notification service so published notifications also flow to the control plane.
func NewControlEventSink(fwd controlEventForwarder) notification.Channel {
	return &controlEventSink{fwd: fwd}
}

func (c *controlEventSink) Name() string { return "control-channel" }

func (c *controlEventSink) Send(_ context.Context, n notification.Notification) error {
	body, err := json.Marshal(n)
	if err != nil {
		return err
	}
	c.fwd.ForwardEvent("notification", body)
	return nil
}
