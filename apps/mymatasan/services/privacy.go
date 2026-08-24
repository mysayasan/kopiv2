package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	"github.com/mysayasan/kopiv2/infra/onvif"
)

// Privacy zones: regions of a camera's view that must not be seen (W3-6).
//
// ONE stored region, TWO mechanisms, and the whole design is about not confusing them:
//
//   - THE CAMERA is asked to burn it in, as an ONVIF Media2 privacy mask. Then the pixels
//     never leave the sensor. The recording does not contain them, an export cannot leak
//     them, and somebody who steals the drive has nothing. This is a privacy control.
//   - THE EXPORT redacts it regardless. That protects the footage that leaves the building,
//     which is where most of the risk is — but the recording still holds the pixels and
//     anybody with access to this appliance can still see them. This is a control over
//     disclosure, not over capture.
//
// A MASK THAT IS NOT BURNED IN BY THE CAMERA IS A COURTESY, NOT A PRIVACY CONTROL, and
// which one an operator has is a fact about their hardware rather than about this software.
// So it is reported per camera, in those terms, rather than implying the stronger claim.
//
// WHAT WAS DELIBERATELY NOT BUILT: masking inside the recording pipeline. It would give the
// strong guarantee on every camera, and it would cost the product its architecture —
// recording is `-c copy` today, and masking mid-stream means decoding, filtering and
// re-encoding every camera continuously. The capacity story changes by an order of
// magnitude for something most cameras will do for free. Stated here because "why not just
// do it ourselves" is the first question anybody asks.

// Mask confirmation states, reported per camera.
const (
	// MaskingConfirmed: the camera accepted the masks AND read them back matching what was
	// sent. The only state that justifies telling somebody the pixels are not recorded.
	MaskingConfirmed = "confirmed"
	// MaskingUnconfirmed: the camera accepted them and read back something else — a
	// different coordinate space, a bounding box, or nothing. Treated as NOT masked.
	MaskingUnconfirmed = "unconfirmed"
	// MaskingUnsupported: the camera has no Media2 mask support. Exports still redact.
	MaskingUnsupported = "unsupported"
	// MaskingUnreachable: the camera could not be asked. Distinct from unsupported,
	// because one is a fact about the camera and the other about the network.
	MaskingUnreachable = "unreachable"
)

// privacyMaxZones bounds one camera. Cameras hold a handful of masks; this is the point
// past which the export filter chain also stops being reasonable.
const privacyMaxZones = 16

// maskTolerance is how far a read-back polygon may sit from what was written and still
// count as the same region. Generous on purpose: cameras quantise mask coordinates to
// their own grid, and a few pixels is the device being a device. A different coordinate
// space is out by a factor, not by a rounding.
const maskTolerance = 0.06

// PrivacyZoneView is a zone as the screen needs it.
type PrivacyZoneView struct {
	*entities.PrivacyZone
	// Points is Polygon parsed, so no caller has to agree with this file about the
	// encoding.
	Points [][2]float64 `json:"points"`
}

// PrivacyStatus is what this camera can actually do about privacy, in the terms an
// operator has to decide with.
type PrivacyStatus struct {
	CameraId int64 `json:"cameraId"`
	// Masking is one of the Masking* constants above.
	Masking string `json:"masking"`
	// Detail is the English sentence, for API consumers and the log.
	//
	// THE SCREEN MUST NOT RENDER THIS. It is composed here, in English, and an Arabic
	// screen pass found it printed verbatim in an Arabic UI — the single most important
	// sentence on the page, untranslated. Exactly the shape of the defect W3-4 shipped
	// with its rule-schedule summaries. The SPA renders from Masking plus the fields
	// below, out of its own dictionary; this stays because a caller reading the API over
	// curl still deserves a sentence.
	Detail string `json:"detail"`
	// UnconfirmedZones names the zones the camera is NOT known to be masking, so a client
	// can say which ones in its own language rather than parsing an English sentence.
	// Empty on every state except unconfirmed.
	UnconfirmedZones []string `json:"unconfirmedZones"`
	// HasZones distinguishes "this camera can mask and nothing is drawn yet" from "this
	// camera is masking what was drawn" — two very different things to tell somebody, and
	// the same Masking value.
	HasZones bool `json:"hasZones"`
	// MaxMasks and RectangleOnly are what the camera said about its own limits, so the
	// editor can refuse a shape the camera will silently mangle.
	MaxMasks      int  `json:"maxMasks"`
	RectangleOnly bool `json:"rectangleOnly"`
	// ExportRedaction is always true. It is in the payload so the screen does not have to
	// hard-code the one guarantee that does not depend on the hardware.
	ExportRedaction bool `json:"exportRedaction"`
}

// PrivacyZoneSave is a create or an update.
type PrivacyZoneSave struct {
	Id       int64
	CameraId int64
	Name     string
	Points   [][2]float64
	Style    string
	Enabled  bool
	Actor    CaseActor
}

// IPrivacyService is the privacy-zone surface.
type IPrivacyService interface {
	Zones(ctx context.Context, cameraId int64) ([]PrivacyZoneView, error)
	SaveZone(ctx context.Context, req PrivacyZoneSave) (*PrivacyZoneView, error)
	DeleteZone(ctx context.Context, id int64) error
	// Status reports what this camera can do, and re-checks the camera to say so.
	Status(ctx context.Context, cameraId int64) (*PrivacyStatus, error)
	// Apply pushes every enabled zone to the camera and verifies the result.
	Apply(ctx context.Context, cameraId int64) (*PrivacyStatus, error)
	// ExportRegions returns the regions an export of this camera must obscure. Used by
	// the evidence export; returns an empty list when there is nothing to redact.
	ExportRegions(ctx context.Context, cameraId int64) ([]PrivacyRegion, error)
	// DeleteZonesForCamera is the camera-deletion cascade's leg.
	DeleteZonesForCamera(ctx context.Context, cameraId int64) (int, error)
}

// PrivacyRegion is one region to obscure, in our 0..1 space.
type PrivacyRegion struct {
	Name   string       `json:"name"`
	Points [][2]float64 `json:"points"`
	Style  string       `json:"style"`
}

// privacyCameraClient is the slice of the camera service this needs.
type privacyCameraClient interface {
	MaskOptions(ctx context.Context, id uint64) (*onvif.MaskOptions, error)
	CameraMasks(ctx context.Context, id uint64) ([]onvif.Mask, error)
	CreateCameraMask(ctx context.Context, id uint64, mask onvif.Mask) (string, error)
	SetCameraMask(ctx context.Context, id uint64, mask onvif.Mask) error
	DeleteCameraMask(ctx context.Context, id uint64, token string) error
	VideoSourceToken(ctx context.Context, id uint64) (string, error)
}

type privacyService struct {
	repo    dbsql.IGenericRepo[entities.PrivacyZone]
	cameras privacyCameraClient
	now     func() int64
}

func NewPrivacyService(repo dbsql.IGenericRepo[entities.PrivacyZone], cameras privacyCameraClient) IPrivacyService {
	return &privacyService{repo: repo, cameras: cameras, now: func() int64 { return time.Now().UTC().Unix() }}
}

func (s *privacyService) Zones(ctx context.Context, cameraId int64) ([]PrivacyZoneView, error) {
	rows, err := s.list(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]PrivacyZoneView, 0, len(rows))
	for _, row := range rows {
		if row == nil || (cameraId > 0 && row.CameraId != cameraId) {
			continue
		}
		out = append(out, PrivacyZoneView{PrivacyZone: row, Points: parsePrivacyPolygon(row.Polygon)})
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out, nil
}

func (s *privacyService) SaveZone(ctx context.Context, req PrivacyZoneSave) (*PrivacyZoneView, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, errors.New("a privacy zone needs a name")
	}
	if req.CameraId <= 0 {
		return nil, errors.New("a privacy zone needs a camera")
	}
	// A zone that is not a polygon covers nothing, and a "privacy zone" covering nothing
	// is the worst possible row in this table: it reads as protection on every screen.
	points, err := normalizePrivacyPoints(req.Points)
	if err != nil {
		return nil, err
	}
	style := strings.ToLower(strings.TrimSpace(req.Style))
	switch style {
	case "", onvifStyleColor:
		style = onvifStyleColor
	case onvifStyleBlurred, onvifStylePixelated:
	default:
		return nil, fmt.Errorf("unknown style %q — use color, blurred or pixelated", req.Style)
	}

	existing, err := s.Zones(ctx, req.CameraId)
	if err != nil {
		return nil, err
	}
	count := len(existing)
	for _, zone := range existing {
		if zone.Id == req.Id {
			count--
		}
		if zone.Id != req.Id && strings.EqualFold(strings.TrimSpace(zone.Name), name) {
			return nil, fmt.Errorf("this camera already has a privacy zone called %q", zone.Name)
		}
	}
	if req.Id == 0 && count >= privacyMaxZones {
		return nil, fmt.Errorf("a camera holds at most %d privacy zones", privacyMaxZones)
	}

	now := s.now()
	row := entities.PrivacyZone{
		Id: req.Id, CameraId: req.CameraId, Name: name,
		Polygon: encodePrivacyPolygon(points), Style: style, Enabled: req.Enabled,
		UpdatedAt: now,
	}
	if req.Id > 0 {
		prev, gerr := s.get(ctx, req.Id)
		if gerr != nil {
			return nil, gerr
		}
		row.CreatedBy, row.CreatedName, row.CreatedAt = prev.CreatedBy, prev.CreatedName, prev.CreatedAt
		// The camera's own handle survives an edit: the mask is updated in place rather
		// than removed and re-created, which on some cameras would briefly leave the view
		// UNMASKED — a privacy control that has a gap every time somebody adjusts it.
		row.MaskToken = prev.MaskToken
		if _, uerr := s.repo.UpdateById(ctx, "", row); uerr != nil {
			return nil, uerr
		}
	} else {
		row.CreatedBy, row.CreatedName, row.CreatedAt = req.Actor.Id, req.Actor.Name, now
		id, cerr := s.repo.Create(ctx, "", row)
		if cerr != nil {
			return nil, cerr
		}
		row.Id = int64(id)
	}

	// Pushed to the camera immediately, not on a later "apply" press. A zone that is saved
	// and not applied is a row that says "protected" on the screen and protects nothing,
	// and the gap between the two is exactly where somebody stops paying attention.
	if _, aerr := s.Apply(ctx, req.CameraId); aerr != nil {
		log.Printf("privacy: cam%d: could not apply zones to the camera: %v", req.CameraId, aerr)
	}

	saved, err := s.get(ctx, row.Id)
	if err != nil {
		return nil, err
	}
	view := PrivacyZoneView{PrivacyZone: saved, Points: parsePrivacyPolygon(saved.Polygon)}
	return &view, nil
}

func (s *privacyService) DeleteZone(ctx context.Context, id int64) error {
	row, err := s.get(ctx, id)
	if err != nil {
		return err
	}
	// The CAMERA's mask goes first. Deleting our row and failing to remove the mask would
	// leave a region masked that nothing in the product knows about — invisible, and only
	// removable from the camera's own web page.
	if token := strings.TrimSpace(row.MaskToken); token != "" {
		if derr := s.cameras.DeleteCameraMask(ctx, uint64(row.CameraId), token); derr != nil {
			log.Printf("privacy: cam%d: could not remove mask %q from the camera: %v", row.CameraId, token, derr)
		}
	}
	if _, err := s.repo.DeleteById(ctx, "", uint64(id)); err != nil {
		return err
	}
	return nil
}

func (s *privacyService) DeleteZonesForCamera(ctx context.Context, cameraId int64) (int, error) {
	zones, err := s.Zones(ctx, cameraId)
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, zone := range zones {
		// No attempt to clear the camera's masks here: the camera is being removed from
		// this appliance, not decommissioned, and a mask somebody set up should outlive
		// our record of it rather than being silently stripped on the way out.
		if _, derr := s.repo.DeleteById(ctx, "", uint64(zone.Id)); derr != nil {
			return removed, derr
		}
		removed++
	}
	return removed, nil
}

func (s *privacyService) Status(ctx context.Context, cameraId int64) (*PrivacyStatus, error) {
	status := &PrivacyStatus{CameraId: cameraId, ExportRedaction: true}
	opts, err := s.cameras.MaskOptions(ctx, uint64(cameraId))
	if err != nil {
		status.Masking = MaskingUnreachable
		status.Detail = "The camera could not be reached, so whether it can mask anything is unknown. Exports are still redacted."
		return status, nil
	}
	if opts == nil || !opts.Supported {
		status.Masking = MaskingUnsupported
		status.Detail = "This camera cannot mask anything itself, so the recording will contain these areas. Exports are still redacted."
		return status, nil
	}
	status.MaxMasks, status.RectangleOnly = opts.MaxMasks, opts.RectangleOnly

	zones, err := s.Zones(ctx, cameraId)
	if err != nil {
		return nil, err
	}
	enabled := enabledZones(zones)
	status.HasZones = len(enabled) > 0
	if len(enabled) == 0 {
		status.Masking = MaskingConfirmed
		status.Detail = "This camera can mask areas itself. Nothing is masked yet."
		return status, nil
	}
	confirmed, reason, unconfirmed := s.verify(ctx, cameraId, enabled)
	if confirmed {
		status.Masking = MaskingConfirmed
		status.Detail = "The camera is masking these areas, so they are not recorded at all."
		return status, nil
	}
	status.Masking = MaskingUnconfirmed
	status.UnconfirmedZones = unconfirmed
	status.Detail = "The camera accepted these areas but did not confirm them (" + reason +
		"), so treat the recording as containing them. Exports are still redacted."
	return status, nil
}

func (s *privacyService) Apply(ctx context.Context, cameraId int64) (*PrivacyStatus, error) {
	zones, err := s.Zones(ctx, cameraId)
	if err != nil {
		return nil, err
	}
	opts, err := s.cameras.MaskOptions(ctx, uint64(cameraId))
	if err == nil && opts != nil && opts.Supported {
		s.push(ctx, cameraId, zones, opts)
	}
	return s.Status(ctx, cameraId)
}

// push writes the enabled zones to the camera, creating, updating and removing as needed.
func (s *privacyService) push(ctx context.Context, cameraId int64, zones []PrivacyZoneView, opts *onvif.MaskOptions) {
	configToken, err := s.cameras.VideoSourceToken(ctx, uint64(cameraId))
	if err != nil {
		log.Printf("privacy: cam%d: no video source configuration: %v", cameraId, err)
		return
	}
	enabled := enabledZones(zones)
	if opts.MaxMasks > 0 && len(enabled) > opts.MaxMasks {
		// The camera would take the first few and silently ignore the rest. Which zones
		// were dropped would be invisible, so the overflow is logged by NAME — the ones
		// that are not protected are the ones somebody has to know about.
		dropped := make([]string, 0)
		for _, z := range enabled[opts.MaxMasks:] {
			dropped = append(dropped, z.Name)
		}
		log.Printf("privacy: cam%d: the camera holds %d masks; NOT masked on the camera: %s",
			cameraId, opts.MaxMasks, strings.Join(dropped, ", "))
		enabled = enabled[:opts.MaxMasks]
	}

	wanted := map[string]bool{}
	for _, zone := range enabled {
		polygon := maskPolygonFor(zone, opts)
		mask := onvif.Mask{
			Token:              strings.TrimSpace(zone.MaskToken),
			ConfigurationToken: configToken,
			Polygon:            polygon,
			Type:               onvifMaskType(zone.Style, opts),
			Enabled:            true,
		}
		if mask.Token != "" {
			if serr := s.cameras.SetCameraMask(ctx, uint64(cameraId), mask); serr == nil {
				wanted[mask.Token] = true
				continue
			}
			// The stored token is a handle we do not own: a camera that has been factory
			// reset, or had its masks cleared from its own web page, will not know it.
			// Re-create rather than failing, which is what the operator meant.
			mask.Token = ""
		}
		token, cerr := s.cameras.CreateCameraMask(ctx, uint64(cameraId), mask)
		if cerr != nil {
			log.Printf("privacy: cam%d: zone %q was not applied to the camera: %v", cameraId, zone.Name, cerr)
			continue
		}
		wanted[token] = true
		row := *zone.PrivacyZone
		row.MaskToken = token
		row.UpdatedAt = s.now()
		if _, uerr := s.repo.UpdateById(ctx, "", row); uerr != nil {
			log.Printf("privacy: cam%d: could not record mask token for %q: %v", cameraId, zone.Name, uerr)
		}
	}

	// Masks this appliance created and no longer wants. Only OURS: a mask set up on the
	// camera's own web page is somebody else's decision, and quietly deleting it would be
	// this product removing a privacy control it did not create.
	for _, zone := range zones {
		token := strings.TrimSpace(zone.MaskToken)
		if token == "" || wanted[token] {
			continue
		}
		if derr := s.cameras.DeleteCameraMask(ctx, uint64(cameraId), token); derr != nil {
			log.Printf("privacy: cam%d: could not remove a disabled mask: %v", cameraId, derr)
			continue
		}
		row := *zone.PrivacyZone
		row.MaskToken = ""
		row.UpdatedAt = s.now()
		_, _ = s.repo.UpdateById(ctx, "", row)
	}
}

// verify reads the masks back and compares them with what was asked for.
//
// THE WHOLE POINT OF THE CAMERA HALF. A camera can accept a mask with HTTP 200 and store
// something else — a different coordinate space, a bounding rectangle instead of the
// polygon, or nothing at all. encoder.go already carries this scar for H.265: "many cameras
// accept a Media1 set with HTTP 200 but silently keep H.264". A privacy mask believed to be
// applied and not applied is worse than none, because somebody relies on it.
// It returns the English reason (for Detail) and the NAMES of the zones that are not
// confirmed, so the screen can compose its own sentence in its own language.
func (s *privacyService) verify(ctx context.Context, cameraId int64, enabled []PrivacyZoneView) (bool, string, []string) {
	onCamera, err := s.cameras.CameraMasks(ctx, uint64(cameraId))
	if err != nil {
		names := make([]string, 0, len(enabled))
		for _, zone := range enabled {
			names = append(names, zone.Name)
		}
		return false, "the camera would not say what it has stored", names
	}
	byToken := map[string]onvif.Mask{}
	for _, m := range onCamera {
		byToken[m.Token] = m
	}
	for _, zone := range enabled {
		token := strings.TrimSpace(zone.MaskToken)
		if token == "" {
			return false, "the camera did not accept " + zone.Name, []string{zone.Name}
		}
		mask, ok := byToken[token]
		if !ok {
			return false, "the camera no longer has a mask for " + zone.Name, []string{zone.Name}
		}
		if !mask.Enabled {
			return false, "the mask for " + zone.Name + " is switched off on the camera", []string{zone.Name}
		}
		want := unitPointsToMask(zone.Points)
		if !onvif.MasksMatch(want, mask.Polygon, maskTolerance) {
			return false, "the camera stored a different shape for " + zone.Name, []string{zone.Name}
		}
	}
	return true, "", nil
}

func (s *privacyService) ExportRegions(ctx context.Context, cameraId int64) ([]PrivacyRegion, error) {
	zones, err := s.Zones(ctx, cameraId)
	if err != nil {
		return nil, err
	}
	out := make([]PrivacyRegion, 0, len(zones))
	for _, zone := range enabledZones(zones) {
		out = append(out, PrivacyRegion{Name: zone.Name, Points: zone.Points, Style: zone.Style})
	}
	return out, nil
}

// --- internals ------------------------------------------------------------------------

const (
	onvifStyleColor     = "color"
	onvifStyleBlurred   = "blurred"
	onvifStylePixelated = "pixelated"
)

func onvifMaskType(style string, opts *onvif.MaskOptions) string {
	want := map[string]string{
		onvifStyleColor:     onvif.MaskTypeColor,
		onvifStyleBlurred:   onvif.MaskTypeBlurred,
		onvifStylePixelated: onvif.MaskTypePixelated,
	}[strings.ToLower(strings.TrimSpace(style))]
	if want == "" {
		want = onvif.MaskTypeColor
	}
	if opts == nil || len(opts.Types) == 0 {
		return want
	}
	for _, t := range opts.Types {
		if strings.EqualFold(t, want) {
			return want
		}
	}
	// A camera that cannot blur can still black out. Falling back beats refusing the zone:
	// a solid box is still a mask, and no mask is not.
	return onvif.MaskTypeColor
}

// maskPolygonFor converts a zone into the camera's coordinate space, reducing it to its
// bounding rectangle when the camera only takes rectangles.
//
// Reducing it HERE rather than letting the camera do it is what keeps the read-back
// comparison honest: a camera that silently squares off a polygon would otherwise fail
// verification forever and report a working mask as unconfirmed.
func maskPolygonFor(zone PrivacyZoneView, opts *onvif.MaskOptions) []onvif.MaskPoint {
	points := zone.Points
	if opts != nil && opts.RectangleOnly {
		points = boundingRect(points)
	}
	if opts != nil && opts.MaxPoints > 0 && len(points) > opts.MaxPoints {
		points = boundingRect(points)
	}
	return unitPointsToMask(points)
}

func unitPointsToMask(points [][2]float64) []onvif.MaskPoint {
	out := make([]onvif.MaskPoint, 0, len(points))
	for _, p := range points {
		out = append(out, onvif.MaskPointFromUnit(p[0], p[1]))
	}
	return out
}

func boundingRect(points [][2]float64) [][2]float64 {
	if len(points) == 0 {
		return points
	}
	minX, minY := points[0][0], points[0][1]
	maxX, maxY := minX, minY
	for _, p := range points[1:] {
		if p[0] < minX {
			minX = p[0]
		}
		if p[0] > maxX {
			maxX = p[0]
		}
		if p[1] < minY {
			minY = p[1]
		}
		if p[1] > maxY {
			maxY = p[1]
		}
	}
	return [][2]float64{{minX, minY}, {maxX, minY}, {maxX, maxY}, {minX, maxY}}
}

func enabledZones(zones []PrivacyZoneView) []PrivacyZoneView {
	out := make([]PrivacyZoneView, 0, len(zones))
	for _, zone := range zones {
		if zone.Enabled && len(zone.Points) >= 3 {
			out = append(out, zone)
		}
	}
	return out
}

func (s *privacyService) list(ctx context.Context) ([]*entities.PrivacyZone, error) {
	rows, _, err := s.repo.Get(ctx, "", 500, 0, nil,
		[]sqldataenums.Sorter{{FieldName: "CameraId", Sort: sqldataenums.ASC}})
	return rows, err
}

func (s *privacyService) get(ctx context.Context, id int64) (*entities.PrivacyZone, error) {
	if id <= 0 {
		return nil, errors.New("a privacy zone id is required")
	}
	row, err := s.repo.GetById(ctx, "", uint64(id))
	if err != nil {
		if isNoResultFoundErr(err) {
			return nil, errors.New("no such privacy zone")
		}
		return nil, err
	}
	if row == nil {
		return nil, errors.New("no such privacy zone")
	}
	return row, nil
}

// normalizePrivacyPoints clamps to 0..1 and refuses anything that is not a polygon.
//
// A zone with fewer than three points, or one with no area, covers nothing — and a privacy
// zone covering nothing is the worst row this table can hold, because it reads as
// protection on every screen that lists it.
func normalizePrivacyPoints(points [][2]float64) ([][2]float64, error) {
	out := make([][2]float64, 0, len(points))
	for _, p := range points {
		out = append(out, [2]float64{clampUnit(p[0]), clampUnit(p[1])})
	}
	if len(out) < 3 {
		return nil, errors.New("a privacy zone needs at least three corners")
	}
	rect := boundingRect(out)
	if rect[1][0]-rect[0][0] < 0.01 || rect[2][1]-rect[1][1] < 0.01 {
		return nil, errors.New("that privacy zone is too small to cover anything")
	}
	return out, nil
}

func clampUnit(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func encodePrivacyPolygon(points [][2]float64) string {
	blob, err := json.Marshal(points)
	if err != nil {
		return "[]"
	}
	return string(blob)
}

// parsePrivacyPolygon reads the stored polygon. The same encoding detection zones use, so
// the same editor draws both on the same picture.
func parsePrivacyPolygon(value string) [][2]float64 {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return [][2]float64{}
	}
	var raw [][]float64
	if err := json.Unmarshal([]byte(trimmed), &raw); err != nil {
		return [][2]float64{}
	}
	out := make([][2]float64, 0, len(raw))
	for _, p := range raw {
		if len(p) < 2 {
			continue
		}
		out = append(out, [2]float64{clampUnit(p[0]), clampUnit(p[1])})
	}
	return out
}
