package services

import (
	"sync"
	"testing"

	"github.com/mysayasan/kopiv2/infra/control"
)

// A stale connection's teardown must NOT be announced as a disconnect.
//
// A node that reconnects — to this same instance — replaces the entry in the connection
// map, and the old goroutine's deferred teardown then arrives afterwards. Announcing that
// unconditionally withdrew the ownership claim of the LIVE connection, after which nothing
// held the node at all. Because the heartbeat now reads presence from the owner registry,
// the consequence was a perfectly healthy node being marked lost and alerted on — on a
// single instance exactly as much as in a cluster.
func TestStaleConnectionTeardownIsNotAnnounced(t *testing.T) {
	cs := &ControlServer{
		conns:    map[string]*control.Conn{},
		pending:  map[string]pendingReq{},
		rejected: map[string]*RejectedNode{},
		logf:     func(string, ...any) {},
	}

	var mu sync.Mutex
	disconnects := 0
	cs.SetOnDisconnect(func(string) {
		mu.Lock()
		disconnects++
		mu.Unlock()
	})

	// Two distinct connection objects for one node: the original, then its replacement.
	first := &control.Conn{}
	second := &control.Conn{}

	cs.add("node-1", first)
	cs.add("node-1", second) // the node reconnected; `second` is now current

	// The OLD connection's teardown lands after the reconnect.
	if wasCurrent := cs.remove("node-1", first); wasCurrent {
		t.Fatal("removing a superseded connection must report that it was not current")
	}
	// The current one is untouched by that.
	if !cs.IsConnected("node-1") {
		t.Fatal("the live connection was dropped by the stale teardown")
	}

	// And the real teardown does report current.
	if wasCurrent := cs.remove("node-1", second); !wasCurrent {
		t.Fatal("removing the current connection must report that it was current")
	}
	if cs.IsConnected("node-1") {
		t.Fatal("the node should be gone after its current connection closed")
	}
}

// The counterpart: a genuine disconnect must still be announced, or ownership is never
// released and another instance waits out the whole lease to take the node over.
func TestCurrentConnectionTeardownIsAnnounced(t *testing.T) {
	cs := &ControlServer{
		conns:    map[string]*control.Conn{},
		pending:  map[string]pendingReq{},
		rejected: map[string]*RejectedNode{},
		logf:     func(string, ...any) {},
	}
	conn := &control.Conn{}
	cs.add("node-1", conn)

	if wasCurrent := cs.remove("node-1", conn); !wasCurrent {
		t.Fatal("the only connection must report as current when removed")
	}
}
