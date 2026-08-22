package services

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/mysayasan/kopiv2/apps/myseliasan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	"github.com/mysayasan/kopiv2/infra/fleetca"
	"github.com/mysayasan/kopiv2/infra/pairing"
)

// --- in-memory fakes for the two generic repos the registry uses ----------

// fakeSettingsRepo stands in for the control_setting table.
//
// The mutex is not decoration. The registry's heartbeat/grace-window paths touch settings
// from the goroutines under test while the test body reads them, so an unsynchronized fake
// reports a data race that belongs to the TEST rather than the code — noise that would sit
// in every nightly -race run and train people to ignore it.
type fakeSettingsRepo struct {
	dbsql.IGenericRepo[entities.ControlSetting]
	mu     sync.Mutex
	rows   []*entities.ControlSetting
	nextID int64
}

func (f *fakeSettingsRepo) GetByUnique(_ context.Context, _ string, _ string, uids ...any) (*entities.ControlSetting, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
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
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	m.Id = f.nextID
	cp := m
	f.rows = append(f.rows, &cp)
	return uint64(f.nextID), nil
}
func (f *fakeSettingsRepo) UpdateById(_ context.Context, _ string, m entities.ControlSetting) (uint64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
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
func (f *fakeNodesRepo) DeleteById(_ context.Context, _ string, id uint64) (uint64, error) {
	for i, r := range f.rows {
		if r.Id == int64(id) {
			f.rows = append(f.rows[:i], f.rows[i+1:]...)
			return 1, nil
		}
	}
	return 0, errors.New("no result found")
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

// pagingNodesRepo honors limit/offset so List's pagination loop can be exercised
// (the default fakeNodesRepo returns every row regardless of paging).
type pagingNodesRepo struct {
	dbsql.IGenericRepo[entities.ManagedNode]
	rows []*entities.ManagedNode
}

func (f *pagingNodesRepo) Get(_ context.Context, _ string, limit uint64, offset uint64, _ []sqldataenums.Filter, _ []sqldataenums.Sorter) ([]*entities.ManagedNode, uint64, error) {
	if offset >= uint64(len(f.rows)) {
		return nil, uint64(len(f.rows)), nil
	}
	end := offset + limit
	if end > uint64(len(f.rows)) {
		end = uint64(len(f.rows))
	}
	return f.rows[offset:end], uint64(len(f.rows)), nil
}

func TestRegistryListPaginatesBeyondPageSize(t *testing.T) {
	// More nodes than one page → List must accumulate every node, not truncate.
	total := nodeListPageSize*2 + 7
	repo := &pagingNodesRepo{}
	for i := 0; i < total; i++ {
		repo.rows = append(repo.rows, &entities.ManagedNode{Id: int64(i + 1), NodeId: strconv.Itoa(i)})
	}
	reg := newNodeRegistry(repo, &fakeSettingsRepo{}, NodeRegistryConfig{ParentID: "p"})
	got, err := reg.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != total {
		t.Fatalf("List truncated: got %d want %d", len(got), total)
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

func TestRegistryHeartbeatEmitsLostOnceThenRecovered(t *testing.T) {
	reg, nodes := newTestRegistry()
	ctx := context.Background()
	now := time.Now().Unix()
	// Online node with no contact well past grace → should flip to lost and emit exactly
	// one lost event; a second sweep (still lost) must NOT re-emit (edge-triggered).
	nodes.rows = append(nodes.rows, &entities.ManagedNode{Id: 1, NodeId: "node-x", Status: "online", LastSeenAt: now - 1000})

	var events []FleetEvent
	reg.SetFleetEventSink(func(e FleetEvent) { events = append(events, e) })

	reg.Heartbeat(ctx)
	reg.Heartbeat(ctx) // still lost, no contact

	lost := 0
	for _, e := range events {
		if e.Kind == FleetEventNodeLost {
			lost++
			if e.Node == nil || e.Node.NodeId != "node-x" {
				t.Fatalf("lost event missing node identity: %+v", e.Node)
			}
		}
	}
	if lost != 1 {
		t.Fatalf("expected exactly 1 lost event, got %d (events=%+v)", lost, events)
	}
	if nodes.rows[0].Status != "lost" {
		t.Fatalf("node should be lost, got %q", nodes.rows[0].Status)
	}

	// Now the node reconnects (control presence) → recovered event on the lost→online edge.
	reg.SetControlPresence(func(id string) bool { return id == "node-x" })
	events = nil
	reg.Heartbeat(ctx)
	if len(events) != 1 || events[0].Kind != FleetEventNodeRecovered {
		t.Fatalf("expected 1 recovered event, got %+v", events)
	}
	if nodes.rows[0].Status != "online" {
		t.Fatalf("node should be online after recovery, got %q", nodes.rows[0].Status)
	}
}

func TestRegistryHeartbeatWarnsOnExpiringCertOncePerExpiry(t *testing.T) {
	reg, nodes := newTestRegistry()
	reg.cfg.CertWarnBefore = 7 * 24 * time.Hour
	ctx := context.Background()
	now := time.Now().Unix()
	// A reachable node (control-connected, so liveness is fine) whose cert expires in 2
	// days — inside the 7-day warn window — must warn exactly once across repeated sweeps.
	nodes.rows = append(nodes.rows, &entities.ManagedNode{
		Id: 1, NodeId: "node-cert", Status: "online", LastSeenAt: now,
		CertExpiresAt: now + 2*24*3600,
	})
	reg.SetControlPresence(func(string) bool { return true })

	var certEvents []FleetEvent
	reg.SetFleetEventSink(func(e FleetEvent) {
		if e.Kind == FleetEventCertExpiring {
			certEvents = append(certEvents, e)
		}
	})

	reg.Heartbeat(ctx)
	reg.Heartbeat(ctx)
	if len(certEvents) != 1 {
		t.Fatalf("expected exactly 1 cert-expiring event, got %d", len(certEvents))
	}
	if certEvents[0].HoursLeft <= 0 || certEvents[0].HoursLeft > 48 {
		t.Fatalf("unexpected HoursLeft: %d", certEvents[0].HoursLeft)
	}

	// Renewal pushes the expiry out of the window → re-arms; a later cert with a new
	// (still-in-window) expiry warns again.
	nodes.rows[0].CertExpiresAt = now + 30*24*3600 // healthy, outside window
	reg.Heartbeat(ctx)
	nodes.rows[0].CertExpiresAt = now + 3*24*3600 // back inside window, new value
	reg.Heartbeat(ctx)
	if len(certEvents) != 2 {
		t.Fatalf("expected re-armed cert warning after renewal cycle, got %d total", len(certEvents))
	}
}

func TestRegistryFleetStatusRollup(t *testing.T) {
	reg, nodes := newTestRegistry()
	reg.cfg.CertWarnBefore = 7 * 24 * time.Hour
	now := time.Now().Unix()
	nodes.rows = append(nodes.rows,
		&entities.ManagedNode{Id: 1, NodeId: "a", Status: "online", CertExpiresAt: now + 30*24*3600},
		&entities.ManagedNode{Id: 2, NodeId: "b", Status: "lost", CertExpiresAt: now + 2*24*3600}, // expiring
		&entities.ManagedNode{Id: 3, NodeId: "c", Status: "self-dropped"},
		&entities.ManagedNode{Id: 4, NodeId: "d", Status: "online", CertExpiresAt: now - 3600}, // expired
	)
	st, err := reg.FleetStatus(context.Background())
	if err != nil {
		t.Fatalf("FleetStatus: %v", err)
	}
	if st.Total != 4 || st.Online != 2 || st.Lost != 1 || st.SelfDropped != 1 {
		t.Fatalf("liveness counts wrong: %+v", st)
	}
	if st.CertsExpiring != 1 || st.CertsExpired != 1 {
		t.Fatalf("cert counts wrong: expiring=%d expired=%d", st.CertsExpiring, st.CertsExpired)
	}
	if st.CertWarnDays != 7 {
		t.Fatalf("CertWarnDays: got %d want 7", st.CertWarnDays)
	}
}

// A node's FIRST enrollment (no prior cert) is always allowed, but a RENEWAL (it already
// has an issued cert) is refused unless auto-renew is on — the dead-man's switch that lets
// an un-blessed node's certificate lapse. Turning auto-renew on lets the next renewal through.
func TestRegistryEnrollGatesRenewalOnAutoRenew(t *testing.T) {
	reg, nodes := newTestRegistry()
	ctx := context.Background()
	nodes.rows = append(nodes.rows, &entities.ManagedNode{Id: 1, NodeId: "node-r", Token: "tok", Status: "online", AutoRenew: false})
	_, csrPEM, _ := fleetca.GenerateKeyAndCSR("node-r")

	// Initial enrollment (CertExpiresAt == 0) is allowed even with auto-renew off.
	if _, _, err := reg.Enroll(ctx, "node-r", "tok", csrPEM); err != nil {
		t.Fatalf("initial enroll: %v", err)
	}
	if nodes.rows[0].CertExpiresAt == 0 {
		t.Fatal("initial enroll should record CertExpiresAt")
	}

	// Now it has a cert → a second enroll is a RENEWAL, refused while auto-renew is off.
	if _, _, err := reg.Enroll(ctx, "node-r", "tok", csrPEM); !errors.Is(err, ErrRenewNotAuthorized) {
		t.Fatalf("gated renewal: got %v want ErrRenewNotAuthorized", err)
	}

	// Enable auto-renew → renewal is honoured again.
	if _, err := reg.SetAutoRenew(ctx, "node-r", true, 42); err != nil {
		t.Fatalf("SetAutoRenew: %v", err)
	}
	if _, _, err := reg.Enroll(ctx, "node-r", "tok", csrPEM); err != nil {
		t.Fatalf("renewal after enabling auto-renew: %v", err)
	}
}

// BackfillAutoRenew blesses already-enrolled nodes once so upgrading an existing fleet does
// not surprise-expire it, leaves never-enrolled records alone, and never runs twice.
func TestRegistryBackfillAutoRenewOnceForEnrolledNodes(t *testing.T) {
	nodes := &fakeNodesRepo{}
	settings := &fakeSettingsRepo{}
	reg := newNodeRegistry(nodes, settings, NodeRegistryConfig{ParentID: "p"})
	ctx := context.Background()
	nodes.rows = append(nodes.rows,
		&entities.ManagedNode{Id: 1, NodeId: "enrolled", CertExpiresAt: 1000, AutoRenew: false},
		&entities.ManagedNode{Id: 2, NodeId: "never", CertExpiresAt: 0, AutoRenew: false},
	)

	if err := reg.BackfillAutoRenew(ctx); err != nil {
		t.Fatalf("BackfillAutoRenew: %v", err)
	}
	if !nodes.rows[0].AutoRenew {
		t.Fatal("enrolled node should be backfilled to AutoRenew=true")
	}
	if nodes.rows[1].AutoRenew {
		t.Fatal("never-enrolled node must stay AutoRenew=false")
	}

	// Idempotent: an operator turning it back off must not be re-flipped by a later run.
	nodes.rows[0].AutoRenew = false
	if err := reg.BackfillAutoRenew(ctx); err != nil {
		t.Fatalf("second BackfillAutoRenew: %v", err)
	}
	if nodes.rows[0].AutoRenew {
		t.Fatal("backfill must not run twice (operator's off choice was overwritten)")
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
