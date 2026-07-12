package services

import (
	"context"
	"errors"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/mysayasan/kopiv2/apps/myseliasan/entities"
	"github.com/mysayasan/kopiv2/infra/control"
	"github.com/mysayasan/kopiv2/infra/fleetca"
)

// TestControlServerSendRequestRoundTrip stands up a real ControlServer, dials it
// from a node holding a fleet-CA cert, and verifies a tunneled request reaches the
// node and its response is correlated back to SendRequest.
func TestControlServerSendRequestRoundTrip(t *testing.T) {
	reg, nodes := newTestRegistry()
	ctx := context.Background()

	const nodeID = "node-e2e"
	nodes.rows = append(nodes.rows, &entities.ManagedNode{Id: 1, NodeId: nodeID, Token: "tok", Status: "online"})
	nodeKey, csr, err := fleetca.GenerateKeyAndCSR(nodeID)
	if err != nil {
		t.Fatalf("node key/csr: %v", err)
	}
	nodeCert, caRoot, err := reg.Enroll(ctx, nodeID, "tok", csr)
	if err != nil {
		t.Fatalf("enroll: %v", err)
	}
	clientTLS, err := fleetca.ClientTLSConfig(nodeCert, nodeKey, caRoot, "parent-1")
	if err != nil {
		t.Fatalf("client tls: %v", err)
	}

	port := freePort(t)
	cs := NewControlServer(reg, port, nil, nil)
	srvCtx, cancelSrv := context.WithCancel(ctx)
	defer cancelSrv()
	go cs.Run(srvCtx)
	waitTCP(t, port)

	// Node side: dial and echo every request back as a 200 with the path in the body.
	wsURL := wsURLForPort(port)
	dialCtx, cancelDial := context.WithTimeout(ctx, 5*time.Second)
	conn, err := control.Dial(dialCtx, wsURL, clientTLS)
	cancelDial()
	if err != nil {
		t.Fatalf("node dial: %v", err)
	}
	defer conn.Close()
	go func() {
		for {
			f, err := conn.ReadFrame()
			if err != nil {
				return
			}
			if f.Type == control.FrameReq {
				resp := control.Response{Status: 200, Body: []byte("echo:" + f.Path + ":role=" + f.Role)}
				_ = conn.WriteFrame(resp.ToFrame(f.ID))
			}
		}
	}()

	// Retry until the server has registered the connection (dial/registration race).
	var resp control.Response
	deadline := time.Now().Add(3 * time.Second)
	for {
		resp, err = cs.SendRequest(ctx, nodeID, control.Request{Method: "GET", Path: "/api/widgets", Role: "admin"})
		if err == nil || !errors.Is(err, ErrNodeOffline) || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("SendRequest: %v", err)
	}
	if resp.Status != 200 || string(resp.Body) != "echo:/api/widgets:role=admin" {
		t.Fatalf("unexpected response: %d %q", resp.Status, resp.Body)
	}
}

// TestControlServerForwardsEvents verifies a node-pushed event frame reaches the
// parent's event handler with the node identity (from the client cert) attached.
func TestControlServerForwardsEvents(t *testing.T) {
	reg, nodes := newTestRegistry()
	ctx := context.Background()

	const nodeID = "node-evt"
	nodes.rows = append(nodes.rows, &entities.ManagedNode{Id: 1, NodeId: nodeID, Token: "tok", Status: "online"})
	nodeKey, csr, err := fleetca.GenerateKeyAndCSR(nodeID)
	if err != nil {
		t.Fatalf("node key/csr: %v", err)
	}
	nodeCert, caRoot, err := reg.Enroll(ctx, nodeID, "tok", csr)
	if err != nil {
		t.Fatalf("enroll: %v", err)
	}
	clientTLS, err := fleetca.ClientTLSConfig(nodeCert, nodeKey, caRoot, "parent-1")
	if err != nil {
		t.Fatalf("client tls: %v", err)
	}

	events := make(chan string, 4)
	onEvent := func(id, kind string, body []byte) { events <- id + "|" + kind + "|" + string(body) }

	port := freePort(t)
	cs := NewControlServer(reg, port, onEvent, nil)
	srvCtx, cancelSrv := context.WithCancel(ctx)
	defer cancelSrv()
	go cs.Run(srvCtx)
	waitTCP(t, port)

	dialCtx, cancelDial := context.WithTimeout(ctx, 5*time.Second)
	conn, err := control.Dial(dialCtx, wsURLForPort(port), clientTLS)
	cancelDial()
	if err != nil {
		t.Fatalf("node dial: %v", err)
	}
	defer conn.Close()

	if err := conn.WriteFrame(&control.Frame{Type: control.FrameEvent, Kind: "notification", Body: []byte(`{"title":"x"}`)}); err != nil {
		t.Fatalf("write event: %v", err)
	}

	select {
	case got := <-events:
		if want := nodeID + "|notification|" + `{"title":"x"}`; got != want {
			t.Fatalf("event = %q, want %q", got, want)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for forwarded event")
	}
}

// TestControlServerSendRequestFailsFastOnDisconnect verifies that when a node's
// control channel drops while a tunneled request is in flight, SendRequest returns
// ErrNodeDisconnected promptly instead of hanging until controlRequestTimeout (30s).
func TestControlServerSendRequestFailsFastOnDisconnect(t *testing.T) {
	reg, nodes := newTestRegistry()
	ctx := context.Background()

	const nodeID = "node-drop"
	nodes.rows = append(nodes.rows, &entities.ManagedNode{Id: 1, NodeId: nodeID, Token: "tok", Status: "online"})
	nodeKey, csr, err := fleetca.GenerateKeyAndCSR(nodeID)
	if err != nil {
		t.Fatalf("node key/csr: %v", err)
	}
	nodeCert, caRoot, err := reg.Enroll(ctx, nodeID, "tok", csr)
	if err != nil {
		t.Fatalf("enroll: %v", err)
	}
	clientTLS, err := fleetca.ClientTLSConfig(nodeCert, nodeKey, caRoot, "parent-1")
	if err != nil {
		t.Fatalf("client tls: %v", err)
	}

	port := freePort(t)
	cs := NewControlServer(reg, port, nil, nil)
	srvCtx, cancelSrv := context.WithCancel(ctx)
	defer cancelSrv()
	go cs.Run(srvCtx)
	waitTCP(t, port)

	dialCtx, cancelDial := context.WithTimeout(ctx, 5*time.Second)
	conn, err := control.Dial(dialCtx, wsURLForPort(port), clientTLS)
	cancelDial()
	if err != nil {
		t.Fatalf("node dial: %v", err)
	}
	// Node reads frames but NEVER answers a request — so SendRequest can only return via
	// the disconnect signal, not a real response.
	go func() {
		for {
			if _, rerr := conn.ReadFrame(); rerr != nil {
				return
			}
		}
	}()

	// Wait for the server to register the connection.
	for i := 0; i < 150 && !cs.IsConnected(nodeID); i++ {
		time.Sleep(20 * time.Millisecond)
	}
	if !cs.IsConnected(nodeID) {
		t.Fatal("node never registered on the control server")
	}

	done := make(chan error, 1)
	go func() {
		_, e := cs.SendRequest(ctx, nodeID, control.Request{Method: "POST", Path: "/api/wipe", Role: "admin"})
		done <- e
	}()

	// Let SendRequest register its pending waiter + write the frame, then drop the node.
	time.Sleep(200 * time.Millisecond)
	_ = conn.Close()

	select {
	case e := <-done:
		if !errors.Is(e, ErrNodeDisconnected) {
			t.Fatalf("got %v, want ErrNodeDisconnected", e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("SendRequest hung after disconnect (expected fast ErrNodeDisconnected)")
	}
}

func TestControlServerSendRequestOffline(t *testing.T) {
	reg, _ := newTestRegistry()
	cs := NewControlServer(reg, 0, nil, nil)
	if _, err := cs.SendRequest(context.Background(), "ghost", control.Request{Method: "GET", Path: "/x"}); !errors.Is(err, ErrNodeOffline) {
		t.Fatalf("offline node: got %v want ErrNodeOffline", err)
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return port
}

func wsURLForPort(port int) string {
	return "wss://127.0.0.1:" + strconv.Itoa(port) + "/control"
}

func waitTCP(t *testing.T, port int) {
	t.Helper()
	addr := "127.0.0.1:" + strconv.Itoa(port)
	for i := 0; i < 100; i++ {
		c, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			_ = c.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("control server never came up on %s", addr)
}
