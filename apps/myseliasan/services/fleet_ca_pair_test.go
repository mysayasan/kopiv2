package services

import (
	"context"
	"crypto/tls"
	"testing"

	"github.com/mysayasan/kopiv2/infra/fleetca"
)

// newTestFleetCA builds a CA backed by the in-memory settings repo the registry tests use.
func newTestFleetCA(t *testing.T) *fleetCA {
	t.Helper()
	return newFleetCA(&fakeSettingsRepo{}, "test-parent", 0, nil)
}

func mustEnsure(t *testing.T, f *fleetCA) *fleetca.CA {
	t.Helper()
	ca, err := f.ensure(context.Background())
	if err != nil {
		t.Fatalf("ensure CA: %v", err)
	}
	return ca
}

// A mismatched stored pair must be detected and replaced, not served.
//
// The parent certificate and its private key live in two separate rows, so two instances
// issuing at the same moment can interleave their writes and leave one instance's
// certificate beside the other's key. Nothing catches that at write time, and the broken
// pair then persists: both rows are non-empty and not near expiry, so every later boot
// used to hand it straight to the TLS listener, which failed with "private key does not
// match public key". The fleet's control channel never starts — every node dark — and it
// stays that way until somebody clears the rows by hand.
func TestParentCertReissuesOnMismatchedStoredPair(t *testing.T) {
	ctx := context.Background()
	ca := newTestFleetCA(t)

	// A healthy pair first, so there is something to corrupt.
	certA, keyA, err := ca.parentCert(ctx, mustEnsure(t, ca))
	if err != nil {
		t.Fatalf("first parentCert: %v", err)
	}
	if _, err := tls.X509KeyPair(certA, keyA); err != nil {
		t.Fatalf("the freshly issued pair should be valid: %v", err)
	}

	// Simulate the interleaved write: a second instance's key lands beside the first
	// instance's certificate.
	other, otherKey, err := mustEnsure(t, ca).IssueLeaf("other-instance", parentCertTTL, nil)
	if err != nil {
		t.Fatalf("issue second leaf: %v", err)
	}
	_ = other
	if err := ca.writeSecret(ctx, parentKeyKey, string(otherKey)); err != nil {
		t.Fatalf("write mismatched key: %v", err)
	}

	// The next read must NOT return the broken pair.
	certPEM, keyPEM, err := ca.parentCert(ctx, mustEnsure(t, ca))
	if err != nil {
		t.Fatalf("parentCert after corruption: %v", err)
	}
	if _, err := tls.X509KeyPair(certPEM, keyPEM); err != nil {
		t.Fatalf("a mismatched stored pair was served instead of re-issued: %v", err)
	}

	// And the repair is persisted, so the next boot is healthy too rather than
	// re-issuing forever.
	storedCert, _ := ca.read(ctx, parentCertKey)
	storedKey, _ := ca.readSecret(ctx, parentKeyKey)
	if _, err := tls.X509KeyPair([]byte(storedCert), []byte(storedKey)); err != nil {
		t.Fatalf("the repaired pair was not stored: %v", err)
	}
}

// The ordinary path must stay stable: a healthy stored pair is reused rather than
// re-issued on every call, or every boot would churn the fleet's parent certificate.
func TestParentCertReusesHealthyStoredPair(t *testing.T) {
	ctx := context.Background()
	ca := newTestFleetCA(t)

	first, firstKey, err := ca.parentCert(ctx, mustEnsure(t, ca))
	if err != nil {
		t.Fatalf("first parentCert: %v", err)
	}
	second, secondKey, err := ca.parentCert(ctx, mustEnsure(t, ca))
	if err != nil {
		t.Fatalf("second parentCert: %v", err)
	}
	if string(first) != string(second) || string(firstKey) != string(secondKey) {
		t.Fatal("a healthy pair should be reused, not re-issued")
	}
}
