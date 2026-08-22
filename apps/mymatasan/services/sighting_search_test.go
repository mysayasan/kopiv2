package services

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// searchObsRepo serves observation rows to the search, honouring the limit so the
// "asked for one more than the limit" cap detection can be exercised for real.
type searchObsRepo struct {
	dbsql.IGenericRepo[entities.ObjectObservation]
	rows []*entities.ObjectObservation
	// lastFilters records what the service asked for, so filter construction is testable
	// without a database.
	lastFilters []sqldataenums.Filter
}

func (r *searchObsRepo) Get(_ context.Context, _ string, limit, offset uint64, filters []sqldataenums.Filter, _ []sqldataenums.Sorter) ([]*entities.ObjectObservation, uint64, error) {
	r.lastFilters = filters
	rows := r.rows
	if offset >= uint64(len(rows)) {
		return nil, uint64(len(r.rows)), nil
	}
	rows = rows[offset:]
	if uint64(len(rows)) > limit {
		rows = rows[:limit]
	}
	return rows, uint64(len(r.rows)), nil
}

// searchAlertRepo pages alert rows back to the identity search.
type searchAlertRepo struct {
	dbsql.IGenericRepo[entities.AlertEvent]
	rows        []*entities.AlertEvent
	lastFilters []sqldataenums.Filter
}

func (r *searchAlertRepo) Get(_ context.Context, _ string, limit, offset uint64, filters []sqldataenums.Filter, _ []sqldataenums.Sorter) ([]*entities.AlertEvent, uint64, error) {
	r.lastFilters = filters
	if offset >= uint64(len(r.rows)) {
		return nil, uint64(len(r.rows)), nil
	}
	rows := r.rows[offset:]
	if uint64(len(rows)) > limit {
		rows = rows[:limit]
	}
	return rows, uint64(len(r.rows)), nil
}

type searchCameraRepo struct {
	dbsql.IGenericRepo[entities.Camera]
	rows []*entities.Camera
}

func (r *searchCameraRepo) Get(_ context.Context, _ string, _, _ uint64, _ []sqldataenums.Filter, _ []sqldataenums.Sorter) ([]*entities.Camera, uint64, error) {
	return r.rows, uint64(len(r.rows)), nil
}

// noSegments is a recording service that has no footage at all. configs describes which
// cameras are configured to record — the detect-only case is a camera absent from it.
type noSegments struct {
	IRecordingService
	configs   []*entities.RecordingConfig
	configErr bool
}

func (noSegments) GetSegments(_ context.Context, _, _ uint64, _, _, _, _ int64) ([]*entities.RecordingSegment, uint64, error) {
	return nil, 0, nil
}
func (n noSegments) ListConfigs(context.Context) ([]*entities.RecordingConfig, error) {
	if n.configErr {
		return nil, errors.New("configs unavailable")
	}
	return n.configs, nil
}

func obs(id, cameraId, startedAt int64, label string) *entities.ObjectObservation {
	return &entities.ObjectObservation{Id: id, CameraId: cameraId, Label: label, StartedAt: startedAt, EndedAt: startedAt + 4, MaxConfidence: 0.9, MaxCount: 1}
}

func plateAlert(id, cameraId, at int64, plate, color, vehicleType string) *entities.AlertEvent {
	meta, _ := json.Marshal(map[string]any{"plate": plate, "color": color, "vehicleType": vehicleType})
	return &entities.AlertEvent{Id: id, CameraId: cameraId, DetectionType: "object", Label: "Plate " + plate + " (" + color + " " + vehicleType + ")", Confidence: 0.88, Metadata: string(meta), CreatedAt: at}
}

func faceAlert(id, cameraId, at int64, person string) *entities.AlertEvent {
	meta, _ := json.Marshal(map[string]any{"personName": person, "personId": 3})
	return &entities.AlertEvent{Id: id, CameraId: cameraId, DetectionType: "person", Label: person + " (94%)", Confidence: 0.94, Metadata: string(meta), CreatedAt: at}
}

func newSearch(obsRows []*entities.ObjectObservation, alertRows []*entities.AlertEvent) (*SightingSearch, *searchObsRepo, *searchAlertRepo) {
	return newSearchWithConfigs(obsRows, alertRows, nil)
}

// newSearchWithConfigs builds the search with an explicit set of recording configs, so a
// test can distinguish a camera that records (footage is coming) from a detect-only one
// (footage is never coming).
func newSearchWithConfigs(obsRows []*entities.ObjectObservation, alertRows []*entities.AlertEvent, cfgs []*entities.RecordingConfig) (*SightingSearch, *searchObsRepo, *searchAlertRepo) {
	obsRepo := &searchObsRepo{rows: obsRows}
	alertRepo := &searchAlertRepo{rows: alertRows}
	cams := &searchCameraRepo{rows: []*entities.Camera{{Id: 1, Name: "Front Gate"}, {Id: 2, Name: "Loading Bay"}}}
	observations := NewObservationService(obsRepo, noSegments{configs: cfgs})
	return NewSightingSearch(observations, alertRepo, cams), obsRepo, alertRepo
}

// TestSearchObjectsKeepsFootagelessSightings is the deliberate divergence from the node's
// own Objects grid, and the reason it exists. The grid hides sightings with no playable
// footage so it does not offer un-openable rows; a FLEET search must not, because a
// detect-only camera is often the only thing that saw the vehicle, and dropping it would
// answer "never seen here" to an investigator.
func TestSearchObjectsKeepsFootagelessSightings(t *testing.T) {
	search, _, _ := newSearch([]*entities.ObjectObservation{
		obs(1, 1, 500, "person"),
		obs(2, 2, 400, "car"),
	}, nil)

	page, err := search.SearchObjects(context.Background(), SightingQuery{})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(page.Items) != 2 {
		t.Fatalf("items = %d, want both sightings even with no footage", len(page.Items))
	}
	if page.Items[0].CameraName != "Front Gate" {
		t.Fatalf("camera name = %q, want the node's own label — an id alone is meaningless across a fleet", page.Items[0].CameraName)
	}
	if page.Items[0].SegmentId != 0 || page.Items[0].FootagePending {
		t.Fatalf("with no camera recording, a sighting has no footage and none is coming; got segment=%d pending=%v",
			page.Items[0].SegmentId, page.Items[0].FootagePending)
	}
}

// TestSearchObjectsDistinguishesPendingFootageFromNone. Both cases have no segment to
// play, and until a live UI check they looked the same: a detect-only camera keeps no
// footage at all, so every one of its sightings is "newer than its newest footage" and was
// labelled "Recording…" forever — promising a clip that is never coming. A promise the
// system cannot keep is worse than an honest blank.
func TestSearchObjectsDistinguishesPendingFootageFromNone(t *testing.T) {
	rows := []*entities.ObjectObservation{obs(1, 1, 900, "person"), obs(2, 2, 800, "car")}
	// Camera 1 records; camera 2 is detect-only (absent from the configs).
	search, _, _ := newSearchWithConfigs(rows, nil, []*entities.RecordingConfig{
		{CameraId: 1, Enabled: true},
		{CameraId: 2, Enabled: false},
	})

	page, err := search.SearchObjects(context.Background(), SightingQuery{})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(page.Items) != 2 {
		t.Fatalf("items = %d, want both — neither camera's sighting may be dropped", len(page.Items))
	}
	if !page.Items[0].FootagePending {
		t.Error("a sighting on a RECORDING camera, newer than its saved footage, is still being written")
	}
	if page.Items[1].FootagePending {
		t.Error("a sighting on a detect-only camera must not claim footage is on its way — none ever will be")
	}
}

// TestSearchObjectsFailsOpenWhenRecordingConfigIsUnreadable. Deciding "there is no footage
// and there never will be" requires knowing which cameras record. When that cannot be read,
// the honest answer is the softer one: say footage may still be coming rather than assert an
// absence nothing verified. Asserting the absence would tell an investigator the clip does
// not exist because a config read failed.
func TestSearchObjectsFailsOpenWhenRecordingConfigIsUnreadable(t *testing.T) {
	obsRepo := &searchObsRepo{rows: []*entities.ObjectObservation{obs(1, 1, 900, "person")}}
	cams := &searchCameraRepo{rows: []*entities.Camera{{Id: 1, Name: "Front Gate"}}}
	observations := NewObservationService(obsRepo, noSegments{configErr: true})
	search := NewSightingSearch(observations, &searchAlertRepo{}, cams)

	page, err := search.SearchObjects(context.Background(), SightingQuery{})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("items = %d, want the sighting despite the config read failing", len(page.Items))
	}
	if !page.Items[0].FootagePending {
		t.Fatal("with recording state unknown, the sighting must not be declared footage-less")
	}
}

// TestSearchObjectsDeclaresItsCap covers the contract the control plane's completeness
// horizon is built on: a node that had more matches than it could return says so, and says
// how far back the prefix it DID return reaches.
func TestSearchObjectsDeclaresItsCap(t *testing.T) {
	rows := []*entities.ObjectObservation{obs(1, 1, 900, "person"), obs(2, 1, 800, "person"), obs(3, 1, 700, "person")}
	search, _, _ := newSearch(rows, nil)

	page, err := search.SearchObjects(context.Background(), SightingQuery{Limit: 2})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(page.Items) != 2 {
		t.Fatalf("items = %d, want the limit", len(page.Items))
	}
	if !page.Capped {
		t.Fatal("a node with more matches than the limit must declare the cap — silence reads as the whole truth")
	}
	if page.Oldest != 800 {
		t.Fatalf("oldest = %d, want 800 (how far back the returned prefix reaches)", page.Oldest)
	}

	// Exactly as many rows as the limit is NOT a cap: there was nothing left behind.
	page, err = search.SearchObjects(context.Background(), SightingQuery{Limit: 3})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if page.Capped {
		t.Fatal("a page that happens to fill the limit exactly is complete, not capped")
	}
}

// TestSearchObjectsBuildsTheRequestedFilters checks the query terms reach the repository —
// a search that silently ignores its time window would return the right-looking rows for
// the wrong period.
func TestSearchObjectsBuildsTheRequestedFilters(t *testing.T) {
	search, obsRepo, _ := newSearch([]*entities.ObjectObservation{obs(1, 1, 500, "person")}, nil)
	if _, err := search.SearchObjects(context.Background(), SightingQuery{
		From: 100, To: 900, Labels: []string{"car", "person", "car"}, MinConfidence: 0.6,
	}); err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(obsRepo.lastFilters) != 4 {
		t.Fatalf("filters = %d (%+v), want from, to, labels and confidence", len(obsRepo.lastFilters), obsRepo.lastFilters)
	}
	for _, f := range obsRepo.lastFilters {
		if f.FieldName == "Label" {
			labels, ok := f.Value.([]string)
			if !ok || len(labels) != 2 || labels[0] != "car" || labels[1] != "person" {
				t.Fatalf("label filter = %#v, want the de-duplicated sorted set", f.Value)
			}
			if f.Compare != sqldataenums.In {
				t.Fatalf("label filter compare = %v, want In", f.Compare)
			}
		}
	}
}

// TestSearchIdentitiesFindsPlatesAndFaces is the F-10 query term this feature exists for.
func TestSearchIdentitiesFindsPlatesAndFaces(t *testing.T) {
	search, _, _ := newSearch(nil, []*entities.AlertEvent{
		plateAlert(1, 1, 900, "WXY1234", "white", "car"),
		faceAlert(2, 2, 800, "Alice"),
		{Id: 3, CameraId: 1, Label: "person", CreatedAt: 700}, // no identity at all
	})

	page, err := search.SearchIdentities(context.Background(), SightingQuery{})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(page.Items) != 2 {
		t.Fatalf("items = %d, want the plate and the face (the bare object alert is not an identity)", len(page.Items))
	}
	if page.Items[0].Identity != "WXY1234" || page.Items[0].IdentityKind != "plate" {
		t.Fatalf("first hit = %+v, want the plate", page.Items[0])
	}
	if page.Items[0].CameraName != "Front Gate" {
		t.Fatalf("identity hit camera = %q, want the node's label", page.Items[0].CameraName)
	}
	if page.Items[1].Identity != "Alice" || page.Items[1].IdentityKind != "face" {
		t.Fatalf("second hit = %+v, want the recognized face", page.Items[1])
	}
}

// TestSearchIdentitiesMatchesPlateLabelAndDescriptor covers what an operator actually
// types: a partial plate, a person's name, or the descriptor they read off an alert.
func TestSearchIdentitiesMatchesPlateLabelAndDescriptor(t *testing.T) {
	alerts := []*entities.AlertEvent{
		plateAlert(1, 1, 900, "WXY1234", "white", "car"),
		faceAlert(2, 2, 800, "Alice"),
	}
	for _, tc := range []struct {
		text string
		want int64
	}{
		{"wxy", 1},       // partial plate, wrong case
		{"WXY1234", 1},   // the whole plate
		{"white car", 1}, // the descriptor, which is only in the label and detail
		{"alice", 2},     // person name, wrong case
	} {
		search, _, _ := newSearch(nil, alerts)
		page, err := search.SearchIdentities(context.Background(), SightingQuery{Text: tc.text})
		if err != nil {
			t.Fatalf("search %q: %v", tc.text, err)
		}
		if len(page.Items) != 1 || page.Items[0].Id != tc.want {
			t.Fatalf("search %q matched %+v, want exactly alert %d", tc.text, page.Items, tc.want)
		}
	}

	search, _, _ := newSearch(nil, alerts)
	page, err := search.SearchIdentities(context.Background(), SightingQuery{Text: "nothing like this"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(page.Items) != 0 {
		t.Fatalf("a non-matching search returned %d rows", len(page.Items))
	}
}

// TestSearchIdentitiesIgnoresUnrecognizedFaces. "Unknown face" is a real alert but names
// nobody; returning it from an identity search would put rows in front of an operator that
// can never match what they typed, and make a fruitless search look productive.
func TestSearchIdentitiesIgnoresUnrecognizedFaces(t *testing.T) {
	meta, _ := json.Marshal(map[string]any{"personName": "", "matchConfidence": 0.2})
	search, _, _ := newSearch(nil, []*entities.AlertEvent{
		{Id: 1, CameraId: 1, Label: "Unknown face", Metadata: string(meta), CreatedAt: 900},
	})
	page, err := search.SearchIdentities(context.Background(), SightingQuery{})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(page.Items) != 0 {
		t.Fatalf("an unnamed face was returned as an identity: %+v", page.Items)
	}
}

// TestSearchIdentitiesNarrowsByKind.
func TestSearchIdentitiesNarrowsByKind(t *testing.T) {
	alerts := []*entities.AlertEvent{plateAlert(1, 1, 900, "WXY1234", "white", "car"), faceAlert(2, 2, 800, "Alice")}
	search, _, _ := newSearch(nil, alerts)
	page, err := search.SearchIdentities(context.Background(), SightingQuery{IdentityKinds: []string{"face"}})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].IdentityKind != "face" {
		t.Fatalf("kind filter returned %+v, want only the face", page.Items)
	}
}

// TestSearchIdentitiesExcludesDiagnosticsAtTheQuery. Diagnostics are the bulk of the alert
// table; scanning past them instead of excluding them would burn the scan bound and report
// a cap on a fleet that had barely any real alerts.
func TestSearchIdentitiesExcludesDiagnosticsAtTheQuery(t *testing.T) {
	search, _, alertRepo := newSearch(nil, []*entities.AlertEvent{plateAlert(1, 1, 900, "WXY1234", "white", "car")})
	if _, err := search.SearchIdentities(context.Background(), SightingQuery{From: 100, To: 900}); err != nil {
		t.Fatalf("search: %v", err)
	}
	found := false
	for _, f := range alertRepo.lastFilters {
		if f.FieldName == "IsDiagnostic" && f.Compare == sqldataenums.Equal && f.Value == false {
			found = true
		}
	}
	if !found {
		t.Fatalf("identity search did not exclude diagnostics: %+v", alertRepo.lastFilters)
	}
}

// TestSearchIdentitiesDeclaresItsCap.
func TestSearchIdentitiesDeclaresItsCap(t *testing.T) {
	search, _, _ := newSearch(nil, []*entities.AlertEvent{
		plateAlert(1, 1, 900, "AAA111", "white", "car"),
		plateAlert(2, 1, 800, "BBB222", "black", "van"),
		plateAlert(3, 1, 700, "CCC333", "red", "car"),
	})
	page, err := search.SearchIdentities(context.Background(), SightingQuery{Limit: 2})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(page.Items) != 2 || !page.Capped {
		t.Fatalf("items=%d capped=%v, want 2/true", len(page.Items), page.Capped)
	}
	if page.Oldest != 800 {
		t.Fatalf("oldest = %d, want 800", page.Oldest)
	}
}

// TestIdentityKindWantedDefaultsToBoth guards the typo case: a request naming an unknown
// kind must not silently return nothing.
func TestIdentityKindWantedDefaultsToBoth(t *testing.T) {
	for _, in := range [][]string{nil, {}, {" "}, {"plaet"}} {
		plate, face := identityKindWanted(in)
		if !plate || !face {
			t.Fatalf("kinds %v resolved to plate=%v face=%v, want both", in, plate, face)
		}
	}
	if plate, face := identityKindWanted([]string{"plate"}); !plate || face {
		t.Fatal("an explicit kind must narrow the search")
	}
}

// TestAlertIdentityIgnoresUnparseableMetadata. Metadata is free-form JSON written by
// several detectors; a malformed blob must not take down a search.
func TestAlertIdentityIgnoresUnparseableMetadata(t *testing.T) {
	kind, _, _ := alertIdentity(&entities.AlertEvent{Metadata: "{not json"})
	if kind != "" {
		t.Fatalf("unparseable metadata yielded kind %q, want none", kind)
	}
	kind, identity, detail := alertIdentity(&entities.AlertEvent{Metadata: `{"plate":" WXY1234 ","color":"white","vehicleType":"car"}`})
	if kind != "plate" || identity != "WXY1234" || detail != "white car" {
		t.Fatalf("plate metadata parsed to %q/%q/%q", kind, identity, detail)
	}
}

// TestClampSearchLimitBounds keeps one node from being asked for an unbounded page — the
// answer crosses the control channel as a single body.
func TestClampSearchLimitBounds(t *testing.T) {
	if got := clampSearchLimit(0); got != defaultSearchLimit {
		t.Fatalf("clamp(0) = %d, want the default", got)
	}
	if got := clampSearchLimit(maxSearchLimit + 5000); got != maxSearchLimit {
		t.Fatalf("clamp(huge) = %d, want the ceiling", got)
	}
	if got := clampSearchLimit(37); got != 37 {
		t.Fatalf("clamp(37) = %d, want it untouched", got)
	}
}
