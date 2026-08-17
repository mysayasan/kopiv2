package services

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func mediaTestServer(t *testing.T, secret string, answer func(context.Context, MediaOfferRequest) (MediaOfferReply, error)) *httptest.Server {
	t.Helper()
	h := NewPeerMediaOfferHandler(secret, answer, nil)
	mux := http.NewServeMux()
	mux.Handle("/api"+PeerMediaOfferPath, h)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// The happy path: the offer reaches the instance holding the media channel and its answer
// comes back intact. The answer carries THAT instance's ICE candidates, which is what makes
// the browser peer with it directly instead of through the load balancer.
func TestMediaOfferForwardedAndAnswered(t *testing.T) {
	const secret = "shared"
	var got MediaOfferRequest
	srv := mediaTestServer(t, secret, func(_ context.Context, req MediaOfferRequest) (MediaOfferReply, error) {
		got = req
		return MediaOfferReply{Type: "answer", SDP: "v=0 owning-instance-candidates"}, nil
	})

	client := NewPeerClient(secret, 5*time.Second, true)
	reply, err := client.ForwardMediaOffer(context.Background(), srv.URL, MediaOfferRequest{
		NodeID: "node-1", CameraID: 7, Type: "offer", SDP: "v=0 browser",
	})
	if err != nil {
		t.Fatalf("ForwardMediaOffer: %v", err)
	}
	if reply.Type != "answer" || !strings.Contains(reply.SDP, "owning-instance-candidates") {
		t.Fatalf("got %+v, want the owning instance's answer verbatim", reply)
	}
	// The camera must survive the hop, or a viewer would silently get a different one.
	if got.NodeID != "node-1" || got.CameraID != 7 {
		t.Fatalf("owner saw %+v, want node-1 camera 7", got)
	}
}

// This endpoint hands out a live video feed, so an unauthenticated caller must get nowhere.
func TestMediaOfferRejectsBadCredentials(t *testing.T) {
	const secret = "shared"
	called := false
	srv := mediaTestServer(t, secret, func(context.Context, MediaOfferRequest) (MediaOfferReply, error) {
		called = true
		return MediaOfferReply{Type: "answer"}, nil
	})

	for _, tc := range []struct{ name, token string }{
		{"no token", ""},
		{"wrong token", DerivePeerToken("another-secret")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api"+PeerMediaOfferPath,
				strings.NewReader(`{"nodeId":"node-1","cameraId":7,"type":"offer","sdp":"v=0"}`))
			if tc.token != "" {
				req.Header.Set("Authorization", "Bearer "+tc.token)
			}
			resp, err := srv.Client().Do(req)
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("got %d, want 401 — an unauthenticated caller must not reach a camera", resp.StatusCode)
			}
		})
	}
	if called {
		t.Fatal("a rejected caller still reached the negotiation")
	}
}

// A node whose media channel the owner does not actually hold must produce an error the
// caller can render, not a hang or an empty answer.
func TestMediaOfferReportsNotConnected(t *testing.T) {
	const secret = "shared"
	srv := mediaTestServer(t, secret, func(context.Context, MediaOfferRequest) (MediaOfferReply, error) {
		return MediaOfferReply{}, ErrMediaNotConnected
	})

	client := NewPeerClient(secret, 5*time.Second, true)
	_, err := client.ForwardMediaOffer(context.Background(), srv.URL, MediaOfferRequest{
		NodeID: "node-1", CameraID: 1, Type: "offer", SDP: "v=0",
	})
	if err == nil || !strings.Contains(err.Error(), ErrMediaNotConnected.Error()) {
		t.Fatalf("got %v, want the owner's not-connected reason preserved", err)
	}
}

// A malformed or incomplete offer must be refused before any negotiation is attempted.
func TestMediaOfferValidatesPayload(t *testing.T) {
	const secret = "shared"
	called := false
	srv := mediaTestServer(t, secret, func(context.Context, MediaOfferRequest) (MediaOfferReply, error) {
		called = true
		return MediaOfferReply{}, nil
	})
	token := DerivePeerToken(secret)

	for _, body := range []string{`{not json`, `{"nodeId":"","cameraId":7}`, `{"nodeId":"node-1","cameraId":0}`} {
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api"+PeerMediaOfferPath, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("body %q got %d, want 400", body, resp.StatusCode)
		}
	}
	if called {
		t.Fatal("an invalid offer reached the negotiation")
	}
}

// An unreachable owner must surface as an error rather than hanging the viewer.
func TestMediaOfferFailsOnUnreachableOwner(t *testing.T) {
	client := NewPeerClient("shared", time.Second, true)
	_, err := client.ForwardMediaOffer(context.Background(), "https://127.0.0.1:1", MediaOfferRequest{
		NodeID: "node-1", CameraID: 1, Type: "offer", SDP: "v=0",
	})
	if err == nil {
		t.Fatal("an unreachable owner must surface as an error")
	}
}

// The control channel and the media channel are separate connections that need not land on
// the same instance, so their ownership must be tracked under separate keys. Sharing one
// key would send a camera offer to an instance that holds only the command channel.
func TestMediaOwnershipIsTrackedSeparatelyFromControl(t *testing.T) {
	store := newFakeOwnerStore()
	ctx := context.Background()

	control := NewNodeOwnerRegistry(store, "https://a:3002", 30*time.Second, nil)
	media := NewMediaOwnerRegistry(store, "https://b:3012", 30*time.Second, nil)

	control.Claim(ctx, "node-1") // command channel on A
	media.Claim(ctx, "node-1")   // media channel on B

	// Each registry must resolve its OWN owner, not the other's.
	readControl := NewNodeOwnerRegistry(store, "https://c:3022", 30*time.Second, nil)
	readMedia := NewMediaOwnerRegistry(store, "https://c:3022", 30*time.Second, nil)

	if owner, _ := readControl.OwnerOf(ctx, "node-1"); owner != "https://a:3002" {
		t.Fatalf("control owner = %q, want A", owner)
	}
	if owner, _ := readMedia.OwnerOf(ctx, "node-1"); owner != "https://b:3012" {
		t.Fatalf("media owner = %q, want B — the two channels are independent", owner)
	}
}

// Releasing a media claim must not disturb the control claim for the same node.
func TestMediaReleaseLeavesControlOwnershipIntact(t *testing.T) {
	store := newFakeOwnerStore()
	ctx := context.Background()

	control := NewNodeOwnerRegistry(store, "https://a:3002", 30*time.Second, nil)
	media := NewMediaOwnerRegistry(store, "https://a:3002", 30*time.Second, nil)
	control.Claim(ctx, "node-1")
	media.Claim(ctx, "node-1")

	media.Release(ctx, "node-1")

	if !control.ConnectedAnywhere(ctx, "node-1") {
		t.Fatal("releasing the media channel dropped the control-channel claim")
	}
	if media.ConnectedAnywhere(ctx, "node-1") {
		t.Fatal("the media claim should be gone")
	}
}
