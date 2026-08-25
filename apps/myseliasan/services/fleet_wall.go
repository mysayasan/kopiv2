package services

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/mysayasan/kopiv2/apps/myseliasan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// Fleet video walls: one screen, several appliances.
//
// mymatasan's own wall (W3-3b) arranges cameras that live on one recorder. That is right for a
// recorder and it is not what a control room watches. A guard station covers four buildings,
// which is four appliances, and the arrangement it needs is the one no appliance can hold —
// because no appliance can see the other three.
//
// The tile is therefore (appliance, camera) rather than (camera), and everything else about
// this file is the consequence of that one change.

// fleetWallGrids is the set of layouts a wall may use. Duplicated from the SPA's own list, and
// that duplication is deliberate and narrow: the server has to refuse a grid it cannot
// describe rather than store whatever a client sends and render an empty screen. Drift fails
// loudly, with a message naming the grids that exist.
var fleetWallGrids = []string{"1x1", "2x2", "3x2", "3x3", "4x3", "4x4"}

// fleetWallMaxTiles bounds one wall.
//
// LOWER THAN THE APPLIANCE'S OWN LIMIT (64), on purpose. Every tile here is a relayed stream
// crossing the control plane's tunnel from a different machine, not a local decode, and a wall
// that cannot be watched is worse than one that refuses to be built.
const fleetWallMaxTiles = 32

// Cycle and auto-pop bounds. A one-second cycle is a strobe, not a rotation; a three-second
// pop is gone before anybody looks up. Same numbers as the appliance's wall, because an
// operator moving between the two screens should not have to learn two sets of limits.
const (
	fleetWallMinCycleSeconds   = 3
	fleetWallMaxCycleSeconds   = 600
	fleetWallMinAutoPopSeconds = 5
	fleetWallMaxAutoPopSeconds = 300
)

// FleetWallTile is one tile: an appliance and a camera on it, with the two facts the stored
// row cannot carry.
type FleetWallTile struct {
	NodeId   string `json:"nodeId"`
	CameraId int64  `json:"cameraId"`
	// NodeName and NodeStatus come from the fleet registry. Status matters MORE here than a
	// name does: a tile whose appliance is offline will never show a picture, and a wall
	// that renders it as a black rectangle indistinguishable from a dark room is a wall that
	// lies at exactly the moment somebody is relying on it.
	NodeName   string `json:"nodeName"`
	NodeStatus string `json:"nodeStatus"`
	// NodeKnown is false when the tile names an appliance that is no longer in this fleet —
	// released, or never adopted. Different from "offline", and it needs a different answer.
	NodeKnown bool `json:"nodeKnown"`
}

// FleetWallView is a wall as the screen needs it.
type FleetWallView struct {
	*entities.FleetWall
	// Tiles is the parsed, resolved tile list in order, so no caller has to agree with this
	// file about how the list is encoded.
	TileList []FleetWallTile `json:"tileList"`
	// OfflineTiles / UnknownTiles are the counts a list screen leads with, so "this wall has
	// a hole in it" is answerable without opening it.
	OfflineTiles int `json:"offlineTiles"`
	UnknownTiles int `json:"unknownTiles"`
}

// SaveFleetWallRequest creates or updates a wall.
type SaveFleetWallRequest struct {
	Id             int64           `json:"id"`
	Name           string          `json:"name"`
	Grid           string          `json:"grid"`
	Tiles          []FleetWallTile `json:"tiles"`
	CycleSeconds   int             `json:"cycleSeconds"`
	AutoPopSeconds int             `json:"autoPopSeconds"`
	IsDefault      bool            `json:"isDefault"`
}

// IFleetWallService owns the fleet's saved wall arrangements.
type IFleetWallService interface {
	List(ctx context.Context) ([]FleetWallView, error)
	Get(ctx context.Context, id int64) (*FleetWallView, error)
	Save(ctx context.Context, req SaveFleetWallRequest, actor int64, actorName string) (*FleetWallView, error)
	Delete(ctx context.Context, id int64, actor int64) error
	// Grids reports the layouts a wall may use, so a client can offer exactly what the
	// server will accept instead of discovering the answer through a 400.
	Grids() []string
}

// ErrFleetWallNotFound is returned for an id that names no wall.
var ErrFleetWallNotFound = errors.New("no such wall")

type fleetWallService struct {
	walls dbsql.IGenericRepo[entities.FleetWall]
	nodes PolicyNodeLister
	audit IAuditService
}

// NewFleetWallService builds the service. nodes and audit may be nil (tests).
//
// PolicyNodeLister is reused rather than taking the whole registry, for the same reason the
// policy reconciler narrowed it: a wall service holding INodeRegistry is one refactor away
// from being able to release an appliance.
func NewFleetWallService(db dbsql.IDbCrud, nodes PolicyNodeLister, audit IAuditService) IFleetWallService {
	return &fleetWallService{
		walls: dbsql.NewGenericRepo[entities.FleetWall](db),
		nodes: nodes,
		audit: audit,
	}
}

func newFleetWallServiceWith(walls dbsql.IGenericRepo[entities.FleetWall], nodes PolicyNodeLister, audit IAuditService) *fleetWallService {
	return &fleetWallService{walls: walls, nodes: nodes, audit: audit}
}

func (s *fleetWallService) Grids() []string { return append([]string(nil), fleetWallGrids...) }

func (s *fleetWallService) List(ctx context.Context) ([]FleetWallView, error) {
	rows, err := s.all(ctx)
	if err != nil {
		return nil, err
	}
	byNode := s.nodeIndex(ctx)
	out := make([]FleetWallView, 0, len(rows))
	for _, row := range rows {
		out = append(out, s.view(row, byNode))
	}
	return out, nil
}

func (s *fleetWallService) Get(ctx context.Context, id int64) (*FleetWallView, error) {
	row, err := s.wall(ctx, id)
	if err != nil {
		return nil, err
	}
	view := s.view(row, s.nodeIndex(ctx))
	return &view, nil
}

func (s *fleetWallService) Save(ctx context.Context, req SaveFleetWallRequest, actor int64, actorName string) (*FleetWallView, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, errors.New("a wall needs a name — it is how somebody chooses it")
	}
	grid := strings.TrimSpace(req.Grid)
	if !validFleetGrid(grid) {
		return nil, fmt.Errorf("%q is not a layout this control plane knows; the layouts are %s",
			grid, strings.Join(fleetWallGrids, ", "))
	}
	if len(req.Tiles) == 0 {
		return nil, errors.New("a wall with no cameras is a blank screen — add at least one")
	}
	if len(req.Tiles) > fleetWallMaxTiles {
		return nil, fmt.Errorf("a fleet wall holds at most %d cameras: every tile here is a "+
			"stream relayed from another machine, and a wall nobody can watch is worse than "+
			"one that refuses to be built", fleetWallMaxTiles)
	}
	if req.CycleSeconds != 0 && (req.CycleSeconds < fleetWallMinCycleSeconds || req.CycleSeconds > fleetWallMaxCycleSeconds) {
		return nil, fmt.Errorf("the rotation must be between %d and %d seconds, or 0 to leave it still",
			fleetWallMinCycleSeconds, fleetWallMaxCycleSeconds)
	}
	if req.AutoPopSeconds != 0 && (req.AutoPopSeconds < fleetWallMinAutoPopSeconds || req.AutoPopSeconds > fleetWallMaxAutoPopSeconds) {
		return nil, fmt.Errorf("an alarm must stay up for between %d and %d seconds, or 0 to never pop one",
			fleetWallMinAutoPopSeconds, fleetWallMaxAutoPopSeconds)
	}

	// THE SAME CAMERA TWICE IS A MISTAKE, NOT AN ARRANGEMENT. It costs a second relayed
	// stream of the same picture and it means one of the two tiles the operator is watching
	// is redundant at the moment they most need the space.
	seen := map[string]bool{}
	for _, tile := range req.Tiles {
		nodeId := strings.TrimSpace(tile.NodeId)
		if nodeId == "" || tile.CameraId <= 0 {
			return nil, errors.New("every tile needs an appliance and a camera on it")
		}
		key := tileKey(nodeId, tile.CameraId)
		if seen[key] {
			return nil, errors.New("that camera is on this wall twice")
		}
		seen[key] = true
	}

	existing, err := s.all(ctx)
	if err != nil {
		return nil, err
	}
	for _, other := range existing {
		if other.Id != req.Id && strings.EqualFold(strings.TrimSpace(other.Name), name) {
			return nil, errors.New("a wall with that name already exists")
		}
	}

	now := time.Now().Unix()
	row := entities.FleetWall{
		Id:             req.Id,
		Name:           name,
		Grid:           grid,
		Tiles:          encodeTiles(req.Tiles),
		CycleSeconds:   req.CycleSeconds,
		AutoPopSeconds: req.AutoPopSeconds,
		IsDefault:      req.IsDefault,
		UpdatedBy:      actor,
		UpdatedAt:      now,
	}
	if req.Id > 0 {
		prev, err := s.wall(ctx, req.Id)
		if err != nil {
			return nil, err
		}
		row.CreatedBy, row.CreatedName, row.CreatedAt = prev.CreatedBy, prev.CreatedName, prev.CreatedAt
		if _, err := s.walls.UpdateById(ctx, "", row); err != nil {
			return nil, err
		}
	} else {
		row.CreatedBy, row.CreatedName, row.CreatedAt = actor, actorName, now
		id, err := s.walls.Create(ctx, "", row)
		if err != nil {
			return nil, err
		}
		row.Id = int64(id)
	}

	// AT MOST ONE DEFAULT. Cleared on the others rather than left to row order, because "the
	// default" with two answers is a screen that opens differently depending on how the
	// database felt about sorting.
	if row.IsDefault {
		for _, other := range existing {
			if other == nil || other.Id == row.Id || !other.IsDefault {
				continue
			}
			other.IsDefault = false
			other.UpdatedAt = now
			if _, err := s.walls.UpdateById(ctx, "", *other); err != nil {
				return nil, err
			}
		}
	}

	s.record(ctx, ActionFleetWallSave, &row, actor,
		fmt.Sprintf("%q now shows %d camera(s) across the fleet", row.Name, len(req.Tiles)))
	view := s.view(&row, s.nodeIndex(ctx))
	return &view, nil
}

func (s *fleetWallService) Delete(ctx context.Context, id int64, actor int64) error {
	row, err := s.wall(ctx, id)
	if err != nil {
		return err
	}
	if _, err := s.walls.DeleteById(ctx, "", uint64(id)); err != nil {
		return err
	}
	s.record(ctx, ActionFleetWallDelete, row, actor, fmt.Sprintf("%q was removed", row.Name))
	return nil
}

// --- plumbing ---------------------------------------------------------------------------

func (s *fleetWallService) all(ctx context.Context) ([]*entities.FleetWall, error) {
	rows, _, err := s.walls.Get(ctx, "", 500, 0, nil,
		[]sqldataenums.Sorter{{FieldName: "Name", Sort: sqldataenums.ASC}})
	if err != nil && !isNoResultErr(err) {
		return nil, err
	}
	out := make([]*entities.FleetWall, 0, len(rows))
	for _, row := range rows {
		if row != nil {
			out = append(out, row)
		}
	}
	return out, nil
}

func (s *fleetWallService) wall(ctx context.Context, id int64) (*entities.FleetWall, error) {
	row, err := s.walls.GetById(ctx, "", uint64(id))
	if err != nil || row == nil {
		// GetById ERRORS on a missing row rather than returning nil, so both shapes mean the
		// same thing here.
		return nil, ErrFleetWallNotFound
	}
	return row, nil
}

func (s *fleetWallService) nodeIndex(ctx context.Context) map[string]*entities.ManagedNode {
	out := map[string]*entities.ManagedNode{}
	if s.nodes == nil {
		return out
	}
	nodes, err := s.nodes.List(ctx)
	if err != nil {
		return out
	}
	for _, node := range nodes {
		if node != nil {
			out[node.NodeId] = node
		}
	}
	return out
}

// view resolves a stored wall for the screen.
//
// A TILE WHOSE APPLIANCE IS GONE IS NOT SILENTLY DROPPED. It keeps its place in the
// arrangement and says what happened to it: an operator who built a wall of sixteen and now
// sees fifteen has been told nothing, and the missing one is the building they will not think
// to check.
func (s *fleetWallService) view(row *entities.FleetWall, byNode map[string]*entities.ManagedNode) FleetWallView {
	view := FleetWallView{FleetWall: row, TileList: []FleetWallTile{}}
	for _, tile := range decodeTiles(row.Tiles) {
		node, ok := byNode[tile.NodeId]
		tile.NodeKnown = ok
		if ok {
			tile.NodeName = displayNodeName(node)
			tile.NodeStatus = node.Status
			if node.Status != "" && node.Status != "online" {
				view.OfflineTiles++
			}
		} else {
			view.UnknownTiles++
		}
		view.TileList = append(view.TileList, tile)
	}
	return view
}

func (s *fleetWallService) record(ctx context.Context, action string, row *entities.FleetWall, actor int64, detail string) {
	if s.audit == nil {
		return
	}
	s.audit.Record(ctx, AuditEntry{
		Action:     action,
		TargetType: "fleet-wall",
		TargetId:   strconv.FormatInt(row.Id, 10),
		Outcome:    OutcomeSuccess,
		Detail:     detail,
		Metadata:   map[string]any{"grid": row.Grid, "isDefault": row.IsDefault},
	})
}

func validFleetGrid(grid string) bool {
	for _, g := range fleetWallGrids {
		if g == grid {
			return true
		}
	}
	return false
}

func tileKey(nodeId string, cameraId int64) string {
	return nodeId + ":" + strconv.FormatInt(cameraId, 10)
}

// encodeTiles / decodeTiles are the only two places that know how a tile list is stored.
//
// A node id cannot contain a colon or a comma (it is a UUID), which is what makes this
// encoding safe — and is why the pair of functions is here rather than inlined at four call
// sites that would each have to remember that.
func encodeTiles(tiles []FleetWallTile) string {
	parts := make([]string, 0, len(tiles))
	for _, tile := range tiles {
		nodeId := strings.TrimSpace(tile.NodeId)
		if nodeId == "" || tile.CameraId <= 0 {
			continue
		}
		parts = append(parts, tileKey(nodeId, tile.CameraId))
	}
	return strings.Join(parts, ",")
}

func decodeTiles(raw string) []FleetWallTile {
	out := []FleetWallTile{}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		// SplitN from the RIGHT would be wrong: a node id has no colon, so the LAST colon
		// and the only colon are the same one — but splitting on the first keeps this
		// correct even if that ever stops being true of node ids.
		idx := strings.LastIndex(part, ":")
		if idx <= 0 || idx == len(part)-1 {
			continue
		}
		cameraId, err := strconv.ParseInt(part[idx+1:], 10, 64)
		if err != nil || cameraId <= 0 {
			continue
		}
		out = append(out, FleetWallTile{NodeId: part[:idx], CameraId: cameraId})
	}
	return out
}
