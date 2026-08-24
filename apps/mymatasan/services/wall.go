package services

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// Video walls: named, shared arrangements of cameras (W3-3b).
//
// Live View already had a grid and a set of tiles, remembered in a COOKIE. Everything this
// file adds comes from that one word: a cookie is a per-browser preference, and a wall is
// how a control room is arranged. It could not be handed to the next shift, could not be
// opened on a second monitor without being rebuilt by hand, and did not survive somebody
// clearing their browser.
//
// The three behaviours on top of "remember it properly" are what a guard station actually
// needs: CYCLE through more cameras than fit the grid, POP an alerting camera onto the
// visible page, and open the same wall on ANOTHER MONITOR. All three are properties of the
// wall rather than of the browser showing it, which is why they are columns here.

// wallGrids is the set of layouts a wall may use.
//
// It is duplicated from the SPA's own list (views/lib/constants.js `liveViewLayouts`), and
// that is a deliberate, narrow duplication: the server has to refuse a grid it cannot
// describe rather than store whatever a client sends and render an empty screen. The failure
// mode of drift is loud — a 400 naming the grids that exist — which is the right direction.
var wallGrids = []string{"1x1", "2x2", "3x2", "3x3", "4x3", "4x4"}

// wallMaxCameras bounds one wall. Not a storage limit: sixty-four live tiles is already
// more than any browser decodes comfortably, and a wall built by selecting "all" on an
// estate of three hundred cameras is a page that never finishes loading.
const wallMaxCameras = 64

// Cycle and auto-pop bounds. A one-second cycle is a strobe, not a rotation; a three-second
// pop is gone before anybody looks up.
const (
	wallMinCycleSeconds   = 3
	wallMaxCycleSeconds   = 600
	wallMinAutoPopSeconds = 5
	wallMaxAutoPopSeconds = 300
)

// WallView is a wall as the screen needs it: the stored row, plus the two facts the row
// cannot carry.
type WallView struct {
	*entities.WallLayout
	// CameraIds is Cameras parsed, in order, so no caller has to agree with this file about
	// how the list is encoded.
	CameraIds []int64 `json:"cameraIds"`
	// MissingCameras are ids in the wall that no longer name a camera.
	//
	// They are REPORTED, not silently dropped. A wall that quietly renders five tiles when
	// it was built with six tells an operator they are watching everything, and the one
	// camera that was removed is the one nobody is watching. The screen says so and the
	// wall can be re-saved to forget them.
	MissingCameras []int64 `json:"missingCameras"`
}

// WallSave is a create or an update.
type WallSave struct {
	Id             int64
	Name           string
	Grid           string
	CameraIds      []int64
	CycleSeconds   int
	AutoPopSeconds int
	IsDefault      bool
	Actor          CaseActor
}

// IWallService is the video wall's read/write surface.
type IWallService interface {
	List(ctx context.Context) ([]WallView, error)
	Get(ctx context.Context, id int64) (*WallView, error)
	Save(ctx context.Context, req WallSave) (*WallView, error)
	Delete(ctx context.Context, id int64) error
	// Grids reports the layouts a wall may use, so a client can offer exactly what the
	// server will accept instead of discovering the answer through a 400.
	Grids() []string
}

// wallCameraSource is the slice of ICameraService this needs: which camera ids exist.
// Declared at the consumer, so a fake in a test stubs one method rather than forty.
type wallCameraSource interface {
	Get(ctx context.Context, limit uint64, offset uint64) ([]*CameraDetail, uint64, error)
}

type wallService struct {
	repo    dbsql.IGenericRepo[entities.WallLayout]
	cameras wallCameraSource
	now     func() int64
}

func NewWallService(repo dbsql.IGenericRepo[entities.WallLayout], cameras wallCameraSource) IWallService {
	return &wallService{repo: repo, cameras: cameras, now: func() int64 { return time.Now().UTC().Unix() }}
}

func (s *wallService) Grids() []string { return append([]string(nil), wallGrids...) }

func (s *wallService) List(ctx context.Context) ([]WallView, error) {
	rows, _, err := s.repo.Get(ctx, "", 200, 0, nil,
		[]sqldataenums.Sorter{{FieldName: "Name", Sort: sqldataenums.ASC}})
	if err != nil {
		return nil, err
	}
	known := s.knownCameras(ctx)
	out := make([]WallView, 0, len(rows))
	for _, row := range rows {
		if row == nil {
			continue
		}
		out = append(out, s.view(row, known))
	}
	return out, nil
}

func (s *wallService) Get(ctx context.Context, id int64) (*WallView, error) {
	row, err := s.get(ctx, id)
	if err != nil {
		return nil, err
	}
	view := s.view(row, s.knownCameras(ctx))
	return &view, nil
}

func (s *wallService) Save(ctx context.Context, req WallSave) (*WallView, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, errors.New("a wall needs a name")
	}
	grid := strings.TrimSpace(req.Grid)
	if !s.knownGrid(grid) {
		return nil, fmt.Errorf("unknown grid %q — use one of %s", req.Grid, strings.Join(wallGrids, ", "))
	}
	if len(req.CameraIds) == 0 {
		return nil, errors.New("a wall needs at least one camera")
	}
	if len(req.CameraIds) > wallMaxCameras {
		return nil, fmt.Errorf("a wall holds at most %d cameras", wallMaxCameras)
	}
	if req.CycleSeconds != 0 && (req.CycleSeconds < wallMinCycleSeconds || req.CycleSeconds > wallMaxCycleSeconds) {
		return nil, fmt.Errorf("cycle every %d to %d seconds, or 0 to leave the wall still",
			wallMinCycleSeconds, wallMaxCycleSeconds)
	}
	if req.AutoPopSeconds != 0 && (req.AutoPopSeconds < wallMinAutoPopSeconds || req.AutoPopSeconds > wallMaxAutoPopSeconds) {
		return nil, fmt.Errorf("hold a popped camera for %d to %d seconds, or 0 never to pop one",
			wallMinAutoPopSeconds, wallMaxAutoPopSeconds)
	}
	// Duplicates are dropped rather than refused: the same camera twice on one wall is a
	// mis-click, not an intent, and refusing the save loses the rest of the arrangement.
	ids := make([]int64, 0, len(req.CameraIds))
	seen := map[int64]bool{}
	for _, id := range req.CameraIds {
		if id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil, errors.New("a wall needs at least one camera")
	}
	if err := s.nameIsFree(ctx, name, req.Id); err != nil {
		return nil, err
	}

	now := s.now()
	row := entities.WallLayout{
		Id: req.Id, Name: name, Grid: grid, Cameras: encodeCameraIds(ids),
		CycleSeconds: req.CycleSeconds, AutoPopSeconds: req.AutoPopSeconds,
		IsDefault: req.IsDefault, UpdatedAt: now,
	}
	if req.Id > 0 {
		existing, err := s.get(ctx, req.Id)
		if err != nil {
			return nil, err
		}
		row.CreatedBy, row.CreatedName, row.CreatedAt = existing.CreatedBy, existing.CreatedName, existing.CreatedAt
		if _, err := s.repo.UpdateById(ctx, "", row); err != nil {
			return nil, err
		}
	} else {
		row.CreatedBy, row.CreatedName, row.CreatedAt = req.Actor.Id, req.Actor.Name, now
		id, err := s.repo.Create(ctx, "", row)
		if err != nil {
			return nil, err
		}
		row.Id = int64(id)
	}
	if row.IsDefault {
		// One default, enforced after the write so a failure leaves the wall saved rather
		// than half-applied. "The default" with two answers is a screen that opens
		// differently depending on which row the database hands back first.
		if err := s.clearOtherDefaults(ctx, row.Id); err != nil {
			return nil, err
		}
	}
	view := s.view(&row, s.knownCameras(ctx))
	return &view, nil
}

func (s *wallService) Delete(ctx context.Context, id int64) error {
	if _, err := s.get(ctx, id); err != nil {
		return err
	}
	_, err := s.repo.DeleteById(ctx, "", uint64(id))
	return err
}

// --- internals ----------------------------------------------------------------

func (s *wallService) get(ctx context.Context, id int64) (*entities.WallLayout, error) {
	if id <= 0 {
		return nil, errors.New("a wall id is required")
	}
	row, err := s.repo.GetById(ctx, "", uint64(id))
	if err != nil {
		if isNoResultFoundErr(err) {
			return nil, errors.New("no such wall")
		}
		return nil, err
	}
	if row == nil {
		return nil, errors.New("no such wall")
	}
	return row, nil
}

func (s *wallService) knownGrid(grid string) bool {
	for _, g := range wallGrids {
		if g == grid {
			return true
		}
	}
	return false
}

// nameIsFree refuses a name another wall already holds. Compared case-insensitively:
// "Perimeter" and "perimeter" on the same picker are two rows nobody can tell apart.
func (s *wallService) nameIsFree(ctx context.Context, name string, selfId int64) error {
	rows, _, err := s.repo.Get(ctx, "", 200, 0, nil, nil)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if row == nil || row.Id == selfId {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(row.Name), name) {
			return fmt.Errorf("there is already a wall called %q", row.Name)
		}
	}
	return nil
}

func (s *wallService) clearOtherDefaults(ctx context.Context, keepId int64) error {
	rows, _, err := s.repo.Get(ctx, "", 200, 0,
		[]sqldataenums.Filter{{FieldName: "IsDefault", Compare: sqldataenums.Equal, Value: true}}, nil)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if row == nil || row.Id == keepId || !row.IsDefault {
			continue
		}
		row.IsDefault = false
		row.UpdatedAt = s.now()
		if _, err := s.repo.UpdateById(ctx, "", *row); err != nil {
			return err
		}
	}
	return nil
}

// knownCameras is the set of camera ids that still exist. nil when the camera list could
// not be read, which makes missing-camera reporting fail QUIET rather than wrong: claiming
// every camera on a wall has been deleted because one query failed would be worse than
// saying nothing.
func (s *wallService) knownCameras(ctx context.Context) map[int64]bool {
	if s.cameras == nil {
		return nil
	}
	rows, _, err := s.cameras.Get(ctx, 500, 0)
	if err != nil {
		return nil
	}
	out := make(map[int64]bool, len(rows))
	for _, cam := range rows {
		if cam != nil {
			out[cam.Id] = true
		}
	}
	return out
}

func (s *wallService) view(row *entities.WallLayout, known map[int64]bool) WallView {
	ids := decodeCameraIds(row.Cameras)
	view := WallView{WallLayout: row, CameraIds: ids, MissingCameras: []int64{}}
	if known == nil {
		return view
	}
	for _, id := range ids {
		if !known[id] {
			view.MissingCameras = append(view.MissingCameras, id)
		}
	}
	sort.Slice(view.MissingCameras, func(i, j int) bool {
		return view.MissingCameras[i] < view.MissingCameras[j]
	})
	return view
}

func encodeCameraIds(ids []int64) string {
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, strconv.FormatInt(id, 10))
	}
	return strings.Join(parts, ",")
}

func decodeCameraIds(raw string) []int64 {
	out := []int64{}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if id, err := strconv.ParseInt(part, 10, 64); err == nil && id > 0 {
			out = append(out, id)
		}
	}
	return out
}
