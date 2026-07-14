package fleetnode

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/mysayasan/kopiv2/infra/pairing"
)

const testFleetKey = "fleet-key-for-tests-1234567890"

// adoptReq builds a valid adoption request signed with the given fleet key.
func adoptReq(t *testing.T, key, claimCode string) AdoptRequest {
	t.Helper()
	ts := time.Now().Unix()
	nonce := "nonce-1"
	parentID := "parent-1"
	return AdoptRequest{
		ParentID:      parentID,
		ParentName:    "HQ Control",
		ParentBaseURL: "https://hq.local:3002",
		ClaimCode:     claimCode,
		Nonce:         nonce,
		Timestamp:     ts,
		Assertion:     pairing.SignAssertion([]byte(key), parentID, nonce, strconv.FormatInt(ts, 10)),
	}
}

func newPairingSvc() (IPairingService, context.Context) {
	return NewPairingService(&fakeRuntimeSettingRepo{}, nil, "testapp", "Test Node", "1.0.0", KindCamera), context.Background()
}

func TestPairingFreshNodeNotDiscoverableWithoutKey(t *testing.T) {
	svc, ctx := newPairingSvc()
	st, err := svc.Status(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.Paired || st.FleetKeySet || st.Discoverable {
		t.Fatalf("fresh node should be unpaired, keyless, not discoverable: %+v", st)
	}
	if st.NodeID == "" {
		t.Fatal("node should have a stable id")
	}
}

func TestPairingSetFleetKeyMakesDiscoverable(t *testing.T) {
	svc, ctx := newPairingSvc()
	if err := svc.SetFleetKey(ctx, testFleetKey); err != nil {
		t.Fatalf("SetFleetKey: %v", err)
	}
	if !svc.Discoverable(ctx) {
		t.Fatal("node with key and unpaired should be discoverable")
	}
}

func TestPairingFleetKeyTooShort(t *testing.T) {
	svc, ctx := newPairingSvc()
	if err := svc.SetFleetKey(ctx, "short"); err != ErrPairingFleetKeyShort {
		t.Fatalf("short key: got %v want ErrPairingFleetKeyShort", err)
	}
}

func TestPairingAdoptHappyPath(t *testing.T) {
	svc, ctx := newPairingSvc()
	_ = svc.SetFleetKey(ctx, testFleetKey)
	code, _, err := svc.GenerateClaimCode(ctx)
	if err != nil {
		t.Fatalf("GenerateClaimCode: %v", err)
	}

	res, err := svc.Adopt(ctx, adoptReq(t, testFleetKey, code))
	if err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	if res.Token == "" || res.NodeID == "" {
		t.Fatalf("adopt result missing token/nodeId: %+v", res)
	}

	st, _ := svc.Status(ctx)
	if !st.Paired || st.ParentID != "parent-1" {
		t.Fatalf("node should be paired to parent-1: %+v", st)
	}
	if st.Discoverable {
		t.Fatal("paired node must not be discoverable")
	}
	if svc.Discoverable(ctx) {
		t.Fatal("Discoverable() must be false once paired")
	}
}

func TestPairingAdoptRejectsSecondParent(t *testing.T) {
	svc, ctx := newPairingSvc()
	_ = svc.SetFleetKey(ctx, testFleetKey)
	code, _, _ := svc.GenerateClaimCode(ctx)
	if _, err := svc.Adopt(ctx, adoptReq(t, testFleetKey, code)); err != nil {
		t.Fatalf("first Adopt: %v", err)
	}
	// A second control plane with a fresh claim code must be rejected.
	code2, _, _ := svc.GenerateClaimCode(ctx)
	if _, err := svc.Adopt(ctx, adoptReq(t, testFleetKey, code2)); err != ErrPairingAlreadyPaired {
		t.Fatalf("second Adopt: got %v want ErrPairingAlreadyPaired", err)
	}
}

func TestPairingAdoptRejectsBadAssertion(t *testing.T) {
	svc, ctx := newPairingSvc()
	_ = svc.SetFleetKey(ctx, testFleetKey)
	code, _, _ := svc.GenerateClaimCode(ctx)
	// Sign with the wrong key → assertion fails.
	if _, err := svc.Adopt(ctx, adoptReq(t, "wrong-key-aaaaaaaaaaaaaaa", code)); err != ErrPairingBadAssertion {
		t.Fatalf("bad assertion: got %v want ErrPairingBadAssertion", err)
	}
}

func TestPairingAdoptRejectsBadClaimCode(t *testing.T) {
	svc, ctx := newPairingSvc()
	_ = svc.SetFleetKey(ctx, testFleetKey)
	_, _, _ = svc.GenerateClaimCode(ctx)
	if _, err := svc.Adopt(ctx, adoptReq(t, testFleetKey, "WRONGCODE")); err != ErrPairingBadClaimCode {
		t.Fatalf("bad claim code: got %v want ErrPairingBadClaimCode", err)
	}
}

func TestPairingAdoptRejectsWithoutFleetKey(t *testing.T) {
	svc, ctx := newPairingSvc()
	if _, err := svc.Adopt(ctx, adoptReq(t, testFleetKey, "ANY")); err != ErrPairingFleetKeyUnset {
		t.Fatalf("no fleet key: got %v want ErrPairingFleetKeyUnset", err)
	}
}

func TestPairingClaimCodeIsSingleUse(t *testing.T) {
	svc, ctx := newPairingSvc()
	_ = svc.SetFleetKey(ctx, testFleetKey)
	code, _, _ := svc.GenerateClaimCode(ctx)
	if _, err := svc.Adopt(ctx, adoptReq(t, testFleetKey, code)); err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	// Release back to unpaired, then re-adopt with the consumed code must fail.
	st, _ := svc.Status(ctx)
	_ = st
	// Use Unpair to reset, then try the old code.
	if _, err := svc.Unpair(ctx); err != nil {
		t.Fatalf("Unpair: %v", err)
	}
	if _, err := svc.Adopt(ctx, adoptReq(t, testFleetKey, code)); err != ErrPairingBadClaimCode {
		t.Fatalf("reused claim code: got %v want ErrPairingBadClaimCode", err)
	}
}

func TestPairingReleaseRequiresToken(t *testing.T) {
	svc, ctx := newPairingSvc()
	_ = svc.SetFleetKey(ctx, testFleetKey)
	code, _, _ := svc.GenerateClaimCode(ctx)
	res, _ := svc.Adopt(ctx, adoptReq(t, testFleetKey, code))

	if err := svc.Release(ctx, "not-the-token"); err != ErrPairingBadToken {
		t.Fatalf("wrong token: got %v want ErrPairingBadToken", err)
	}
	if err := svc.Release(ctx, res.Token); err != nil {
		t.Fatalf("correct token Release: %v", err)
	}
	st, _ := svc.Status(ctx)
	if st.Paired {
		t.Fatal("node should be unpaired after Release")
	}
	if !svc.Discoverable(ctx) {
		t.Fatal("node should be discoverable again after Release")
	}
}

func TestPairingSelfUnpairReturnsParentURL(t *testing.T) {
	svc, ctx := newPairingSvc()
	_ = svc.SetFleetKey(ctx, testFleetKey)
	code, _, _ := svc.GenerateClaimCode(ctx)
	_, _ = svc.Adopt(ctx, adoptReq(t, testFleetKey, code))

	parentURL, err := svc.Unpair(ctx)
	if err != nil {
		t.Fatalf("Unpair: %v", err)
	}
	if parentURL != "https://hq.local:3002" {
		t.Fatalf("Unpair parent url: got %q", parentURL)
	}
	st, _ := svc.Status(ctx)
	if st.Paired {
		t.Fatal("node should be unpaired after self-drop")
	}
}

func TestPairingNodeIDStable(t *testing.T) {
	svc, ctx := newPairingSvc()
	a, _ := svc.Status(ctx)
	b, _ := svc.Status(ctx)
	if a.NodeID != b.NodeID || a.NodeID == "" {
		t.Fatalf("node id should be stable across reads: %q vs %q", a.NodeID, b.NodeID)
	}
}
