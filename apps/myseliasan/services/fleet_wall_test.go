package services

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mysayasan/kopiv2/apps/myseliasan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

type fakeWallRepo struct {
	dbsql.IGenericRepo[entities.FleetWall]
	rows []*entities.FleetWall
	seq  int64
}

func (f *fakeWallRepo) Get(_ context.Context, _ string, _, _ uint64, _ []sqldataenums.Filter, _ []sqldataenums.Sorter) ([]*entities.FleetWall, uint64, error) {
	out := []*entities.FleetWall{}
	for _, row := range f.rows {
		cp := *row
		out = append(out, &cp)
	}
	return out, uint64(len(out)), nil
}

func (f *fakeWallRepo) GetById(_ context.Context, _ string, id uint64) (*entities.FleetWall, error) {
	for _, row := range f.rows {
		if row.Id == int64(id) {
			cp := *row
			return &cp, nil
		}
	}
	return nil, errors.New("no result found")
}

func (f *fakeWallRepo) Create(_ context.Context, _ string, model entities.FleetWall) (uint64, error) {
	f.seq++
	model.Id = f.seq
	f.rows = append(f.rows, &model)
	return uint64(model.Id), nil
}

func (f *fakeWallRepo) UpdateById(_ context.Context, _ string, model entities.FleetWall) (uint64, error) {
	for i, row := range f.rows {
		if row.Id == model.Id {
			cp := model
			f.rows[i] = &cp
			return 1, nil
		}
	}
	return 0, errors.New("no result found")
}

func (f *fakeWallRepo) DeleteById(_ context.Context, _ string, id uint64) (uint64, error) {
	for i, row := range f.rows {
		if row.Id == int64(id) {
			f.rows = append(f.rows[:i], f.rows[i+1:]...)
			return 1, nil
		}
	}
	return 0, nil
}

func wallRig(nodes ...*entities.ManagedNode) (*fleetWallService, *fakeWallRepo) {
	repo := &fakeWallRepo{}
	return newFleetWallServiceWith(repo, fakeNodeList(nodes), nil), repo
}

func onlineNode(id, name string) *entities.ManagedNode {
	return &entities.ManagedNode{NodeId: id, Name: name, Kind: "camera", Status: "online"}
}

func saveWall(t *testing.T, svc *fleetWallService, req SaveFleetWallRequest) *FleetWallView {
	t.Helper()
	view, err := svc.Save(context.Background(), req, 1, "sam")
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	return view
}

// THE WHOLE POINT OF THIS FEATURE. A tile names an APPLIANCE as well as a camera, and the
// arrangement survives the round trip in order — a wall whose tiles come back shuffled is a
// wall somebody has to rebuild every time they open it.
func TestAWallSpansAppliancesAndKeepsItsOrder(t *testing.T) {
	svc, _ := wallRig(onlineNode("node-a", "Lobby NVR"), onlineNode("node-b", "Warehouse NVR"))

	view := saveWall(t, svc, SaveFleetWallRequest{
		Name: "Night shift", Grid: "2x2",
		Tiles: []FleetWallTile{
			{NodeId: "node-b", CameraId: 7},
			{NodeId: "node-a", CameraId: 3},
			{NodeId: "node-b", CameraId: 2},
		},
	})

	if len(view.TileList) != 3 {
		t.Fatalf("wanted 3 tiles, got %d", len(view.TileList))
	}
	got := []string{}
	for _, tile := range view.TileList {
		got = append(got, tileKey(tile.NodeId, tile.CameraId))
	}
	want := "node-b:7,node-a:3,node-b:2"
	if strings.Join(got, ",") != want {
		t.Fatalf("tiles came back as %q, want %q — a wall that reorders itself is a wall "+
			"somebody rebuilds every time they open it", strings.Join(got, ","), want)
	}
	if view.TileList[0].NodeName != "Warehouse NVR" || view.TileList[1].NodeName != "Lobby NVR" {
		t.Fatalf("tiles were not resolved to their appliances: %+v", view.TileList)
	}
}

// A tile whose appliance is OFFLINE will never show a picture, and a wall that renders it as a
// black rectangle is indistinguishable from a dark room. A tile whose appliance is GONE is a
// different problem with a different answer, so it gets a different count.
func TestOfflineAndMissingAppliancesAreDifferentAnswers(t *testing.T) {
	svc, _ := wallRig(
		onlineNode("node-a", "Lobby NVR"),
		&entities.ManagedNode{NodeId: "node-b", Name: "Warehouse NVR", Status: "lost"},
	)

	view := saveWall(t, svc, SaveFleetWallRequest{
		Name: "Mixed", Grid: "2x2",
		Tiles: []FleetWallTile{
			{NodeId: "node-a", CameraId: 1},
			{NodeId: "node-b", CameraId: 2},
			{NodeId: "node-gone", CameraId: 3},
		},
	})

	if view.OfflineTiles != 1 {
		t.Fatalf("offline tiles = %d, want 1", view.OfflineTiles)
	}
	if view.UnknownTiles != 1 {
		t.Fatalf("tiles on an appliance no longer in the fleet = %d, want 1 — 'offline' and "+
			"'not in this fleet' send somebody to two different places", view.UnknownTiles)
	}
	// AND IT KEEPS ITS PLACE. An operator who built a wall of three and sees two has been
	// told nothing, and the missing one is the building they will not think to check.
	if len(view.TileList) != 3 {
		t.Fatalf("a tile was silently dropped: %d remain", len(view.TileList))
	}
	if view.TileList[2].NodeKnown {
		t.Fatal("a tile on an appliance that is not in this fleet was reported as known")
	}
}

func TestAWallRefusesWhatItCannotRender(t *testing.T) {
	svc, _ := wallRig(onlineNode("node-a", "A"))
	base := func() SaveFleetWallRequest {
		return SaveFleetWallRequest{Name: "W", Grid: "2x2",
			Tiles: []FleetWallTile{{NodeId: "node-a", CameraId: 1}}}
	}
	cases := map[string]func(*SaveFleetWallRequest){
		"no name":             func(r *SaveFleetWallRequest) { r.Name = "  " },
		"a layout nobody has": func(r *SaveFleetWallRequest) { r.Grid = "7x9" },
		"no cameras":          func(r *SaveFleetWallRequest) { r.Tiles = nil },
		"a tile with no appliance": func(r *SaveFleetWallRequest) {
			r.Tiles = []FleetWallTile{{CameraId: 1}}
		},
		"a tile with no camera": func(r *SaveFleetWallRequest) {
			r.Tiles = []FleetWallTile{{NodeId: "node-a"}}
		},
		"a strobe instead of a rotation":     func(r *SaveFleetWallRequest) { r.CycleSeconds = 1 },
		"a pop nobody could look up in time": func(r *SaveFleetWallRequest) { r.AutoPopSeconds = 2 },
	}
	for name, mutate := range cases {
		req := base()
		mutate(&req)
		if _, err := svc.Save(context.Background(), req, 1, "sam"); err == nil {
			t.Fatalf("%s was accepted", name)
		}
	}
}

// The same camera twice costs a second relayed stream of the same picture, and means one of
// the tiles the operator is watching is redundant at the moment they most need the space.
func TestTheSameCameraCannotBeOnAWallTwice(t *testing.T) {
	svc, _ := wallRig(onlineNode("node-a", "A"))
	_, err := svc.Save(context.Background(), SaveFleetWallRequest{
		Name: "Doubled", Grid: "2x2",
		Tiles: []FleetWallTile{{NodeId: "node-a", CameraId: 4}, {NodeId: "node-a", CameraId: 4}},
	}, 1, "sam")
	if err == nil {
		t.Fatal("the same camera was accepted twice on one wall")
	}
	// ...but the SAME camera id on a DIFFERENT appliance is a different camera, and refusing
	// it would make camera numbering on one recorder silently constrain every other.
	if _, err := svc.Save(context.Background(), SaveFleetWallRequest{
		Name: "Two buildings", Grid: "2x2",
		Tiles: []FleetWallTile{{NodeId: "node-a", CameraId: 4}, {NodeId: "node-b", CameraId: 4}},
	}, 1, "sam"); err != nil {
		t.Fatalf("camera 4 on two different appliances was refused: %v", err)
	}
}

// "The default" with two answers is a screen that opens differently depending on how the
// database felt about sorting.
func TestOnlyOneWallCanBeTheDefault(t *testing.T) {
	svc, repo := wallRig(onlineNode("node-a", "A"))
	saveWall(t, svc, SaveFleetWallRequest{Name: "First", Grid: "2x2", IsDefault: true,
		Tiles: []FleetWallTile{{NodeId: "node-a", CameraId: 1}}})
	saveWall(t, svc, SaveFleetWallRequest{Name: "Second", Grid: "2x2", IsDefault: true,
		Tiles: []FleetWallTile{{NodeId: "node-a", CameraId: 2}}})

	defaults := 0
	for _, row := range repo.rows {
		if row.IsDefault {
			defaults++
		}
	}
	if defaults != 1 {
		t.Fatalf("%d walls claim to be the default", defaults)
	}
	for _, row := range repo.rows {
		if row.IsDefault && row.Name != "Second" {
			t.Fatalf("the wrong wall kept the default: %q", row.Name)
		}
	}
}

func TestAWallNameIsTakenOnlyOnce(t *testing.T) {
	svc, _ := wallRig(onlineNode("node-a", "A"))
	saveWall(t, svc, SaveFleetWallRequest{Name: "Front desk", Grid: "2x2",
		Tiles: []FleetWallTile{{NodeId: "node-a", CameraId: 1}}})
	if _, err := svc.Save(context.Background(), SaveFleetWallRequest{
		Name: "front desk", Grid: "2x2", // same name, different case
		Tiles: []FleetWallTile{{NodeId: "node-a", CameraId: 2}},
	}, 1, "sam"); err == nil {
		t.Fatal("two walls can be called the same thing, which makes one of them unchoosable")
	}
}

// Editing must not turn an edit into a second wall, and must not lose who made it.
func TestEditingAWallKeepsItsIdentity(t *testing.T) {
	svc, repo := wallRig(onlineNode("node-a", "A"))
	first := saveWall(t, svc, SaveFleetWallRequest{Name: "Shift", Grid: "2x2",
		Tiles: []FleetWallTile{{NodeId: "node-a", CameraId: 1}}})

	second := saveWall(t, svc, SaveFleetWallRequest{Id: first.Id, Name: "Shift", Grid: "3x3",
		Tiles: []FleetWallTile{{NodeId: "node-a", CameraId: 1}, {NodeId: "node-a", CameraId: 2}}})

	if len(repo.rows) != 1 {
		t.Fatalf("editing made a second wall: %d rows", len(repo.rows))
	}
	if second.Grid != "3x3" || len(second.TileList) != 2 {
		t.Fatalf("the edit did not take: %+v", second.FleetWall)
	}
	if second.CreatedName != "sam" || second.CreatedAt != first.CreatedAt {
		t.Fatalf("who built the wall and when was overwritten by the edit: %+v", second.FleetWall)
	}
}

// The encoding is the only thing that knows how a tile list is stored, so it is the only thing
// that has to survive junk without turning it into a tile pointing somewhere.
func TestTheTileEncodingSurvivesJunk(t *testing.T) {
	cases := map[string]int{
		"":                          0,
		"node-a:1":                  1,
		"node-a:1,node-b:2":         2,
		"node-a:1,,node-b:2":        2,
		" node-a:1 , node-b:2 ":     2,
		"node-a:":                   0,
		":5":                        0,
		"node-a:0":                  0,
		"node-a:-2":                 0,
		"node-a:notanumber":         0,
		"garbage":                   0,
		"node-a:1,garbage,node-b:2": 2,
	}
	for raw, want := range cases {
		if got := len(decodeTiles(raw)); got != want {
			t.Fatalf("decodeTiles(%q) produced %d tiles, want %d", raw, got, want)
		}
	}
	round := []FleetWallTile{{NodeId: "node-a", CameraId: 1}, {NodeId: "node-b", CameraId: 22}}
	if got := decodeTiles(encodeTiles(round)); len(got) != 2 ||
		got[0].NodeId != "node-a" || got[0].CameraId != 1 ||
		got[1].NodeId != "node-b" || got[1].CameraId != 22 {
		t.Fatalf("a tile list did not survive the round trip: %+v", got)
	}
}

// A fleet wall relays every tile across the tunnel from a different machine. The limit is
// lower than the appliance's own for that reason, and a wall nobody can watch is worse than
// one that refuses to be built.
func TestAWallTooBigToWatchIsRefused(t *testing.T) {
	svc, _ := wallRig(onlineNode("node-a", "A"))
	tiles := make([]FleetWallTile, 0, fleetWallMaxTiles+1)
	for i := 1; i <= fleetWallMaxTiles+1; i++ {
		tiles = append(tiles, FleetWallTile{NodeId: "node-a", CameraId: int64(i)})
	}
	if _, err := svc.Save(context.Background(), SaveFleetWallRequest{
		Name: "Everything", Grid: "4x4", Tiles: tiles}, 1, "sam"); err == nil {
		t.Fatalf("a wall of %d relayed tiles was accepted", len(tiles))
	}
	if _, err := svc.Save(context.Background(), SaveFleetWallRequest{
		Name: "Everything", Grid: "4x4", Tiles: tiles[:fleetWallMaxTiles]}, 1, "sam"); err != nil {
		t.Fatalf("a wall at the limit was refused: %v", err)
	}
}
