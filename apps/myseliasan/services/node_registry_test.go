package services

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/mysayasan/kopiv2/apps/myseliasan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	"github.com/mysayasan/kopiv2/infra/fleetca"
	"github.com/mysayasan/kopiv2/infra/pairing"
)

// --- in-memory fakes for the two generic repos the registry uses ----------

type fakeSettingsRepo struct {
	dbsql.IGenericRepo[entities.ControlSetting]
	rows   []*entities.ControlSetting
	nextID int64
}

func (f *fakeSettingsRepo) GetByUnique(_ context.Context, _ string, _ string, uids ...any) (*entities.ControlSetting, error) {
	key, _ := uids[0].(string)
	for _, r := range f.rows {
		if r.Key == key {
			cp := *r
			return &cp, nil
		}
	}
	return nil, errors.New("no result found")
}
func (f *fakeSettingsRepo) Create(_ context.Context, _ string, m entities.ControlSetting) (uint64, error) {
	f.nextID++
	m.Id = f.nextID
	cp := m
	f.rows = append(f.rows, &cp)
	return uint64(f.nextID), nil
}
func (f *fakeSettingsRepo) UpdateById(_ context.Context, _ string, m entities.ControlSetting) (uint64, error) {
	for _, r := range f.rows {
		if r.Id == m.Id {
			*r = m
			return 1, nil
		}
	}
	return 0, nil
}

type fakeNodesRepo struct {
	dbsql.IGenericRepo[entities.ManagedNode]
	rows   []*entities.ManagedNode
	nextID int64
}

func (f *fakeNodesRepo) Get(_ context.Context, _ string, _ uint64, _ uint64, _ []sqldataenums.Filter, _ []sqldataenums.Sorter) ([]*entities.ManagedNode, uint64, error) {
	return f.rows, uint64(len(f.rows)), nil
}
func (f *fakeNodesRepo) GetByUnique(_ context.Context, _ string, _ string, uids ...any) (*entities.ManagedNode, error) {
	id, _ := uids[0].(string)
	for _, r := range f.rows {
		if r.NodeId == id {
			cp := *r
			return &cp, nil
		}
	}
	return nil, errors.New("no result found")
}
func (f *fakeNodesRepo) Create(_ context.Context, _ string, m entities.ManagedNode) (uint64, error) {
	f.nextID++
	m.Id = f.nextID
	cp := m
	f.rows = append(f.rows, &cp)
	return uint64(f.nextID), nil
}
func (f *fakeNodesRepo) UpdateById(_ context.Context, _ string, m entities.ManagedNode) (uint64, error) {
	for _, r := range f.rows {
		if r.Id == m.Id {
			*r = m
			return 1, nil
		}
	}
	return 0, nil
}

func newTestRegistry() (*nodeRegistry, *fakeNodesRepo) {
	nodes := &fakeNodesRepo{}
	cfg := NodeRegistryConfig{
		MulticastAddr: pairing.DefaultMulticastAddr,
		ParentID:      "parent-1",
		ParentName:    "HQ",
		ParentBaseURL: "https://hq.local:3002",
		MTLSPort:      49532,
	}
	return newNodeRegistry(nodes, &fakeSettingsRepo{}, cfg), nodes
}

func TestRegistryFleetKeyGenerateAndRead(t *testing.T) {
	reg, _ := newTestRegistry()
	ctx := context.Background()

	if k, _ := reg.FleetKey(ctx); k != "" {
		t.Fatalf("fresh registry should have no fleet key, got %q", k)
	}
	gen, err := reg.GenerateFleetKey(ctx)
	if err != nil || gen == "" {
		t.Fatalf("GenerateFleetKey: %v / %q", err, gen)
	}
	got, _ := reg.FleetKey(ctx)
	if got != gen {
		t.Fatalf("persisted key mismatch: got %q want %q", got, gen)
	}
	if len(gen) < 16 {
		t.Fatalf("generated key too short: %q", gen)
	}
}

func TestRegistrySetFleetKeyValidatesLength(t *testing.T) {
	reg, _ := newTestRegistry()
	if err := reg.SetFleetKey(context.Background(), "short"); err == nil {
		t.Fatal("expected error for short fleet key")
	}
}

func TestRegistryScanRequiresFleetKey(t *testing.T) {
	reg, _ := newTestRegistry()
	if _, err := reg.Scan(context.Background(), time.Second); !errors.Is(err, ErrFleetKeyUnset) {
		t.Fatalf("Scan without key: got %v want ErrFleetKeyUnset", err)
	}
}

func TestRegistryListEmpty(t *testing.T) {
	reg, _ := newTestRegistry()
	nodes, err := reg.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(nodes) != 0 {
		t.Fatalf("expected no nodes, got %d", len(nodes))
	}
}

func TestRegistryEnrollSignsCSRForKnownToken(t *testing.T) {
	reg, nodes := newTestRegistry()
	ctx := context.Background()
	nodes.rows = append(nodes.rows, &entities.ManagedNode{Id: 1, NodeId: "node-7", Token: "secret-token", Status: "online"})
	_, csrPEM, err := fleetca.GenerateKeyAndCSR("node-7")
	if err != nil {
		t.Fatalf("GenerateKeyAndCSR: %v", err)
	}

	// Wrong token → rejected.
	if _, _, err := reg.Enroll(ctx, "node-7", "wrong", csrPEM); !errors.Is(err, ErrAdoptRejected) {
		t.Fatalf("wrong token: got %v want ErrAdoptRejected", err)
	}
	// Unknown node → rejected.
	if _, _, err := reg.Enroll(ctx, "ghost", "secret-token", csrPEM); !errors.Is(err, ErrNodeUnknown) {
		t.Fatalf("unknown node: got %v want ErrNodeUnknown", err)
	}
	// Correct token → issued cert + CA root, and cert expiry recorded.
	certPEM, caRoot, err := reg.Enroll(ctx, "node-7", "secret-token", csrPEM)
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}
	if len(certPEM) == 0 || len(caRoot) == 0 {
		t.Fatal("Enroll returned empty cert or CA root")
	}
	if nodes.rows[0].CertExpiresAt == 0 {
		t.Fatal("CertExpiresAt should be set after enrollment")
	}
}

func TestRegistryEnrollRefusesRevokedNode(t *testing.T) {
	reg, nodes := newTestRegistry()
	ctx := context.Background()
	nodes.rows = append(nodes.rows, &entities.ManagedNode{Id: 1, NodeId: "node-8", Token: "tok", Status: "online"})
	_, csrPEM, _ := fleetca.GenerateKeyAndCSR("node-8")
	if err := reg.ca.Revoke(ctx, "node-8"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if _, _, err := reg.Enroll(ctx, "node-8", "tok", csrPEM); !errors.Is(err, ErrNodeRevoked) {
		t.Fatalf("revoked enroll: got %v want ErrNodeRevoked", err)
	}
}

func TestRegistryHeartbeatControlChannelIsAuthoritative(t *testing.T) {
	reg, nodes := newTestRegistry()
	ctx := context.Background()
	now := time.Now().Unix()
	// A connected node whose mTLS port is unreachable (the test has no real listener)
	// must still be online purely from its live control channel.
	nodes.rows = append(nodes.rows, &entities.ManagedNode{Id: 1, NodeId: "node-conn", Status: "lost", LastSeenAt: 0})
	reg.SetControlPresence(func(id string) bool { return id == "node-conn" })

	reg.Heartbeat(ctx)

	if nodes.rows[0].Status != "online" {
		t.Fatalf("control-connected node: got status %q want online", nodes.rows[0].Status)
	}
	if nodes.rows[0].LastSeenAt < now {
		t.Fatal("control-connected node should bump LastSeenAt")
	}
}

func TestRegistryHeartbeatGraceWindow(t *testing.T) {
	reg, nodes := newTestRegistry()
	ctx := context.Background()
	now := time.Now().Unix()
	// No control presence and no reachable mTLS port. A node seen recently stays online
	// (within grace); a node not seen for well past the grace window goes lost.
	nodes.rows = append(nodes.rows,
		&entities.ManagedNode{Id: 1, NodeId: "node-recent", Status: "online", LastSeenAt: now - 10},
		&entities.ManagedNode{Id: 2, NodeId: "node-stale", Status: "online", LastSeenAt: now - 1000},
	)

	reg.Heartbeat(ctx)

	if nodes.rows[0].Status != "online" {
		t.Fatalf("recently-seen node within grace: got %q want online", nodes.rows[0].Status)
	}
	if nodes.rows[1].Status != "lost" {
		t.Fatalf("stale node past grace: got %q want lost", nodes.rows[1].Status)
	}
}

func TestRegistryMarkSelfDroppedVerifiesAssertion(t *testing.T) {
	reg, nodes := newTestRegistry()
	ctx := context.Background()
	key, _ := reg.GenerateFleetKey(ctx)
	now := time.Now().Unix()
	nodes.rows = append(nodes.rows, &entities.ManagedNode{Id: 1, NodeId: "node-9", Status: "online", AdoptedAt: now})

	// Forged assertion (wrong key) → rejected, status unchanged.
	bad := pairing.SignAssertion([]byte("not-the-fleet-key-xxxxxx"), "node-9", "n1", strconv.FormatInt(now, 10))
	if err := reg.MarkSelfDropped(ctx, "node-9", "n1", now, bad); !errors.Is(err, ErrAdoptRejected) {
		t.Fatalf("forged self-drop: got %v want ErrAdoptRejected", err)
	}
	if nodes.rows[0].Status != "online" {
		t.Fatal("forged self-drop must not change status")
	}

	// Valid assertion → node marked self-dropped.
	good := pairing.SignAssertion([]byte(key), "node-9", "n2", strconv.FormatInt(now, 10))
	if err := reg.MarkSelfDropped(ctx, "node-9", "n2", now, good); err != nil {
		t.Fatalf("valid self-drop: %v", err)
	}
	if nodes.rows[0].Status != "self-dropped" {
		t.Fatalf("status not updated: %q", nodes.rows[0].Status)
	}
}
