package services

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mysayasan/kopiv2/apps/myseliasan/entities"
	sharedentities "github.com/mysayasan/kopiv2/domain/entities"
	"github.com/mysayasan/kopiv2/domain/report"
	sharedservices "github.com/mysayasan/kopiv2/domain/shared/services"
)

// --- lightweight fakes: embed the interface, override only what the reports call -----

type fakeRegistry struct {
	INodeRegistry
	nodes  []*entities.ManagedNode
	status FleetStatus
}

func (f *fakeRegistry) List(context.Context) ([]*entities.ManagedNode, error) { return f.nodes, nil }
func (f *fakeRegistry) FleetStatus(context.Context) (FleetStatus, error)      { return f.status, nil }

type fakeSites struct {
	ISiteService
	sites  []*entities.Site
	plans  map[int64][]NodeFloorplan
	images map[int64]*FloorImage
}

func (f *fakeSites) ListSites(context.Context) ([]*entities.Site, error) { return f.sites, nil }
func (f *fakeSites) SiteFloorplans(_ context.Context, siteID int64) ([]NodeFloorplan, error) {
	return f.plans[siteID], nil
}
func (f *fakeSites) FloorImage(_ context.Context, id int64) (*FloorImage, error) {
	return f.images[id], nil
}

type fakeNotif struct{ rows []*sharedentities.Notification }

func (f *fakeNotif) List(context.Context, uint64, uint64, int64, bool, string, string) ([]*sharedentities.Notification, uint64, error) {
	return f.rows, uint64(len(f.rows)), nil
}

type fakeAudit struct {
	IAuditService
	rows []*entities.AuditLog
}

func (f *fakeAudit) List(context.Context, uint64, uint64, string, string, string) ([]*entities.AuditLog, uint64, error) {
	return f.rows, uint64(len(f.rows)), nil
}

type fakeUsers struct {
	IControlUserService
	rows []*entities.ControlUser
}

func (f *fakeUsers) List(context.Context) ([]*entities.ControlUser, error) { return f.rows, nil }

type rptRoles struct {
	sharedservices.IAccessRoleService
	rows []*sharedentities.AccessRole
}

func (f *rptRoles) List(context.Context) ([]*sharedentities.AccessRole, error) { return f.rows, nil }

type fakePerms struct {
	sharedservices.IAccessPermissionService
	byRole map[int64][]*sharedentities.AccessRolePermission
}

func (f *fakePerms) ListForRole(_ context.Context, roleID int64) ([]*sharedentities.AccessRolePermission, error) {
	return f.byRole[roleID], nil
}

func whitePNG(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = 255
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

func newTestReportService() *reportService {
	now := int64(1_700_000_000)
	reg := &fakeRegistry{
		status: FleetStatus{Total: 3, Online: 2, Lost: 1, CertsExpiring: 1, CertsExpired: 0, CertWarnDays: 7},
		nodes: []*entities.ManagedNode{
			{NodeId: "n1", Name: "Lobby NVR", SiteId: 1, Status: "online", LastSeenAt: now, CertExpiresAt: now + 86400*30, AutoRenew: true, Kind: "camera"},
			{NodeId: "n2", Name: "Warehouse Hub", SiteId: 1, Status: "lost", LastSeenAt: now - 3600, Kind: "iot"},
			{NodeId: "n3", Name: "Gate Cam", Status: "online", LastSeenAt: now},
		},
	}
	sites := &fakeSites{
		sites: []*entities.Site{
			{Id: 1, Name: "HQ Building", Kind: entities.SiteKindBuilding, Icon: "🏢", Description: "Head office", MapPlaced: true, Lat: 3.14, Lon: 101.6},
			{Id: 2, Name: "North Gate", Kind: entities.SiteKindPoint},
		},
		plans: map[int64][]NodeFloorplan{
			1: {{
				Floor: &entities.FloorPlan{Id: 10, SiteId: 1, Name: "Ground Floor", Width: 400, Height: 300,
					// A rectangular room outline with a door on the south wall and a window on
					// the north, plus a stair — the authored geometry the editor stores in Grid.
					Grid: `{"version":2,"unit":24,
						"segments":[
							{"x1":60,"y1":50,"x2":340,"y2":50},
							{"x1":340,"y1":50,"x2":340,"y2":250},
							{"x1":340,"y1":250,"x2":60,"y2":250},
							{"x1":60,"y1":250,"x2":60,"y2":50}],
						"doors":[{"cx":200,"cy":250,"w":44,"a":0}],
						"windows":[{"cx":200,"cy":50,"w":60,"a":0}],
						"stairs":[{"x1":270,"y1":90,"x2":320,"y2":210,"dir":"n","steps":10}]}`},
				Placements: []*entities.NodePlacement{
					{FloorId: 10, NodeId: "n1", CameraId: "c1", LastKnownName: "Entrance", X: 110, Y: 90, Heading: 135, Fov: 90, MountHeight: 2.5},
					{FloorId: 10, NodeId: "n1", LastKnownName: "NVR", X: 300, Y: 230},
				},
			}, {
				Floor: &entities.FloorPlan{Id: 11, SiteId: 1, Name: "First Floor", Ordinal: 1, Width: 400, Height: 300,
					Grid: `{"version":2,"unit":24,"segments":[{"x1":60,"y1":60,"x2":340,"y2":60},{"x1":340,"y1":60,"x2":340,"y2":240},{"x1":340,"y1":240,"x2":60,"y2":240},{"x1":60,"y1":240,"x2":60,"y2":60}],"doors":[{"cx":60,"cy":150,"w":40,"a":1.5708}]}`},
				Placements: []*entities.NodePlacement{
					{FloorId: 11, NodeId: "n1", CameraId: "c2", LastKnownName: "Corridor", X: 200, Y: 150, Heading: 90, Fov: 70, MountHeight: 2.4},
				},
			}},
		},
		images: map[int64]*FloorImage{
			10: {Data: whitePNG(400, 300), ContentType: "image/png"},
			11: {Data: whitePNG(400, 300), ContentType: "image/png"},
		},
	}
	notif := &fakeNotif{rows: []*sharedentities.Notification{
		{Id: 1, Category: "motion", Severity: "warning", Title: "Motion at entrance", Body: "Person detected", Source: "Lobby NVR", CreatedAt: now - 100},
		{Id: 2, Category: "health", Title: "Node lost", Source: "Warehouse Hub", CreatedAt: now - 200},
		{Id: 3, Category: "motion", Source: "Lobby NVR", CreatedAt: now - 300},
	}}
	audit := &fakeAudit{rows: []*entities.AuditLog{
		{Id: 1, Action: "node.adopt", ActorEmail: "admin@site", TargetType: "node", TargetId: "n1", Outcome: "success", Detail: "adopted Lobby NVR", CreatedAt: now - 500},
	}}
	users := &fakeUsers{rows: []*entities.ControlUser{
		{Id: 1, Kind: "local", Username: "admin", Name: "Stock Admin", RoleId: 1, IsStock: true, LastLoginAt: now},
		{Id: 2, Kind: "federated", Email: "op@site", Name: "Operator", RoleId: 2},
	}}
	roles := &rptRoles{rows: []*sharedentities.AccessRole{
		{Id: 1, Name: "Superadmin", IsSuperadmin: true, Builtin: true, Description: "Full access"},
		{Id: 2, Name: "Viewer", Builtin: true, Description: "Read-only"},
	}}
	perms := &fakePerms{byRole: map[int64][]*sharedentities.AccessRolePermission{
		2: {{RoleId: 2, Path: "/api/nodes", CanGet: true}, {RoleId: 2, Path: "/api/sites", CanGet: true}},
	}}
	return NewReportService(reg, sites, nil, audit, users, roles, perms, nil).(*reportService).withNotif(notif)
}

type fakeBriefer struct{ b Briefing }

func (f *fakeBriefer) GenerateBriefing(context.Context, int) (Briefing, error) { return f.b, nil }

// The executive summary must appear on the range reports when a briefer is
// wired, and its absence (nil briefer) must leave the report exactly as before.
func TestFleetHealthExecutiveSummary(t *testing.T) {
	svc := newTestReportService()
	svc.briefer = &fakeBriefer{b: Briefing{
		Lines:     []string{"1 critical event(s) in the last 168h.", "Node Gate cam (gate) is lost; last seen 2026-08-01 10:00."},
		Narrative: "One critical event and one lost node need attention.",
		Model:     "test-model",
	}}
	rep, err := svc.FleetHealth(context.Background(), time.Date(2026, 8, 6, 8, 0, 0, 0, time.UTC), 7)
	if err != nil {
		t.Fatalf("FleetHealth: %v", err)
	}
	if len(rep.Data) == 0 || string(rep.Data[:5]) != "%PDF-" {
		t.Fatal("not a PDF")
	}
	// A PDF's text is compressed, so assert behavior instead: the summary version
	// must be LARGER than the no-briefer version of the same report.
	svc.briefer = nil
	plain, err := svc.FleetHealth(context.Background(), time.Date(2026, 8, 6, 8, 0, 0, 0, time.UTC), 7)
	if err != nil {
		t.Fatalf("FleetHealth plain: %v", err)
	}
	if len(rep.Data) <= len(plain.Data) {
		t.Fatalf("summary report (%d bytes) should exceed plain report (%d bytes)", len(rep.Data), len(plain.Data))
	}
}

// withNotif swaps in a fake notification lister for the test (the constructor takes the
// concrete *notification.Service, which needs a DB we don't want in a unit test).
func (r *reportService) withNotif(n notifLister) *reportService {
	r.notif = n
	return r
}

func assertPDF(t *testing.T, rep *Report, err error, wantName string) {
	t.Helper()
	if err != nil {
		t.Fatalf("builder error = %v", err)
	}
	if rep == nil || len(rep.Data) == 0 {
		t.Fatal("empty report")
	}
	if !bytes.HasPrefix(rep.Data, []byte("%PDF-")) {
		t.Fatalf("not a PDF: %q", rep.Data[:min(8, len(rep.Data))])
	}
	if len(rep.Data) < 1200 {
		t.Fatalf("PDF too small: %d bytes", len(rep.Data))
	}
	if rep.Filename == "" {
		t.Fatal("missing filename")
	}
	// Optional: dump the rendered PDFs for manual inspection (REPORT_DUMP_DIR=<dir>).
	if dir := os.Getenv("REPORT_DUMP_DIR"); dir != "" {
		_ = os.WriteFile(filepath.Join(dir, wantName+".pdf"), rep.Data, 0o644)
	}
}

// TestInventoryEmbedsRealFloorPlan drives the image pipeline through the REAL
// siteService (reads an encrypted-at-rest-style plan from disk, decrypts, composites
// pins, embeds), proving the inventory report actually embeds a floor-plan image and
// not just the placement table.
func TestInventoryEmbedsRealFloorPlan(t *testing.T) {
	svc, _, _, _ := seedFloorWithPlan(t) // real *siteService, floor id 7 with a plan on disk
	rs := &reportService{sites: svc}

	doc := report.New(report.Options{Title: "Inventory", GeneratedAt: time.Unix(1_700_000_000, 0)})
	doc.H1("HQ Building")
	floor := &entities.FloorPlan{Id: 7, Name: "Ground floor", Width: 640, Height: 480}
	placements := []*entities.NodePlacement{
		{FloorId: 7, NodeId: "n1", CameraId: "c1", X: 100, Y: 120, Heading: 30, Fov: 90, LastKnownName: "Lobby"},
	}
	if err := rs.renderFloorPlan(context.Background(), doc, floor, placements); err != nil {
		t.Fatalf("renderFloorPlan() error = %v", err)
	}
	out, err := doc.Output()
	if err != nil {
		t.Fatalf("Output() error = %v", err)
	}
	if !bytes.Contains(out, []byte("/Subtype /Image")) {
		t.Fatal("inventory PDF embeds no image — floor plan was not rendered")
	}
}

func TestReportBuilders(t *testing.T) {
	svc := newTestReportService()
	now := time.Date(2026, 7, 26, 9, 0, 0, 0, time.UTC)
	ctx := context.Background()

	rep, err := svc.FleetHealth(ctx, now, 30)
	assertPDF(t, rep, err, "fleet-health")

	rep, err = svc.Inventory(ctx, now, 0)
	assertPDF(t, rep, err, "inventory")

	rep, err = svc.Inventory(ctx, now, 1) // single building, exercises floor-plan render
	assertPDF(t, rep, err, "inventory")

	rep, err = svc.Security(ctx, now, 30)
	assertPDF(t, rep, err, "security")

	rep, err = svc.Incident(ctx, now, 30, 0)
	assertPDF(t, rep, err, "incident")

	rep, err = svc.Incident(ctx, now, 30, 1) // single event
	assertPDF(t, rep, err, "incident")
}
