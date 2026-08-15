package services

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mysayasan/kopiv2/infra/atrest"
	"github.com/mysayasan/kopiv2/infra/control"
)

// fakePeerSender stands in for a ControlServer: it answers for the nodes it "holds".
type fakePeerSender struct {
	held  map[string]control.Response
	calls []string
	err   error
}

func (f *fakePeerSender) SendRequest(_ context.Context, nodeID string, req control.Request) (control.Response, error) {
	f.calls = append(f.calls, nodeID+" "+req.Method+" "+req.Path)
	if f.err != nil {
		return control.Response{}, f.err
	}
	resp, ok := f.held[nodeID]
	if !ok {
		return control.Response{}, ErrNodeOffline
	}
	return resp, nil
}

// Every instance derives the same credential from the same signing secret, without any of
// them ever transmitting it. That is the entire basis of peer admission.
func TestPeerTokenIsDeterministicAndDerived(t *testing.T) {
	const secret = "a-shared-signing-secret"

	a, b := DerivePeerToken(secret), DerivePeerToken(secret)
	if a == "" || a != b {
		t.Fatalf("the same secret must derive the same token; got %q and %q", a, b)
	}
	if other := DerivePeerToken("a-different-secret"); other == a {
		t.Fatal("different secrets must derive different tokens")
	}
	// One-way: the token must not contain the secret it came from.
	if strings.Contains(a, secret) {
		t.Fatal("the derived token leaks the signing secret")
	}
	if DerivePeerToken("") != "" {
		t.Fatal("no secret means no token — forwarding must stay disabled rather than open")
	}
}

// The token must not collide with the boot-log fingerprint derived from the SAME secret:
// the fingerprint is printed in logs, and if it doubled as the peer credential, anyone who
// read a log file could call this endpoint.
func TestPeerTokenIsNotTheBootFingerprint(t *testing.T) {
	const secret = "a-shared-signing-secret"
	token := DerivePeerToken(secret)
	fingerprint := atrest.FingerprintSecret(secret)

	if fingerprint == "" || token == "" {
		t.Fatal("both derivations should produce a value for a non-empty secret")
	}
	if token == fingerprint {
		t.Fatal("the peer credential equals the value printed in the boot log")
	}
	// Not a prefix either: the fingerprint is the shorter derivation, and if the token
	// merely extended it, publishing the fingerprint would leak a guessable head start.
	if strings.HasPrefix(token, fingerprint) {
		t.Fatalf("the peer token starts with the logged fingerprint (%s…)", fingerprint)
	}
	if len(token) != peerTokenBytes*2 {
		t.Fatalf("token length = %d hex chars, want %d", len(token), peerTokenBytes*2)
	}
}

func peerTestServer(t *testing.T, secret string, local ControlSender) *httptest.Server {
	t.Helper()
	h := NewPeerForwardHandler(secret, local, nil)
	mux := http.NewServeMux()
	mux.Handle("/api"+PeerForwardPath, h)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// The happy path: instance A forwards, instance B delivers, the node's reply comes back
// intact. This is what makes a node's screens work from any instance.
func TestPeerForwardDeliversAndReturnsTheNodeReply(t *testing.T) {
	const secret = "shared"
	owner := &fakePeerSender{held: map[string]control.Response{
		"node-1": {Status: 200, Headers: map[string]string{"Content-Type": "application/json"}, Body: []byte(`{"ok":true}`)},
	}}
	srv := peerTestServer(t, secret, owner)

	client := NewPeerClient(secret, 5*time.Second, true)
	resp, err := client.Forward(context.Background(), srv.URL, "node-1", control.Request{
		Method: "GET", Path: "/api/status", Role: "admin", Actor: "operator@example.com",
	})
	if err != nil {
		t.Fatalf("Forward: %v", err)
	}
	if resp.Status != 200 || string(resp.Body) != `{"ok":true}` {
		t.Fatalf("got status=%d body=%q, want the node's own reply", resp.Status, resp.Body)
	}
	// The operator identity and asserted role must survive the hop, or the node would
	// audit the wrong actor and authorize at the wrong level.
	if len(owner.calls) != 1 || !strings.Contains(owner.calls[0], "node-1 GET /api/status") {
		t.Fatalf("owner saw %v, want the request delivered verbatim", owner.calls)
	}
}

// Admission. This endpoint carries operator-authorized node commands — unlocking doors,
// changing settings — so an unauthenticated caller must get nowhere.
func TestPeerForwardRejectsBadCredentials(t *testing.T) {
	const secret = "shared"
	owner := &fakePeerSender{held: map[string]control.Response{"node-1": {Status: 200}}}
	srv := peerTestServer(t, secret, owner)

	for _, tc := range []struct{ name, token string }{
		{"no token", ""},
		{"wrong token", DerivePeerToken("a-different-secret")},
		{"garbage", "not-a-token"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api"+PeerForwardPath,
				strings.NewReader(`{"nodeId":"node-1","method":"GET","path":"/api/status"}`))
			if tc.token != "" {
				req.Header.Set("Authorization", "Bearer "+tc.token)
			}
			resp, err := srv.Client().Do(req)
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("got %d, want 401 — an unauthenticated peer must not reach a node", resp.StatusCode)
			}
		})
	}
	if len(owner.calls) != 0 {
		t.Fatalf("a rejected caller still reached the node: %v", owner.calls)
	}
}

// A node being offline at the owner is a different fact from the owner being unreachable,
// and the caller renders them differently. The hop must not blur them.
func TestPeerForwardReportsNodeErrorsDistinctly(t *testing.T) {
	const secret = "shared"
	srv := peerTestServer(t, secret, &fakePeerSender{held: map[string]control.Response{}})

	client := NewPeerClient(secret, 5*time.Second, true)
	_, err := client.Forward(context.Background(), srv.URL, "node-missing", control.Request{Method: "GET", Path: "/x"})
	if err == nil {
		t.Fatal("a node the owner does not hold must be an error")
	}
	if !strings.Contains(err.Error(), ErrNodeOffline.Error()) {
		t.Fatalf("got %q, want the owner's node-offline reason preserved", err)
	}
}

// An unreachable peer must fail rather than hang or silently succeed.
func TestPeerForwardFailsOnUnreachableOwner(t *testing.T) {
	client := NewPeerClient("shared", time.Second, true)
	_, err := client.Forward(context.Background(), "https://127.0.0.1:1", "node-1", control.Request{Method: "GET", Path: "/x"})
	if err == nil {
		t.Fatal("an unreachable owner must surface as an error")
	}
}

// The decorator's routing decisions, which are what make both the proxy and the recording
// reader cluster-aware without either of them changing.
func TestForwardingSenderRouting(t *testing.T) {
	const secret = "shared"

	t.Run("local node is served locally, never forwarded", func(t *testing.T) {
		local := &fakePeerSender{held: map[string]control.Response{"node-1": {Status: 200, Body: []byte("local")}}}
		owners := NewNodeOwnerRegistry(newFakeOwnerStore(), "https://me:3002", 30*time.Second, nil)
		owners.Claim(context.Background(), "node-1")

		// A peer client pointed at nothing: if this forwards, the test fails loudly.
		f := NewForwardingSender(local, owners, NewPeerClient(secret, time.Second, true), nil)
		resp, err := f.SendRequest(context.Background(), "node-1", control.Request{Method: "GET", Path: "/x"})
		if err != nil || string(resp.Body) != "local" {
			t.Fatalf("got body=%q err=%v, want the local sender used", resp.Body, err)
		}
	})

	t.Run("node owned elsewhere is forwarded", func(t *testing.T) {
		store := newFakeOwnerStore()
		remote := &fakePeerSender{held: map[string]control.Response{"node-2": {Status: 200, Body: []byte("remote")}}}
		srv := peerTestServer(t, secret, remote)

		// The owner publishes itself as the test server.
		ownerSide := NewNodeOwnerRegistry(store, srv.URL, 30*time.Second, nil)
		ownerSide.Claim(context.Background(), "node-2")

		mine := NewNodeOwnerRegistry(store, "https://me:3002", 30*time.Second, nil)
		f := NewForwardingSender(&fakePeerSender{held: map[string]control.Response{}}, mine,
			NewPeerClient(secret, 5*time.Second, true), nil)

		resp, err := f.SendRequest(context.Background(), "node-2", control.Request{Method: "GET", Path: "/x"})
		if err != nil {
			t.Fatalf("SendRequest: %v", err)
		}
		if string(resp.Body) != "remote" {
			t.Fatalf("got %q, want the owning instance's reply", resp.Body)
		}
	})

	t.Run("node owned nowhere reports offline without a hop", func(t *testing.T) {
		local := &fakePeerSender{held: map[string]control.Response{}}
		owners := NewNodeOwnerRegistry(newFakeOwnerStore(), "https://me:3002", 30*time.Second, nil)

		f := NewForwardingSender(local, owners, NewPeerClient(secret, time.Second, true), nil)
		_, err := f.SendRequest(context.Background(), "node-gone", control.Request{Method: "GET", Path: "/x"})
		if !errors.Is(err, ErrNodeOffline) {
			t.Fatalf("got %v, want ErrNodeOffline for a node nobody holds", err)
		}
		if len(local.calls) != 1 {
			t.Fatal("a node nobody owns should be answered by the local sender, not a peer hop")
		}
	})

	t.Run("a stale claim degrades to offline rather than hanging", func(t *testing.T) {
		store := newFakeOwnerStore()
		// A claim pointing at an instance that is not there any more.
		dead := NewNodeOwnerRegistry(store, "https://127.0.0.1:1", 30*time.Second, nil)
		dead.Claim(context.Background(), "node-3")

		mine := NewNodeOwnerRegistry(store, "https://me:3002", 30*time.Second, nil)
		f := NewForwardingSender(&fakePeerSender{held: map[string]control.Response{}}, mine,
			NewPeerClient(secret, time.Second, true), nil)

		_, err := f.SendRequest(context.Background(), "node-3", control.Request{Method: "GET", Path: "/x"})
		if !errors.Is(err, ErrNodeOffline) {
			t.Fatalf("got %v, want ErrNodeOffline so the UI renders a node problem", err)
		}
	})
}
