package services

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	// Registers PNG/JPEG/GIF decoders for image.DecodeConfig (dimensions at upload).
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/mysayasan/kopiv2/apps/myseliasan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	"github.com/mysayasan/kopiv2/infra/atrest"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

var (
	ErrSiteUnknown  = errors.New("site not found")
	ErrFloorUnknown = errors.New("floor plan not found")
	ErrBadImage     = errors.New("unsupported or unreadable image")
)

// FloorImage is a decrypted plan image ready to serve.
type FloorImage struct {
	Data        []byte
	ContentType string
}

// NodeFloorplan pairs a floor plan with one node's placements on it.
type NodeFloorplan struct {
	Floor      *entities.FloorPlan       `json:"floor"`
	Placements []*entities.NodePlacement `json:"placements"`
}

// ISiteService manages sites and their floor-plan images. Plan image bytes are encrypted
// at rest with the fleet cipher; only metadata + pixel dimensions live in the database.
type ISiteService interface {
	ListSites(ctx context.Context) ([]*entities.Site, error)
	CreateSite(ctx context.Context, name, description string, by int64) (*entities.Site, error)
	UpdateSite(ctx context.Context, id int64, name, description string, ordinal int, by int64) (*entities.Site, error)
	DeleteSite(ctx context.Context, id int64) error

	ListFloors(ctx context.Context, siteID int64) ([]*entities.FloorPlan, error)
	// AddFloor decodes the image (for its pixel dimensions), encrypts the bytes to disk, and
	// records the floor plan. contentType must be image/png|jpeg|gif. design is the drawn-plan
	// vector JSON ("" for an uploaded image).
	AddFloor(ctx context.Context, siteID int64, name string, img []byte, contentType, design string, by int64) (*entities.FloorPlan, error)
	// ReplaceFloorImage rewrites an existing floor's image bytes, dimensions, name and design —
	// used when re-saving a drawn plan from the designer.
	ReplaceFloorImage(ctx context.Context, id int64, name string, img []byte, contentType, design string, by int64) (*entities.FloorPlan, error)
	UpdateFloor(ctx context.Context, id int64, name string, ordinal int, by int64) (*entities.FloorPlan, error)
	DeleteFloor(ctx context.Context, id int64) error
	// FloorImage decrypts and returns a plan image for serving.
	FloorImage(ctx context.Context, id int64) (*FloorImage, error)
	// FloorBackground decrypts and returns the pristine background image (for re-editing a plan).
	FloorBackground(ctx context.Context, id int64) (*FloorImage, error)
	GetFloor(ctx context.Context, id int64) (*entities.FloorPlan, error)

	// Placements pin nodes/cameras onto a floor plan. They are myseliasan-owned so they
	// survive the node being offline (the live camera list does not).
	ListPlacements(ctx context.Context, floorID int64) ([]*entities.NodePlacement, error)
	// NodeFloorplans returns the floor plan(s) that carry a given node's placements, each with
	// the node's markers on it — so clicking the node on the geo map can drill into its plan.
	// Ordered by marker count (the plan with the most of this node's cameras first).
	NodeFloorplans(ctx context.Context, nodeID string) ([]NodeFloorplan, error)
	AddPlacement(ctx context.Context, floorID int64, nodeID, cameraID, lastKnownName string, x, y float64, by int64) (*entities.NodePlacement, error)
	// UpdatePlacement moves and/or re-orients a placement; any nil field is left unchanged, so a
	// drag sends x/y while the FOV editor sends heading/fov.
	UpdatePlacement(ctx context.Context, id int64, x, y, heading, fov *float64, by int64) (*entities.NodePlacement, error)
	DeletePlacement(ctx context.Context, id int64) error
}

type siteService struct {
	sites      dbsql.IGenericRepo[entities.Site]
	floors     dbsql.IGenericRepo[entities.FloorPlan]
	placements dbsql.IGenericRepo[entities.NodePlacement]
	cipher     *atrest.Cipher // may be nil (encryption disabled)
	dir        string         // absolute directory for encrypted plan images
}

// NewSiteService builds the sites/floors service. planDir is where encrypted plan images
// are written; cipher may be nil when at-rest encryption is disabled.
func NewSiteService(db dbsql.IDbCrud, cipher *atrest.Cipher, planDir string) ISiteService {
	return &siteService{
		sites:      dbsql.NewGenericRepo[entities.Site](db),
		floors:     dbsql.NewGenericRepo[entities.FloorPlan](db),
		placements: dbsql.NewGenericRepo[entities.NodePlacement](db),
		cipher:     cipher,
		dir:        planDir,
	}
}

func (s *siteService) ListPlacements(ctx context.Context, floorID int64) ([]*entities.NodePlacement, error) {
	// Get (not GetByForeign) — GetByForeign returns only one row (see ListFloors note).
	rows, _, err := s.placements.Get(ctx, "", 2000, 0,
		[]sqldataenums.Filter{{FieldName: "FloorId", Compare: sqldataenums.Equal, Value: floorID}}, nil)
	if err != nil {
		if isNoResultFoundErr(err) {
			return []*entities.NodePlacement{}, nil
		}
		return nil, err
	}
	return rows, nil
}

// NodeFloorplans finds the floor plan(s) that hold this node's placements. See ISiteService.
func (s *siteService) NodeFloorplans(ctx context.Context, nodeID string) ([]NodeFloorplan, error) {
	rows, _, err := s.placements.Get(ctx, "", 2000, 0,
		[]sqldataenums.Filter{{FieldName: "NodeId", Compare: sqldataenums.Equal, Value: nodeID}}, nil)
	if err != nil {
		if isNoResultFoundErr(err) {
			return []NodeFloorplan{}, nil
		}
		return nil, err
	}
	// Group the node's placements by floor, preserving each floor's marker list.
	byFloor := map[int64][]*entities.NodePlacement{}
	order := []int64{}
	for _, p := range rows {
		if _, seen := byFloor[p.FloorId]; !seen {
			order = append(order, p.FloorId)
		}
		byFloor[p.FloorId] = append(byFloor[p.FloorId], p)
	}
	out := make([]NodeFloorplan, 0, len(order))
	for _, fid := range order {
		floor, ferr := s.floors.GetById(ctx, "", uint64(fid))
		if ferr != nil || floor == nil {
			continue // floor was deleted out from under the placements — skip it
		}
		out = append(out, NodeFloorplan{Floor: floor, Placements: byFloor[fid]})
	}
	// Most-populated plan first (the building where most of this node's cameras live).
	sort.Slice(out, func(i, j int) bool { return len(out[i].Placements) > len(out[j].Placements) })
	return out, nil
}

// floorPlacements is the internal list used by cascade deletes (all placements on a floor).
func (s *siteService) floorPlacements(ctx context.Context, floorID int64) []*entities.NodePlacement {
	rows, _, err := s.placements.Get(ctx, "", 2000, 0,
		[]sqldataenums.Filter{{FieldName: "FloorId", Compare: sqldataenums.Equal, Value: floorID}}, nil)
	if err != nil {
		return nil
	}
	return rows
}

func (s *siteService) AddPlacement(ctx context.Context, floorID int64, nodeID, cameraID, lastKnownName string, x, y float64, by int64) (*entities.NodePlacement, error) {
	if _, err := s.floors.GetById(ctx, "", uint64(floorID)); err != nil {
		return nil, ErrFloorUnknown
	}
	now := time.Now().Unix()
	// A camera gets a default coverage arc on drop (the operator then aims it); a node/sensor
	// marker (no cameraId) stays a plain point with no arc.
	fov := 0.0
	if cameraID != "" {
		fov = 70
	}
	row := entities.NodePlacement{
		FloorId: floorID, NodeId: nodeID, CameraId: cameraID, LastKnownName: lastKnownName,
		X: x, Y: y, Heading: 0, Fov: fov, CreatedBy: by, CreatedAt: now, UpdatedBy: by, UpdatedAt: now,
	}
	id, err := s.placements.Create(ctx, "", row)
	if err != nil {
		return nil, err
	}
	row.Id = int64(id)
	return &row, nil
}

func (s *siteService) UpdatePlacement(ctx context.Context, id int64, x, y, heading, fov *float64, by int64) (*entities.NodePlacement, error) {
	row, err := s.placements.GetById(ctx, "", uint64(id))
	if err != nil || row == nil {
		return nil, errors.New("placement not found")
	}
	if x != nil {
		row.X = *x
	}
	if y != nil {
		row.Y = *y
	}
	if heading != nil {
		row.Heading = *heading
	}
	if fov != nil {
		row.Fov = *fov
	}
	row.UpdatedBy = by
	row.UpdatedAt = time.Now().Unix()
	if _, err := s.placements.UpdateById(ctx, "", *row); err != nil {
		return nil, err
	}
	return row, nil
}

func (s *siteService) DeletePlacement(ctx context.Context, id int64) error {
	_, err := s.placements.DeleteById(ctx, "", uint64(id))
	return err
}

func (s *siteService) ListSites(ctx context.Context) ([]*entities.Site, error) {
	rows, _, err := s.sites.Get(ctx, "", 1000, 0, nil, []sqldataenums.Sorter{
		{FieldName: "Ordinal", Sort: sqldataenums.ASC}, {FieldName: "Id", Sort: sqldataenums.ASC},
	})
	if err != nil {
		if isNoResultFoundErr(err) {
			return []*entities.Site{}, nil
		}
		return nil, err
	}
	return rows, nil
}

func (s *siteService) CreateSite(ctx context.Context, name, description string, by int64) (*entities.Site, error) {
	now := time.Now().Unix()
	row := entities.Site{Name: name, Description: description, CreatedBy: by, CreatedAt: now, UpdatedBy: by, UpdatedAt: now}
	id, err := s.sites.Create(ctx, "", row)
	if err != nil {
		return nil, err
	}
	row.Id = int64(id)
	return &row, nil
}

func (s *siteService) UpdateSite(ctx context.Context, id int64, name, description string, ordinal int, by int64) (*entities.Site, error) {
	row, err := s.sites.GetById(ctx, "", uint64(id))
	if err != nil || row == nil {
		return nil, ErrSiteUnknown
	}
	row.Name = name
	row.Description = description
	row.Ordinal = ordinal
	row.UpdatedBy = by
	row.UpdatedAt = time.Now().Unix()
	if _, err := s.sites.UpdateById(ctx, "", *row); err != nil {
		return nil, err
	}
	return row, nil
}

// DeleteSite removes a site and all its floor plans (and their encrypted images). Node/camera
// placements that referenced those floors are orphaned by id; the placement service tolerates
// a missing floor, so this does not cascade there.
func (s *siteService) DeleteSite(ctx context.Context, id int64) error {
	floors, err := s.ListFloors(ctx, id)
	if err != nil {
		return err
	}
	for _, f := range floors {
		if derr := s.DeleteFloor(ctx, f.Id); derr != nil {
			return derr
		}
	}
	_, err = s.sites.DeleteById(ctx, "", uint64(id))
	return err
}

func (s *siteService) ListFloors(ctx context.Context, siteID int64) ([]*entities.FloorPlan, error) {
	// NOTE: GetByForeign hardcodes limit=1 in the shared sqlite layer (SelectByForeign), so it
	// returns only ONE child. For a real one-to-many list we must use Get with an explicit
	// filter on the foreign column.
	rows, _, err := s.floors.Get(ctx, "", 1000, 0,
		[]sqldataenums.Filter{{FieldName: "SiteId", Compare: sqldataenums.Equal, Value: siteID}},
		[]sqldataenums.Sorter{{FieldName: "Ordinal", Sort: sqldataenums.ASC}, {FieldName: "Id", Sort: sqldataenums.ASC}})
	if err != nil {
		if isNoResultFoundErr(err) {
			return []*entities.FloorPlan{}, nil
		}
		return nil, err
	}
	return rows, nil
}

func (s *siteService) GetFloor(ctx context.Context, id int64) (*entities.FloorPlan, error) {
	row, err := s.floors.GetById(ctx, "", uint64(id))
	if err != nil || row == nil {
		return nil, ErrFloorUnknown
	}
	return row, nil
}

func (s *siteService) AddFloor(ctx context.Context, siteID int64, name string, img []byte, contentType, design string, by int64) (*entities.FloorPlan, error) {
	if _, err := s.sites.GetById(ctx, "", uint64(siteID)); err != nil {
		return nil, ErrSiteUnknown
	}
	// Decode just the header for dimensions — these become the OL pixel-projection extent.
	cfg, _, err := image.DecodeConfig(bytes.NewReader(img))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
		return nil, ErrBadImage
	}

	now := time.Now().Unix()
	row := entities.FloorPlan{
		SiteId:      siteID,
		Name:        name,
		ContentType: contentType,
		Width:       cfg.Width,
		Height:      cfg.Height,
		Design:      design,
		CreatedBy:   by, CreatedAt: now, UpdatedBy: by, UpdatedAt: now,
	}
	// Create first to obtain the id, which names the on-disk file.
	id, err := s.floors.Create(ctx, "", row)
	if err != nil {
		return nil, err
	}
	row.Id = int64(id)

	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		_, _ = s.floors.DeleteById(ctx, "", id)
		return nil, err
	}
	path := filepath.Join(s.dir, fmt.Sprintf("floor-%d.img", id))
	payload := img
	if s.cipher != nil {
		enc, encErr := s.cipher.EncryptBytes(img)
		if encErr != nil {
			_, _ = s.floors.DeleteById(ctx, "", id)
			return nil, encErr
		}
		payload = enc
	}
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		_, _ = s.floors.DeleteById(ctx, "", id)
		return nil, err
	}
	row.ImagePath = path
	// For an UPLOADED image (no design), keep a pristine copy as the editable background so the
	// designer can later annotate on top of the original photo rather than a re-composited one.
	if design == "" {
		bgPath := filepath.Join(s.dir, fmt.Sprintf("floor-%d.bg.img", id))
		if werr := os.WriteFile(bgPath, payload, 0o644); werr == nil {
			row.BgPath = bgPath
		}
	}
	if _, err := s.floors.UpdateById(ctx, "", row); err != nil {
		s.removeImage(path)
		s.removeImage(row.BgPath)
		_, _ = s.floors.DeleteById(ctx, "", id)
		return nil, err
	}
	return &row, nil
}

// ReplaceFloorImage rewrites an existing floor's rasterised image + design (drawn-plan re-save).
func (s *siteService) ReplaceFloorImage(ctx context.Context, id int64, name string, img []byte, contentType, design string, by int64) (*entities.FloorPlan, error) {
	row, err := s.floors.GetById(ctx, "", uint64(id))
	if err != nil || row == nil {
		return nil, ErrFloorUnknown
	}
	cfg, _, derr := image.DecodeConfig(bytes.NewReader(img))
	if derr != nil || cfg.Width <= 0 || cfg.Height <= 0 {
		return nil, ErrBadImage
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(s.dir, fmt.Sprintf("floor-%d.img", id))
	// First-time annotation of a plain UPLOADED image (had no design and no stored background):
	// preserve the CURRENT image as the editable background BEFORE we overwrite it with the
	// composite, so future edits draw on the original photo, not on the annotated render.
	if row.BgPath == "" && row.Design == "" && design != "" {
		if raw, rerr := os.ReadFile(row.ImagePath); rerr == nil {
			bgPath := filepath.Join(s.dir, fmt.Sprintf("floor-%d.bg.img", id))
			if werr := os.WriteFile(bgPath, raw, 0o644); werr == nil {
				row.BgPath = bgPath
			}
		}
	}
	payload := img
	if s.cipher != nil {
		enc, encErr := s.cipher.EncryptBytes(img)
		if encErr != nil {
			return nil, encErr
		}
		payload = enc
	}
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		return nil, err
	}
	if name != "" {
		row.Name = name
	}
	row.ImagePath = path
	row.ContentType = contentType
	row.Width = cfg.Width
	row.Height = cfg.Height
	row.Design = design
	row.UpdatedBy = by
	row.UpdatedAt = time.Now().Unix()
	if _, err := s.floors.UpdateById(ctx, "", *row); err != nil {
		return nil, err
	}
	return row, nil
}

func (s *siteService) UpdateFloor(ctx context.Context, id int64, name string, ordinal int, by int64) (*entities.FloorPlan, error) {
	row, err := s.floors.GetById(ctx, "", uint64(id))
	if err != nil || row == nil {
		return nil, ErrFloorUnknown
	}
	row.Name = name
	row.Ordinal = ordinal
	row.UpdatedBy = by
	row.UpdatedAt = time.Now().Unix()
	if _, err := s.floors.UpdateById(ctx, "", *row); err != nil {
		return nil, err
	}
	return row, nil
}

func (s *siteService) DeleteFloor(ctx context.Context, id int64) error {
	row, err := s.floors.GetById(ctx, "", uint64(id))
	if err != nil || row == nil {
		return nil // already gone
	}
	// Remove the floor's placements too, so a deleted plan leaves no orphaned pins.
	for _, p := range s.floorPlacements(ctx, id) {
		_, _ = s.placements.DeleteById(ctx, "", uint64(p.Id))
	}
	s.removeImage(row.ImagePath)
	s.removeImage(row.BgPath)
	_, err = s.floors.DeleteById(ctx, "", uint64(id))
	return err
}

func (s *siteService) FloorImage(ctx context.Context, id int64) (*FloorImage, error) {
	row, err := s.floors.GetById(ctx, "", uint64(id))
	if err != nil || row == nil {
		return nil, ErrFloorUnknown
	}
	raw, err := os.ReadFile(row.ImagePath)
	if err != nil {
		return nil, ErrFloorUnknown
	}
	data := raw
	if s.cipher != nil {
		dec, decErr := s.cipher.DecryptBytes(raw)
		if decErr != nil {
			return nil, decErr
		}
		data = dec
	}
	return &FloorImage{Data: data, ContentType: row.ContentType}, nil
}

// FloorBackground returns the pristine background image (uploaded photo) for the designer to draw
// on. Returns ErrFloorUnknown when the plan was drawn on a blank canvas (no background).
func (s *siteService) FloorBackground(ctx context.Context, id int64) (*FloorImage, error) {
	row, err := s.floors.GetById(ctx, "", uint64(id))
	if err != nil || row == nil {
		return nil, ErrFloorUnknown
	}
	if row.BgPath == "" {
		return nil, ErrFloorUnknown
	}
	raw, err := os.ReadFile(row.BgPath)
	if err != nil {
		return nil, ErrFloorUnknown
	}
	data := raw
	if s.cipher != nil {
		dec, decErr := s.cipher.DecryptBytes(raw)
		if decErr != nil {
			return nil, decErr
		}
		data = dec
	}
	return &FloorImage{Data: data, ContentType: row.ContentType}, nil
}

func (s *siteService) removeImage(path string) {
	if path == "" {
		return
	}
	_ = os.Remove(path)
}
