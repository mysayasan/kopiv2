package app

import (
	"context"
	"encoding/json"
	"time"

	"github.com/mysayasan/kopiv2/apps/myseliasan/services"
	"github.com/mysayasan/kopiv2/domain/notification"
)

// observeForCorrelation flattens one node event into what a fleet rule can match on, and hands
// it to the correlator.
//
// It reads the NODE's event, not the control plane's re-published copy of it. That is not an
// accident: correlating on our own output would let one fleet rule's alert satisfy another fleet
// rule's clause, and two rules could then trigger each other indefinitely. Events come from
// nodes; conclusions come from here; the two must not be mixed.
func observeForCorrelation(ctx context.Context, c *services.Correlator, nodeID, kind string, body []byte) {
	if c == nil {
		return
	}
	e := services.NodeEvent{NodeId: nodeID, At: time.Now()}

	var n notification.Notification
	if err := json.Unmarshal(body, &n); err == nil && (n.Title != "" || n.Body != "") {
		e.Category = n.Category
		e.Title = n.Title
		e.Body = n.Body
	} else {
		// A frame we cannot parse as a notification still says SOMETHING happened on that node.
		// Pass it through with the raw kind rather than dropping it: a rule that matches on a
		// node going offline should still work.
		e.Category = kind
		e.Title = kind
	}
	// Kind is left empty here on purpose — the correlator resolves it from the adopted node's
	// record, which is the authoritative answer to "is this a camera or a sensor hub?". Trusting
	// a value carried in the event body would let a node claim to be something it is not.
	c.Observe(ctx, e)
}
