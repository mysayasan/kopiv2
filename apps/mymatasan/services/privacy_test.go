package services

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	"github.com/mysayasan/kopiv2/infra/onvif"
)

type fakeZoneRepo struct {
	dbsql.IGenericRepo[entities.PrivacyZone]
	rows []*entities.PrivacyZone
	seq  int64
}

func (f *fakeZoneRepo) Get(_ context.Context, _ string, _, _ uint64, _ []sqldataenums.Filter, _ []sqldataenums.Sorter) ([]*entities.PrivacyZone, uint64, error) {
	out := make([]*entities.PrivacyZone, 0, len(f.rows))
	for _, row := range f.rows {
		cp := *row
		out = append(out, &cp)
	}
	return out, uint64(len(out)), nil
}

func (f *fakeZoneRepo) GetById(_ context.Context, _ string, id uint64) (*entities.PrivacyZone, error) {
	for _, row := range f.rows {
		if row.Id == int64(id) {
			cp := *row
			return &cp, nil
		}
	}
	return nil, errors.New("no result found")
}

func (f *fakeZoneRepo) Create(_ context.Context, _ string, model entities.PrivacyZone) (uint64, error) {
	f.seq++
	model.Id = f.seq
	f.rows = append(f.rows, &model)
	return uint64(model.Id), nil
}

func (f *fakeZoneRepo) UpdateById(_ context.Context, _ string, model entities.PrivacyZone) (uint64, error) {
	for i, row := range f.rows {
		if row.Id == model.Id {
			cp := model
			f.rows[i] = &cp
			return 1, nil
		}
	}
	return 0, errors.New("no result found")
}

func (f *fakeZoneRepo) DeleteById(_ context.Context, _ string, id uint64) (uint64, error) {
	for i, row := range f.rows {
		if row.Id == int64(id) {
			f.rows = append(f.rows[:i], f.rows[i+1:]...)
			return 1, nil
		}
	}
	return 0, nil
}

// fakeMaskCamera is a camera that stores masks — and can be told to store them WRONG,
// which is the case the whole verification path exists for.
type fakeMaskCamera struct {
	mu      sync.Mutex
	opts    *onvif.MaskOptions
	optsErr error
	masks   map[string]onvif.Mask
	seq     int
	calls   []string
	// distort rewrites what the camera stores, modelling a device that accepts a mask
	// with HTTP 200 and keeps something else.
	distort func(onvif.Mask) onvif.Mask
	// createErr makes the camera refuse new masks.
	createErr error
}

func newFakeMaskCamera(opts *onvif.MaskOptions) *fakeMaskCamera {
	return &fakeMaskCamera{opts: opts, masks: map[string]onvif.Mask{}}
}

func (f *fakeMaskCamera) MaskOptions(context.Context, uint64) (*onvif.MaskOptions, error) {
	if f.optsErr != nil {
		return nil, f.optsErr
	}
	return f.opts, nil
}

func (f *fakeMaskCamera) CameraMasks(context.Context, uint64) ([]onvif.Mask, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]onvif.Mask, 0, len(f.masks))
	for _, m := range f.masks {
		out = append(out, m)
	}
	return out, nil
}

func (f *fakeMaskCamera) CreateCameraMask(_ context.Context, _ uint64, mask onvif.Mask) (string, error) {
	if f.createErr != nil {
		return "", f.createErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seq++
	token := "MASK_" + string(rune('0'+f.seq))
	mask.Token = token
	if f.distort != nil {
		mask = f.distort(mask)
	}
	f.masks[token] = mask
	f.calls = append(f.calls, "create:"+token)
	return token, nil
}

func (f *fakeMaskCamera) SetCameraMask(_ context.Context, _ uint64, mask onvif.Mask) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.masks[mask.Token]; !ok {
		return errors.New("no such mask")
	}
	stored := mask
	if f.distort != nil {
		stored = f.distort(mask)
	}
	f.masks[mask.Token] = stored
	f.calls = append(f.calls, "set:"+mask.Token)
	return nil
}

func (f *fakeMaskCamera) DeleteCameraMask(_ context.Context, _ uint64, token string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.masks, token)
	f.calls = append(f.calls, "delete:"+token)
	return nil
}

func (f *fakeMaskCamera) VideoSourceToken(context.Context, uint64) (string, error) {
	return "VSC_1", nil
}

func (f *fakeMaskCamera) trail() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return strings.Join(f.calls, ",")
}

func newPrivacyRig(cam *fakeMaskCamera) (*privacyService, *fakeZoneRepo) {
	repo := &fakeZoneRepo{}
	svc := NewPrivacyService(repo, cam).(*privacyService)
	svc.now = func() int64 { return 1_700_000_000 }
	return svc, repo
}

func squareZone() [][2]float64 {
	return [][2]float64{{0.1, 0.1}, {0.4, 0.1}, {0.4, 0.4}, {0.1, 0.4}}
}

func maskingCamera() *onvif.MaskOptions {
	return &onvif.MaskOptions{Supported: true, MaxMasks: 4, MaxPoints: 8, Types: []string{"Color", "Blurred"}}
}

// THE POINT OF THE CAMERA HALF. A camera can accept a mask with HTTP 200 and store
// something else — a different coordinate space, a bounding box, or nothing. A privacy mask
// believed to be applied and not applied is worse than none, because somebody relies on it.
func TestAMaskTheCameraStoredDifferentlyIsNotConfirmed(t *testing.T) {
	cam := newFakeMaskCamera(maskingCamera())
	svc, _ := newPrivacyRig(cam)
	ctx := context.Background()

	if _, err := svc.SaveZone(ctx, PrivacyZoneSave{
		CameraId: 7, Name: "Neighbour's window", Points: squareZone(), Enabled: true,
	}); err != nil {
		t.Fatalf("save: %v", err)
	}
	status, err := svc.Status(ctx, 7)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status.Masking != MaskingConfirmed {
		t.Fatalf("a camera that stored the mask correctly should be confirmed, got %q (%s)",
			status.Masking, status.Detail)
	}

	// Now the camera starts keeping a different shape — the coordinate-space bug that
	// looks like success at every layer.
	cam.distort = func(m onvif.Mask) onvif.Mask {
		for i := range m.Polygon {
			m.Polygon[i].X /= 2
			m.Polygon[i].Y /= 2
		}
		return m
	}
	if _, err := svc.Apply(ctx, 7); err != nil {
		t.Fatalf("apply: %v", err)
	}
	status, _ = svc.Status(ctx, 7)
	if status.Masking != MaskingUnconfirmed {
		t.Fatalf("a mask stored as a different shape must NOT read as confirmed, got %q", status.Masking)
	}
	if !strings.Contains(status.Detail, "Neighbour's window") {
		t.Fatalf("the detail must name the zone that is not protected: %q", status.Detail)
	}
	// ...and the export protection is still true, because that half does not depend on
	// the camera at all.
	if !status.ExportRedaction {
		t.Fatal("exports are redacted whatever the camera does")
	}
}

// A camera that cannot mask is not an error, and the wording has to distinguish "the
// recording contains this" from "the exports do not".
func TestACameraThatCannotMaskSaysSo(t *testing.T) {
	cam := newFakeMaskCamera(&onvif.MaskOptions{Supported: false})
	svc, _ := newPrivacyRig(cam)
	ctx := context.Background()

	if _, err := svc.SaveZone(ctx, PrivacyZoneSave{
		CameraId: 7, Name: "Pavement", Points: squareZone(), Enabled: true,
	}); err != nil {
		t.Fatalf("save: %v", err)
	}
	status, _ := svc.Status(ctx, 7)
	if status.Masking != MaskingUnsupported {
		t.Fatalf("masking = %q, want unsupported", status.Masking)
	}
	if !strings.Contains(status.Detail, "recording") || !strings.Contains(status.Detail, "redacted") {
		t.Fatalf("the detail must say the recording keeps it and exports do not: %q", status.Detail)
	}
	// The zone is still stored and still redacts exports: an unmaskable camera is exactly
	// the case where the export protection matters most.
	regions, err := svc.ExportRegions(ctx, 7)
	if err != nil || len(regions) != 1 {
		t.Fatalf("export regions = %v (%v)", regions, err)
	}
	// A camera that could not be REACHED is a different answer from one that cannot mask:
	// one is a fact about the camera, the other about the network.
	cam.optsErr = errors.New("no answer")
	status, _ = svc.Status(ctx, 7)
	if status.Masking != MaskingUnreachable {
		t.Fatalf("an unreachable camera must not read as unsupported, got %q", status.Masking)
	}
}

func TestPrivacyZoneRefusals(t *testing.T) {
	cam := newFakeMaskCamera(maskingCamera())
	svc, _ := newPrivacyRig(cam)
	ctx := context.Background()

	cases := []struct {
		name string
		req  PrivacyZoneSave
		want string
	}{
		{name: "no name", req: PrivacyZoneSave{CameraId: 7, Points: squareZone()}, want: "needs a name"},
		{name: "no camera", req: PrivacyZoneSave{Name: "x", Points: squareZone()}, want: "needs a camera"},
		{
			// A zone that is not a polygon covers nothing — and a privacy zone covering
			// nothing is the worst row this table can hold, because it reads as protection
			// on every screen that lists it.
			name: "two points", req: PrivacyZoneSave{CameraId: 7, Name: "x", Points: [][2]float64{{0, 0}, {1, 1}}},
			want: "three corners",
		},
		{
			name: "no area", req: PrivacyZoneSave{CameraId: 7, Name: "x",
				Points: [][2]float64{{0.5, 0.5}, {0.501, 0.5}, {0.501, 0.501}}},
			want: "too small",
		},
		{
			name: "unknown style", req: PrivacyZoneSave{CameraId: 7, Name: "x", Points: squareZone(), Style: "invisible"},
			want: "unknown style",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := svc.SaveZone(ctx, tc.req); err == nil {
				t.Fatal("want a refusal")
			} else if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("refusal should say %q, said %q", tc.want, err.Error())
			}
		})
	}

	if _, err := svc.SaveZone(ctx, PrivacyZoneSave{CameraId: 7, Name: "Gate", Points: squareZone(), Enabled: true}); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := svc.SaveZone(ctx, PrivacyZoneSave{CameraId: 7, Name: "gate", Points: squareZone(), Enabled: true}); err == nil {
		t.Fatal("a duplicate name whatever its case must be refused — two rows nobody can tell apart")
	}
}

// Editing a zone UPDATES the camera's mask in place rather than removing and re-creating
// it. On some cameras a delete-then-create leaves the view briefly UNMASKED — a privacy
// control with a gap every time somebody adjusts it.
func TestEditingAZoneDoesNotUnmaskTheCamera(t *testing.T) {
	cam := newFakeMaskCamera(maskingCamera())
	svc, _ := newPrivacyRig(cam)
	ctx := context.Background()

	zone, err := svc.SaveZone(ctx, PrivacyZoneSave{
		CameraId: 7, Name: "Window", Points: squareZone(), Enabled: true,
	})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	token := zone.MaskToken
	if token == "" {
		t.Fatal("the camera's mask token was not recorded")
	}

	moved := [][2]float64{{0.2, 0.2}, {0.6, 0.2}, {0.6, 0.6}, {0.2, 0.6}}
	if _, err := svc.SaveZone(ctx, PrivacyZoneSave{
		Id: zone.Id, CameraId: 7, Name: "Window", Points: moved, Enabled: true,
	}); err != nil {
		t.Fatalf("edit: %v", err)
	}
	if strings.Contains(cam.trail(), "delete:") {
		t.Fatalf("editing removed the mask before replacing it: %s", cam.trail())
	}
	if !strings.Contains(cam.trail(), "set:"+token) {
		t.Fatalf("the existing mask was not updated in place: %s", cam.trail())
	}
	status, _ := svc.Status(ctx, 7)
	if status.Masking != MaskingConfirmed {
		t.Fatalf("the moved zone should still be confirmed, got %q (%s)", status.Masking, status.Detail)
	}
}

// Deleting a zone clears the CAMERA's mask first. The other order leaves a region masked
// that nothing in the product knows about — invisible here, and removable only from the
// camera's own web page.
func TestDeletingAZoneClearsTheCameraMask(t *testing.T) {
	cam := newFakeMaskCamera(maskingCamera())
	svc, _ := newPrivacyRig(cam)
	ctx := context.Background()

	zone, _ := svc.SaveZone(ctx, PrivacyZoneSave{
		CameraId: 7, Name: "Window", Points: squareZone(), Enabled: true,
	})
	if err := svc.DeleteZone(ctx, zone.Id); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if !strings.Contains(cam.trail(), "delete:"+zone.MaskToken) {
		t.Fatalf("the camera's mask was left behind: %s", cam.trail())
	}
	masks, _ := cam.CameraMasks(ctx, 7)
	if len(masks) != 0 {
		t.Fatalf("the camera still holds %d mask(s)", len(masks))
	}
}

// A camera that only takes rectangles gets a rectangle — reduced HERE rather than by the
// camera, so the read-back comparison stays honest. Letting the camera square it off
// silently would fail verification forever and report a working mask as unconfirmed.
func TestARectangleOnlyCameraIsSentARectangle(t *testing.T) {
	opts := maskingCamera()
	opts.RectangleOnly = true
	cam := newFakeMaskCamera(opts)
	svc, _ := newPrivacyRig(cam)
	ctx := context.Background()

	// An L-shape, which no rectangle-only camera can store as drawn.
	l := [][2]float64{{0.1, 0.1}, {0.5, 0.1}, {0.5, 0.3}, {0.3, 0.3}, {0.3, 0.5}, {0.1, 0.5}}
	if _, err := svc.SaveZone(ctx, PrivacyZoneSave{
		CameraId: 7, Name: "Corner", Points: l, Enabled: true,
	}); err != nil {
		t.Fatalf("save: %v", err)
	}
	masks, _ := cam.CameraMasks(ctx, 7)
	if len(masks) != 1 || len(masks[0].Polygon) != 4 {
		t.Fatalf("want a 4-point rectangle on the camera, got %+v", masks)
	}
	// The rectangle must COVER the whole shape. Erring towards covering more is the only
	// safe direction for a privacy control: too much black is a complaint, too little is
	// a disclosure.
	minX, maxX := 1.0, 0.0
	for _, p := range masks[0].Polygon {
		x, _ := onvif.MaskPointToUnit(p)
		if x < minX {
			minX = x
		}
		if x > maxX {
			maxX = x
		}
	}
	if minX > 0.1001 || maxX < 0.4999 {
		t.Fatalf("the rectangle does not cover the drawn shape: x %v..%v", minX, maxX)
	}
}

// A camera holds a fixed number of masks. It would take the first few and silently ignore
// the rest, and WHICH zones were dropped would be invisible.
func TestZonesBeyondTheCamerasLimitAreNotSilentlyLost(t *testing.T) {
	opts := maskingCamera()
	opts.MaxMasks = 2
	cam := newFakeMaskCamera(opts)
	svc, _ := newPrivacyRig(cam)
	ctx := context.Background()

	for _, name := range []string{"A zone", "B zone", "C zone"} {
		if _, err := svc.SaveZone(ctx, PrivacyZoneSave{
			CameraId: 7, Name: name, Points: squareZone(), Enabled: true,
		}); err != nil {
			t.Fatalf("save %s: %v", name, err)
		}
	}
	masks, _ := cam.CameraMasks(ctx, 7)
	if len(masks) > 2 {
		t.Fatalf("the camera was sent more masks than it can hold: %d", len(masks))
	}
	// The third zone is NOT confirmed, which is what the operator has to be told — and
	// every zone still redacts exports.
	status, _ := svc.Status(ctx, 7)
	if status.Masking == MaskingConfirmed {
		t.Fatal("a zone the camera could not take must not read as confirmed")
	}
	regions, _ := svc.ExportRegions(ctx, 7)
	if len(regions) != 3 {
		t.Fatalf("every zone redacts exports whatever the camera can hold, got %d", len(regions))
	}
}

func TestOnlyEnabledZonesRedactExports(t *testing.T) {
	cam := newFakeMaskCamera(maskingCamera())
	svc, _ := newPrivacyRig(cam)
	ctx := context.Background()

	_, _ = svc.SaveZone(ctx, PrivacyZoneSave{CameraId: 7, Name: "On", Points: squareZone(), Enabled: true})
	_, _ = svc.SaveZone(ctx, PrivacyZoneSave{CameraId: 7, Name: "Off", Points: squareZone(), Enabled: false})

	regions, err := svc.ExportRegions(ctx, 7)
	if err != nil {
		t.Fatalf("regions: %v", err)
	}
	if len(regions) != 1 || regions[0].Name != "On" {
		t.Fatalf("a switched-off zone must not redact: %+v", regions)
	}
	// ...and the camera should not be holding a mask for the switched-off one either.
	masks, _ := cam.CameraMasks(ctx, 7)
	if len(masks) != 1 {
		t.Fatalf("the camera holds %d masks for one enabled zone", len(masks))
	}
}

// The filter is expressed in FRACTIONS of the frame, so the same zone is correct whatever
// resolution the camera is recording at — including after somebody changes it, which is the
// case a pixel rectangle silently gets wrong.
func TestRedactionFilterIsResolutionIndependent(t *testing.T) {
	filter := redactionFilter([]PrivacyRegion{{
		Name: "Window", Points: [][2]float64{{0.1, 0.2}, {0.5, 0.2}, {0.5, 0.6}, {0.1, 0.6}},
	}})
	for _, want := range []string{"drawbox=", "x=iw*0.1000", "y=ih*0.2000", "w=iw*0.4000", "h=ih*0.4000", "t=fill"} {
		if !strings.Contains(filter, want) {
			t.Fatalf("filter missing %q: %s", want, filter)
		}
	}
	// Solid black, fully opaque: a blur invites the argument that something could be
	// recovered, and on a low-detail region it sometimes can be.
	if !strings.Contains(filter, "color=black@1.0") {
		t.Fatalf("the region is not fully obscured: %s", filter)
	}
	// A region that is not a polygon contributes nothing rather than a malformed filter
	// that would make the whole export fail.
	if redactionFilter([]PrivacyRegion{{Name: "x", Points: [][2]float64{{0, 0}}}}) != "" {
		t.Fatal("a zone with too few points must not produce a filter")
	}
}

// A redaction that was asked for and finds nothing to redact must NOT mark the bundle as
// redacted. A bundle that CLAIMS to be redacted and had nothing burned into it is a false
// statement about what the recipient is being protected from.
func TestARedactionWithNoZonesDoesNotClaimToBeRedacted(t *testing.T) {
	svc := &evidenceExportService{privacy: emptyPrivacy{}}
	if plan := svc.redactionFor(context.Background(), true, 7); plan != nil {
		t.Fatalf("want no redaction, got %+v", plan.manifest)
	}
	// ...and an export that did not ask for one never gets one.
	svc2 := &evidenceExportService{privacy: onePrivacy{}}
	if plan := svc2.redactionFor(context.Background(), false, 7); plan != nil {
		t.Fatal("an export that did not ask to be redacted must not be")
	}
	// When there IS something to redact, the manifest names it — a recipient has to know
	// what they are NOT being shown, not merely that something is missing.
	plan := svc2.redactionFor(context.Background(), true, 7)
	if plan == nil || !plan.manifest.Applied {
		t.Fatal("want a redaction")
	}
	if len(plan.manifest.Regions) != 1 || plan.manifest.Regions[0] != "Window" {
		t.Fatalf("the manifest must name the regions: %+v", plan.manifest.Regions)
	}
	if !strings.Contains(plan.manifest.Note, "DERIVATIVE") {
		t.Fatalf("the note must say the file is a derivative: %q", plan.manifest.Note)
	}
	if !strings.Contains(plan.manifest.Note, "will not match the digests") {
		t.Fatalf("the note must warn that the source digests will not match: %q", plan.manifest.Note)
	}
}

type emptyPrivacy struct{}

func (emptyPrivacy) ExportRegions(context.Context, int64) ([]PrivacyRegion, error) {
	return nil, nil
}

type onePrivacy struct{}

func (onePrivacy) ExportRegions(context.Context, int64) ([]PrivacyRegion, error) {
	return []PrivacyRegion{{Name: "Window", Points: [][2]float64{{0.1, 0.1}, {0.4, 0.1}, {0.4, 0.4}}}}, nil
}
