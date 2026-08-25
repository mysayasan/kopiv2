package services

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	"github.com/mysayasan/kopiv2/infra/onvif"
	"github.com/mysayasan/kopiv2/infra/recording"
)

// --- fakes ---------------------------------------------------------------------------

type fakeStandbyRepo struct {
	dbsql.IGenericRepo[entities.StandbyCamera]
	rows []*entities.StandbyCamera
	seq  int64
}

func (f *fakeStandbyRepo) Get(_ context.Context, _ string, _, _ uint64, filters []sqldataenums.Filter, _ []sqldataenums.Sorter) ([]*entities.StandbyCamera, uint64, error) {
	want := ""
	for _, flt := range filters {
		if flt.FieldName == "SourceNodeId" {
			want, _ = flt.Value.(string)
		}
	}
	out := []*entities.StandbyCamera{}
	for _, row := range f.rows {
		if want != "" && row.SourceNodeId != want {
			continue
		}
		cp := *row
		out = append(out, &cp)
	}
	return out, uint64(len(out)), nil
}

func (f *fakeStandbyRepo) Create(_ context.Context, _ string, model entities.StandbyCamera) (uint64, error) {
	f.seq++
	model.Id = f.seq
	f.rows = append(f.rows, &model)
	return uint64(model.Id), nil
}

func (f *fakeStandbyRepo) UpdateById(_ context.Context, _ string, model entities.StandbyCamera) (uint64, error) {
	for i, row := range f.rows {
		if row.Id == model.Id {
			cp := model
			f.rows[i] = &cp
			return 1, nil
		}
	}
	return 0, errors.New("no result found")
}

func (f *fakeStandbyRepo) DeleteById(_ context.Context, _ string, id uint64) (uint64, error) {
	for i, row := range f.rows {
		if row.Id == int64(id) {
			f.rows = append(f.rows[:i], f.rows[i+1:]...)
			return 1, nil
		}
	}
	return 0, nil
}

type fakeStandbyCameras struct {
	cams []*CameraDetail
	seq  int64
	// saved records every Save, so a test can assert that STAGING created nothing.
	saved []CameraDetail
	// verdict maps a camera host to what a probe of it should return.
	verdict  map[string]string
	saveFail map[string]bool
}

func (f *fakeStandbyCameras) Get(_ context.Context, _, _ uint64) ([]*CameraDetail, uint64, error) {
	return f.cams, uint64(len(f.cams)), nil
}

func (f *fakeStandbyCameras) Save(_ context.Context, detail CameraDetail) (uint64, error) {
	if f.saveFail[detail.Camera.Host] {
		return 0, errors.New("camera refused")
	}
	f.saved = append(f.saved, detail)
	if detail.Camera.Id != 0 {
		return uint64(detail.Camera.Id), nil
	}
	f.seq++
	return uint64(100 + f.seq), nil
}

func (f *fakeStandbyCameras) VerifyDeviceCredentials(_ context.Context, detail CameraDetail, _ onvif.Credentials) (string, error) {
	status, ok := f.verdict[detail.Camera.Host]
	if !ok {
		status = CameraAuthOK
	}
	if status == CameraAuthOK {
		return status, nil
	}
	return status, errors.New(status)
}

type fakeStandbyRecordings struct {
	configs map[int64]*entities.RecordingConfig
	saves   []SaveRecordingConfigRequest
}

func newFakeStandbyRecordings() *fakeStandbyRecordings {
	return &fakeStandbyRecordings{configs: map[int64]*entities.RecordingConfig{}}
}

func (f *fakeStandbyRecordings) GetConfig(_ context.Context, cameraId int64) (*entities.RecordingConfig, error) {
	return f.configs[cameraId], nil
}

func (f *fakeStandbyRecordings) SaveConfig(_ context.Context, req SaveRecordingConfigRequest) (*entities.RecordingConfig, error) {
	f.saves = append(f.saves, req)
	cfg := &entities.RecordingConfig{
		CameraId: req.CameraId, Enabled: req.Enabled,
		RetentionDays: req.RetentionDays, SegmentMinutes: req.SegmentMinutes,
	}
	f.configs[req.CameraId] = cfg
	return cfg, nil
}

type fakeStandbyRecorder struct {
	configured []recording.RecorderConfig
	// running decides what Statuses() reports per camera. A camera absent from it is a
	// recorder that never started — the case the read-back exists to catch.
	running map[int64]bool
	// liveFiles is the segment count on disk. A camera that is `running` with zero files is
	// the case the SCREEN PASS found: ffmpeg is alive and retrying against a host this
	// appliance cannot resolve, and calling that "recording" contradicts the drill row
	// immediately above it.
	liveFiles map[int64]int
	lastErr   map[int64]string
}

func (f *fakeStandbyRecorder) Configure(cfg recording.RecorderConfig) error {
	f.configured = append(f.configured, cfg)
	return nil
}

func (f *fakeStandbyRecorder) Statuses() []recording.CameraStatus {
	out := []recording.CameraStatus{}
	for id, ok := range f.running {
		out = append(out, recording.CameraStatus{
			CameraId: id, FFmpegRunning: ok, State: "error",
			LiveFiles: f.liveFiles[id], LastError: f.lastErr[id],
		})
	}
	return out
}

type fakeStandbyRecorderConfig struct{}

func (fakeStandbyRecorderConfig) ForRecording(_ context.Context, cfg *entities.RecordingConfig) (recording.RecorderConfig, string) {
	return recording.RecorderConfig{CameraId: cfg.CameraId, Enabled: cfg.Enabled, RTSPURI: "rtsp://x"}, ""
}

type fakeStandbyIdentity struct {
	id   string
	name string
}

func (f fakeStandbyIdentity) Status(context.Context) (PairingStatus, error) {
	return PairingStatus{NodeID: f.id, Name: f.name}, nil
}

// --- harness -------------------------------------------------------------------------

type standbyRig struct {
	svc        IStandbyService
	repo       *fakeStandbyRepo
	cameras    *fakeStandbyCameras
	recordings *fakeStandbyRecordings
	recorder   *fakeStandbyRecorder
}

func newStandbyRig(nodeId, nodeName string, cams []*CameraDetail) *standbyRig {
	repo := &fakeStandbyRepo{}
	cameras := &fakeStandbyCameras{cams: cams, verdict: map[string]string{}, saveFail: map[string]bool{}}
	recordings := newFakeStandbyRecordings()
	recorder := &fakeStandbyRecorder{
		running: map[int64]bool{}, liveFiles: map[int64]int{}, lastErr: map[int64]string{},
	}
	svc := NewStandbyService(repo, cameras, recordings, recorder, fakeStandbyRecorderConfig{},
		fakeStandbyIdentity{id: nodeId, name: nodeName}, nil, nil)
	// The shipped settle window is twelve seconds. Paying it in every activation test would
	// be twelve seconds spent asserting something none of them is about.
	svc.(*standbyService).settleFor = 120 * time.Millisecond
	return &standbyRig{svc: svc, repo: repo, cameras: cameras, recordings: recordings, recorder: recorder}
}

func sourceCameras() []*CameraDetail {
	return []*CameraDetail{
		{Camera: entities.Camera{Id: 1, Name: "Lobby", Host: "10.0.0.1", RTSPUrl: "rtsp://10.0.0.1/s"},
			XAddr: "http://10.0.0.1/onvif", Username: "admin", Password: "lobby-secret"},
		{Camera: entities.Camera{Id: 2, Name: "Yard", Host: "10.0.0.2", RTSPUrl: "rtsp://10.0.0.2/s"},
			XAddr: "http://10.0.0.2/onvif", Username: "admin", Password: "yard-secret"},
	}
}

// handoffTo runs the real three-step exchange between two rigs and returns the staged set.
func handoffTo(t *testing.T, source, spare *standbyRig, spareNodeId string) *StandbySet {
	t.Helper()
	ctx := context.Background()
	key, err := spare.svc.HandoffKey(ctx)
	if err != nil {
		t.Fatalf("handoff key: %v", err)
	}
	sealed, err := source.svc.Handoff(ctx, StandbyHandoffRequest{RecipientNodeId: spareNodeId, PublicKey: key.PublicKey})
	if err != nil {
		t.Fatalf("handoff: %v", err)
	}
	set, err := spare.svc.Stage(ctx, StandbyStageRequest{Sealed: sealed.Sealed})
	if err != nil {
		t.Fatalf("stage: %v", err)
	}
	return set
}

// --- tests ---------------------------------------------------------------------------

func TestStandbyHandoffAndStage(t *testing.T) {
	source := newStandbyRig("node-a", "Site A", sourceCameras())
	source.recordings.configs[1] = &entities.RecordingConfig{CameraId: 1, Enabled: true, RetentionDays: 30}
	source.recordings.configs[2] = &entities.RecordingConfig{CameraId: 2, Enabled: false}
	spare := newStandbyRig("node-b", "Spare", nil)

	set := handoffTo(t, source, spare, "node-b")

	if set.SourceNodeId != "node-a" || set.SourceNodeName != "Site A" {
		t.Fatalf("staged set names the wrong source: %+v", set)
	}
	if len(set.Cameras) != 2 {
		t.Fatalf("expected 2 staged cameras, got %d", len(set.Cameras))
	}
	// A set nobody has drilled must never read as ready. It is the same rule the fleet
	// policy reconciler follows for an unreachable node: absence of evidence gets its own
	// colour, because a green failover plan that has never been tested is worse than none.
	if set.Readiness != StandbyReadinessUntested {
		t.Fatalf("a freshly staged set reported readiness %q", set.Readiness)
	}
	// The recording INTENT travels: camera 2 was not being recorded on the source, so it
	// must not come up recording here.
	byName := map[string]StandbyCameraView{}
	for _, c := range set.Cameras {
		byName[c.Name] = c
	}
	if !byName["Lobby"].RecordingWanted {
		t.Fatal("a camera the source was recording arrived not wanted")
	}
	if byName["Yard"].RecordingWanted {
		t.Fatal("a camera the source had switched off arrived wanted")
	}
	// The credential is held, encrypted or not, but never handed back out.
	for _, row := range spare.repo.rows {
		if row.Password == "" {
			t.Fatalf("camera %q was staged without its credentials — it could never be opened", row.Name)
		}
	}
}

// Staging must not create cameras. A spare covering four recorders would otherwise show
// four sites' worth of cameras it is not watching, in a control room, permanently.
func TestStandbyStagingCreatesNoCameras(t *testing.T) {
	source := newStandbyRig("node-a", "Site A", sourceCameras())
	spare := newStandbyRig("node-b", "Spare", nil)

	handoffTo(t, source, spare, "node-b")

	if len(spare.cameras.saved) != 0 {
		t.Fatalf("staging created %d camera(s) on the spare", len(spare.cameras.saved))
	}
	if len(spare.recordings.saves) != 0 {
		t.Fatalf("staging wrote %d recording config(s) on the spare", len(spare.recordings.saves))
	}
}

// A bundle sealed for one spare must not open on another, even though both are running the
// same software and both are legitimately part of the fleet.
func TestStandbyStageRefusesABundleForSomebodyElse(t *testing.T) {
	ctx := context.Background()
	source := newStandbyRig("node-a", "Site A", sourceCameras())
	spare := newStandbyRig("node-b", "Spare B", nil)
	other := newStandbyRig("node-c", "Spare C", nil)

	key, _ := spare.svc.HandoffKey(ctx)
	sealed, err := source.svc.Handoff(ctx, StandbyHandoffRequest{RecipientNodeId: "node-b", PublicKey: key.PublicKey})
	if err != nil {
		t.Fatalf("handoff: %v", err)
	}
	// node-c mints its own key (so it HAS one) and is then handed node-b's bundle.
	if _, err := other.svc.HandoffKey(ctx); err != nil {
		t.Fatalf("key: %v", err)
	}
	if _, err := other.svc.Stage(ctx, StandbyStageRequest{Sealed: sealed.Sealed}); err == nil {
		t.Fatal("an appliance staged a camera set sealed for a different appliance")
	}
	if len(other.repo.rows) != 0 {
		t.Fatal("a refused bundle still left rows behind")
	}
}

func TestStandbyRefusesToStandByForItself(t *testing.T) {
	ctx := context.Background()
	rig := newStandbyRig("node-a", "Site A", sourceCameras())
	key, _ := rig.svc.HandoffKey(ctx)
	if _, err := rig.svc.Handoff(ctx, StandbyHandoffRequest{RecipientNodeId: "node-a", PublicKey: key.PublicKey}); err == nil {
		t.Fatal("an appliance sealed its camera set for itself")
	}
}

func TestStandbyDrillReadiness(t *testing.T) {
	ctx := context.Background()
	source := newStandbyRig("node-a", "Site A", sourceCameras())
	spare := newStandbyRig("node-b", "Spare", nil)
	handoffTo(t, source, spare, "node-b")

	// Everything answers.
	set, err := spare.svc.Drill(ctx, "node-a")
	if err != nil {
		t.Fatalf("drill: %v", err)
	}
	if set.Readiness != StandbyReadinessReady || set.Reachable != 2 {
		t.Fatalf("a fully reachable set reported %q (%d/%d)", set.Readiness, set.Reachable, set.Total)
	}

	// One camera rejects the login: partial, and the verdict says WHY per camera.
	spare.cameras.verdict["10.0.0.2"] = CameraAuthUnauthorized
	set, _ = spare.svc.Drill(ctx, "node-a")
	if set.Readiness != StandbyReadinessPartial || set.Reachable != 1 {
		t.Fatalf("a partly reachable set reported %q (%d/%d)", set.Readiness, set.Reachable, set.Total)
	}
	for _, c := range set.Cameras {
		if c.Name == "Yard" && c.CheckStatus != entities.StandbyCheckUnauthorized {
			t.Fatalf("a camera that rejected the login reported %q", c.CheckStatus)
		}
	}

	// Nothing answers: BLIND, not partial. The distinction is what sends somebody to look
	// at the network instead of at three cameras.
	spare.cameras.verdict["10.0.0.1"] = CameraAuthUnreachable
	spare.cameras.verdict["10.0.0.2"] = CameraAuthUnreachable
	set, _ = spare.svc.Drill(ctx, "node-a")
	if set.Readiness != StandbyReadinessBlind {
		t.Fatalf("a set where nothing answered reported %q", set.Readiness)
	}
}

// The load-bearing assertion of the whole feature: a takeover reports what the RECORDER is
// doing, not what the database was told. A config row that says "enabled" next to no ffmpeg
// process is exactly the failure this exists to prevent, and it is invisible to every check
// that stops at the 200.
func TestStandbyActivateReadsBackTheRecorder(t *testing.T) {
	ctx := context.Background()
	source := newStandbyRig("node-a", "Site A", sourceCameras())
	source.recordings.configs[1] = &entities.RecordingConfig{CameraId: 1, Enabled: true, RetentionDays: 30}
	source.recordings.configs[2] = &entities.RecordingConfig{CameraId: 2, Enabled: true, RetentionDays: 30}
	spare := newStandbyRig("node-b", "Spare", nil)
	handoffTo(t, source, spare, "node-b")

	// The first camera's recorder starts; the second's does not.
	spare.recorder.running[101] = true
	spare.recorder.liveFiles[101] = 2
	spare.recorder.running[102] = false
	spare.recorder.lastErr[102] = "connection refused"

	set, err := spare.svc.Activate(ctx, "node-a")
	if err != nil {
		t.Fatalf("activate: %v", err)
	}
	if len(spare.cameras.saved) != 2 {
		t.Fatalf("activation created %d camera(s), expected 2", len(spare.cameras.saved))
	}
	byName := map[string]StandbyCameraView{}
	for _, c := range set.Cameras {
		byName[c.Name] = c
	}
	if byName["Lobby"].Outcome != StandbyOutcomeRecording {
		t.Fatalf("a camera whose recorder started reported %q", byName["Lobby"].Outcome)
	}
	// The CODE says what happened; the DETAIL carries the machine's own words. Both matter:
	// a code with no reason sends nobody anywhere, and a sentence composed here would reach
	// an Arabic screen in English.
	if byName["Yard"].Outcome != StandbyOutcomeNotRecording ||
		!strings.Contains(byName["Yard"].OutcomeDetail, "connection refused") {
		t.Fatalf("a camera whose recorder never started reported %q / %q",
			byName["Yard"].Outcome, byName["Yard"].OutcomeDetail)
	}
	if set.State != entities.StandbyStateActive || set.Readiness != StandbyReadinessActive {
		t.Fatalf("after a takeover the set reads %q/%q", set.State, set.Readiness)
	}
}

// A camera the failed appliance was NOT recording must not start recording here. Taking
// over continues what was happening; it does not quietly change the site's policy.
func TestStandbyActivateHonoursTheSourcesRecordingIntent(t *testing.T) {
	ctx := context.Background()
	source := newStandbyRig("node-a", "Site A", sourceCameras())
	source.recordings.configs[1] = &entities.RecordingConfig{CameraId: 1, Enabled: true}
	spare := newStandbyRig("node-b", "Spare", nil)
	handoffTo(t, source, spare, "node-b")
	spare.recorder.running[101] = true

	if _, err := spare.svc.Activate(ctx, "node-a"); err != nil {
		t.Fatalf("activate: %v", err)
	}
	for _, save := range spare.recordings.saves {
		if save.Enabled && save.CameraId != 101 {
			t.Fatalf("camera %d was set to record although the source was not recording it", save.CameraId)
		}
	}
}

// Failing back stops recording and KEEPS the camera. The footage recorded during the
// outage hangs off that camera row; removing it would take the footage with it.
func TestStandbyReleaseKeepsTheCameraAndItsFootage(t *testing.T) {
	ctx := context.Background()
	source := newStandbyRig("node-a", "Site A", sourceCameras())
	source.recordings.configs[1] = &entities.RecordingConfig{CameraId: 1, Enabled: true}
	source.recordings.configs[2] = &entities.RecordingConfig{CameraId: 2, Enabled: true}
	spare := newStandbyRig("node-b", "Spare", nil)
	handoffTo(t, source, spare, "node-b")
	spare.recorder.running[101] = true
	spare.recorder.liveFiles[101] = 1
	spare.recorder.running[102] = true
	spare.recorder.liveFiles[102] = 1
	if _, err := spare.svc.Activate(ctx, "node-a"); err != nil {
		t.Fatalf("activate: %v", err)
	}

	set, err := spare.svc.Release(ctx, "node-a")
	if err != nil {
		t.Fatalf("release: %v", err)
	}
	if set.State != entities.StandbyStateReleased {
		t.Fatalf("after a fail-back the set reads %q", set.State)
	}
	for _, row := range spare.repo.rows {
		if row.LocalCameraId == 0 {
			t.Fatalf("camera %q lost the link to the row holding its footage", row.Name)
		}
	}
	// Recording was actually turned off — both in the config and at the recorder, or the
	// duplicate stream the fail-back was performed to end keeps running.
	last := spare.recordings.saves[len(spare.recordings.saves)-1]
	if last.Enabled {
		t.Fatal("the last recording config written by a fail-back still had recording on")
	}
	stopped := 0
	for _, cfg := range spare.recorder.configured {
		if !cfg.Enabled {
			stopped++
		}
	}
	if stopped != 2 {
		t.Fatalf("the recorder was told to stop %d time(s), expected 2", stopped)
	}
}

// Re-staging is how a spare tracks a site that gains and loses cameras. It must not
// discard what THIS appliance has already done with the set.
func TestStandbyRestagePreservesLocalState(t *testing.T) {
	ctx := context.Background()
	source := newStandbyRig("node-a", "Site A", sourceCameras())
	source.recordings.configs[1] = &entities.RecordingConfig{CameraId: 1, Enabled: true}
	spare := newStandbyRig("node-b", "Spare", nil)
	handoffTo(t, source, spare, "node-b")
	spare.recorder.running[101] = true
	spare.recorder.liveFiles[101] = 1
	if _, err := spare.svc.Activate(ctx, "node-a"); err != nil {
		t.Fatalf("activate: %v", err)
	}
	local := map[int64]int64{}
	for _, row := range spare.repo.rows {
		local[row.SourceCameraId] = row.LocalCameraId
	}

	handoffTo(t, source, spare, "node-b")

	for _, row := range spare.repo.rows {
		if row.LocalCameraId != local[row.SourceCameraId] {
			t.Fatalf("re-staging moved camera %d from local %d to %d",
				row.SourceCameraId, local[row.SourceCameraId], row.LocalCameraId)
		}
		if row.State != entities.StandbyStateActive {
			t.Fatalf("re-staging knocked camera %d out of the active state into %q",
				row.SourceCameraId, row.State)
		}
	}
}

// A camera the source has since removed stops being staged — but not one this appliance
// has already materialized, because that row is the only link between a local camera (with
// its footage) and where it came from.
func TestStandbyRestageDropsRemovedCamerasButNotMaterializedOnes(t *testing.T) {
	ctx := context.Background()
	cams := sourceCameras()
	source := newStandbyRig("node-a", "Site A", cams)
	spare := newStandbyRig("node-b", "Spare", nil)
	handoffTo(t, source, spare, "node-b")

	// The yard camera is decommissioned at the source.
	source.cameras.cams = cams[:1]
	handoffTo(t, source, spare, "node-b")
	if len(spare.repo.rows) != 1 {
		t.Fatalf("a decommissioned camera is still staged (%d rows)", len(spare.repo.rows))
	}

	// Now do it again, but with the camera already taken over.
	source.cameras.cams = cams
	source.recordings.configs[2] = &entities.RecordingConfig{CameraId: 2, Enabled: true}
	handoffTo(t, source, spare, "node-b")
	spare.recorder.running[101] = true
	spare.recorder.liveFiles[101] = 1
	spare.recorder.running[102] = true
	spare.recorder.liveFiles[102] = 1
	if _, err := spare.svc.Activate(ctx, "node-a"); err != nil {
		t.Fatalf("activate: %v", err)
	}
	source.cameras.cams = cams[:1]
	handoffTo(t, source, spare, "node-b")
	if len(spare.repo.rows) != 2 {
		t.Fatalf("a materialized camera was dropped on re-stage (%d rows)", len(spare.repo.rows))
	}
}

func TestStandbyForgetRefusesWhileActive(t *testing.T) {
	ctx := context.Background()
	source := newStandbyRig("node-a", "Site A", sourceCameras())
	source.recordings.configs[1] = &entities.RecordingConfig{CameraId: 1, Enabled: true}
	spare := newStandbyRig("node-b", "Spare", nil)
	handoffTo(t, source, spare, "node-b")
	spare.recorder.running[101] = true
	spare.recorder.liveFiles[101] = 1
	if _, err := spare.svc.Activate(ctx, "node-a"); err != nil {
		t.Fatalf("activate: %v", err)
	}
	if err := spare.svc.Forget(ctx, "node-a"); err == nil {
		t.Fatal("a set being recorded right now was forgotten")
	}
}

func TestStandbyUnknownSetIsDistinguishable(t *testing.T) {
	spare := newStandbyRig("node-b", "Spare", nil)
	if _, err := spare.svc.Drill(context.Background(), "node-z"); !errors.Is(err, ErrStandbyNoSuchSet) {
		t.Fatalf("drilling an unstaged appliance returned %v", err)
	}
}

// A camera that cannot be created here must not be reported as taken over, and must not
// stop the rest of the set from being taken over — the whole point of a takeover is the
// cameras it CAN cover.
func TestStandbyActivatePartialFailureIsReportedPerCamera(t *testing.T) {
	ctx := context.Background()
	source := newStandbyRig("node-a", "Site A", sourceCameras())
	source.recordings.configs[1] = &entities.RecordingConfig{CameraId: 1, Enabled: true}
	source.recordings.configs[2] = &entities.RecordingConfig{CameraId: 2, Enabled: true}
	spare := newStandbyRig("node-b", "Spare", nil)
	handoffTo(t, source, spare, "node-b")
	spare.cameras.saveFail["10.0.0.2"] = true
	spare.recorder.running[101] = true
	spare.recorder.liveFiles[101] = 1

	set, err := spare.svc.Activate(ctx, "node-a")
	if err != nil {
		t.Fatalf("activate: %v", err)
	}
	byName := map[string]StandbyCameraView{}
	for _, c := range set.Cameras {
		byName[c.Name] = c
	}
	if byName["Lobby"].Outcome != StandbyOutcomeRecording {
		t.Fatalf("the camera that could be taken over reported %q", byName["Lobby"].Outcome)
	}
	if byName["Yard"].Outcome != StandbyOutcomeCreateFailed ||
		!strings.Contains(byName["Yard"].OutcomeDetail, "refused") {
		t.Fatalf("the camera that could not be created reported %q / %q",
			byName["Yard"].Outcome, byName["Yard"].OutcomeDetail)
	}
	if byName["Yard"].State == entities.StandbyStateActive {
		t.Fatal("a camera that was never created is marked active")
	}
}

// THE DEFECT THE SCREEN PASS FOUND, kept as a unit test.
//
// A live ffmpeg process is not footage. Point a recorder at a host this appliance cannot
// resolve and the process is alive and retrying — so a takeover that asked the recorder
// immediately reported "recording" for a camera it had never reached, on the same card whose
// drill row said "could not be reached". A screen that says both things at once is worse
// than one that says nothing.
func TestStandbyActivateWillNotCallARetryingProcessRecording(t *testing.T) {
	ctx := context.Background()
	source := newStandbyRig("node-a", "Site A", sourceCameras())
	source.recordings.configs[1] = &entities.RecordingConfig{CameraId: 1, Enabled: true}
	spare := newStandbyRig("node-b", "Spare", nil)
	handoffTo(t, source, spare, "node-b")

	// The process is up. Nothing has reached the disk.
	spare.recorder.running[101] = true
	spare.recorder.liveFiles[101] = 0

	set, err := spare.svc.Activate(ctx, "node-a")
	if err != nil {
		t.Fatalf("activate: %v", err)
	}
	got := ""
	for _, c := range set.Cameras {
		if c.Name == "Lobby" {
			got = c.Outcome
		}
	}
	if got == StandbyOutcomeRecording {
		t.Fatal("a recorder that has written nothing was reported as recording")
	}
	if got != StandbyOutcomePending {
		t.Fatalf("expected the honest third answer, got %q", got)
	}
}

// ...and when the recorder does start writing, the takeover says so rather than hedging
// forever. The settle window is a bound on waiting, not a reason to never commit.
func TestStandbyActivateReportsRecordingOnceSomethingIsWritten(t *testing.T) {
	ctx := context.Background()
	source := newStandbyRig("node-a", "Site A", sourceCameras())
	source.recordings.configs[1] = &entities.RecordingConfig{CameraId: 1, Enabled: true}
	spare := newStandbyRig("node-b", "Spare", nil)
	handoffTo(t, source, spare, "node-b")
	spare.recorder.running[101] = true
	spare.recorder.liveFiles[101] = 1

	set, err := spare.svc.Activate(ctx, "node-a")
	if err != nil {
		t.Fatalf("activate: %v", err)
	}
	for _, c := range set.Cameras {
		if c.Name == "Lobby" && c.Outcome != StandbyOutcomeRecording {
			t.Fatalf("a recorder that is writing reported %q", c.Outcome)
		}
	}
}
