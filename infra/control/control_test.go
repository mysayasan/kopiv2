package control

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/mysayasan/kopiv2/infra/fleetca"
)

// TestControlChannelMTLSHelloRoundTrip stands up a real fleet CA, a parent control
// server, and a node dial, then verifies: the node's identity is taken from its
// verified client-cert CN, the parent's server cert is verified by CN, and a Hello
// frame crosses the channel intact.
func TestControlChannelMTLSHelloRoundTrip(t *testing.T) {
	const parentID = "parent-cp"
	const nodeID = "node-abc"

	ca, err := fleetca.NewCA("test-fleet-ca", time.Hour)
	if err != nil {
		t.Fatalf("new ca: %v", err)
	}
	// Parent server leaf (CN = parentID).
	parentCert, parentKey, err := ca.IssueLeaf(parentID, time.Hour, nil)
	if err != nil {
		t.Fatalf("issue parent leaf: %v", err)
	}
	// Node client leaf (CN = nodeID) from a node-generated CSR.
	nodeKey, nodeCSR, err := fleetca.GenerateKeyAndCSR(nodeID)
	if err != nil {
		t.Fatalf("node key/csr: %v", err)
	}
	nodeCert, err := ca.IssueFromCSR(nodeCSR, nodeID, time.Hour, nil)
	if err != nil {
		t.Fatalf("issue node leaf: %v", err)
	}

	serverTLS, err := fleetca.ServerTLSConfig(parentCert, parentKey, ca.CertPEM())
	if err != nil {
		t.Fatalf("server tls: %v", err)
	}
	clientTLS, err := fleetca.ClientTLSConfig(nodeCert, nodeKey, ca.CertPEM(), parentID)
	if err != nil {
		t.Fatalf("client tls: %v", err)
	}

	addr := freeAddr(t)
	gotHello := make(chan *Frame, 1)
	gotNodeID := make(chan string, 1)

	srv := NewServer(addr, serverTLS, func(id string, c *Conn) {
		gotNodeID <- id
		f, err := c.ReadFrame()
		if err != nil {
			t.Errorf("server read: %v", err)
			return
		}
		gotHello <- f
	}, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = srv.Run(ctx) }()
	waitDial(t, addr)

	conn, err := Dial(ctx, "wss://"+addr+"/control", clientTLS)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	if err := conn.WriteFrame(&Frame{Type: FrameHello, NodeID: nodeID, Version: "9.9.9"}); err != nil {
		t.Fatalf("write hello: %v", err)
	}

	select {
	case id := <-gotNodeID:
		if id != nodeID {
			t.Fatalf("server identified node as %q, want %q", id, nodeID)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for connection")
	}

	select {
	case f := <-gotHello:
		if f.Type != FrameHello || f.NodeID != nodeID || f.Version != "9.9.9" {
			t.Fatalf("unexpected hello frame: %+v", f)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for hello frame")
	}
}

// TestControlChannelRejectsWrongParentCN ensures the node refuses a server whose
// cert CN is not the expected parent id (a different parent / MITM).
func TestControlChannelRejectsWrongParentCN(t *testing.T) {
	ca, _ := fleetca.NewCA("test-fleet-ca", time.Hour)
	// Server presents CN = "imposter", node expects "real-parent".
	srvCert, srvKey, _ := ca.IssueLeaf("imposter", time.Hour, nil)
	nodeKey, nodeCSR, _ := fleetca.GenerateKeyAndCSR("node-x")
	nodeCert, _ := ca.IssueFromCSR(nodeCSR, "node-x", time.Hour, nil)

	serverTLS, _ := fleetca.ServerTLSConfig(srvCert, srvKey, ca.CertPEM())
	clientTLS, _ := fleetca.ClientTLSConfig(nodeCert, nodeKey, ca.CertPEM(), "real-parent")

	addr := freeAddr(t)
	srv := NewServer(addr, serverTLS, func(string, *Conn) {}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = srv.Run(ctx) }()
	waitDial(t, addr)

	if _, err := Dial(ctx, "wss://"+addr+"/control", clientTLS); err == nil {
		t.Fatal("expected dial to fail against wrong parent CN, but it succeeded")
	}
}

// freeAddr reserves a free localhost port and returns its host:port.
func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	return addr
}

// waitDial blocks until the server is accepting TLS connections on addr.
func waitDial(t *testing.T, addr string) {
	t.Helper()
	for i := 0; i < 100; i++ {
		c, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			_ = c.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("server never came up on %s", addr)
}
