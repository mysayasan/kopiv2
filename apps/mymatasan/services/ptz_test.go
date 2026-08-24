package services

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	"github.com/mysayasan/kopiv2/domain/notification"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	"github.com/mysayasan/kopiv2/infra/onvif"
)

type fakeTourRepo struct {
	dbsql.IGenericRepo[entities.PtzTour]
	rows []*entities.PtzTour
	seq  int64
}

func (f *fakeTourRepo) Get(_ context.Context, _ string, limit, offset uint64, _ []sqldataenums.Filter, sorters []sqldataenums.Sorter) ([]*entities.PtzTour, uint64, error) {
	out := make([]*entities.PtzTour, 0, len(f.rows))
	for _, row := range f.rows {
		cp := *row
		out = append(out, &cp)
	}
	for _, s := range sorters {
		if s.FieldName == "CameraId" {
			sort.SliceStable(out, func(i, j int) bool { return out[i].CameraId < out[j].CameraId })
		}
	}
	return out, uint64(len(out)), nil
}

func (f *fakeTourRepo) GetById(_ context.Context, _ string, id uint64) (*entities.PtzTour, error) {
	for _, row := range f.rows {
		if row.Id == int64(id) {
			cp := *row
			return &cp, nil
		}
	}
	return nil, errors.New("no result found")
}

func (f *fakeTourRepo) Create(_ context.Context, _ string, model entities.PtzTour) (uint64, error) {
	f.seq++
	model.Id = f.seq
	f.rows = append(f.rows, &model)
	return uint64(model.Id), nil
}

func (f *fakeTourRepo) UpdateById(_ context.Context, _ string, model entities.PtzTour) (uint64, error) {
	for i, row := range f.rows {
		if row.Id == model.Id {
			cp := model
			f.rows[i] = &cp
			return 1, nil
		}
	}
	return 0, errors.New("no result found")
}

func (f *fakeTourRepo) DeleteById(_ context.Context, _ string, id uint64) (uint64, error) {
	for i, row := range f.rows {
		if row.Id == int64(id) {
			f.rows = append(f.rows[:i], f.rows[i+1:]...)
			return 1, nil
		}
	}
	return 0, nil
}

// fakePTZCamera is a camera that remembers where it was told to go.
type fakePTZCamera struct {
	presets []onvif.PTZPreset
	// unreachable makes the camera refuse to say what presets it has, which is a DIFFERENT
	// answer from "it has none" and the two must not be confused anywhere.
	unreachable bool
	moveErr     error
	visited     []string
	// duringPresetRead runs once, inside the preset read, to model something landing in
	// the gap between the runner deciding and the runner commanding.
	duringPresetRead func()
}

func (f *fakePTZCamera) PTZPresets(_ context.Context, _ uint64) ([]onvif.PTZPreset, error) {
	if f.unreachable {
		return nil, errors.New("camera did not answer")
	}
	// duringPresetRead stands in for the ONVIF round trip: anything a person or another
	// request can do WHILE the runner is asking the camera what presets it has.
	if f.duringPresetRead != nil {
		fn := f.duringPresetRead
		f.duringPresetRead = nil
		fn()
	}
	return f.presets, nil
}

func (f *fakePTZCamera) PTZGotoPreset(_ context.Context, _ uint64, token string, _ float64) error {
	if f.moveErr != nil {
		return f.moveErr
	}
	f.visited = append(f.visited, token)
	return nil
}

func (f *fakePTZCamera) DisplayName(_ context.Context, _ int64) string { return "Yard dome" }

type capturedNotifications struct {
	INotificationPublisher
	sent []notification.Notification
}

func (c *capturedNotifications) Publish(_ context.Context, n notification.Notification) notification.Notification {
	c.sent = append(c.sent, n)
	return n
}

func newTourService(t *testing.T, cam *fakePTZCamera) (*ptzService, *fakeTourRepo, *PTZJournal, *capturedNotifications, *int64) {
	t.Helper()
	repo := &fakeTourRepo{}
	journal := NewPTZJournal()
	notif := &capturedNotifications{}
	clock := int64(1_000_000)
	svc := NewPTZService(repo, cam, journal, notif).(*ptzService)
	svc.now = func() time.Time { return time.Unix(clock, 0).UTC() }
	journal.now = func() time.Time { return time.Unix(clock, 0).UTC() }
	return svc, repo, journal, notif, &clock
}

func threePresets() []onvif.PTZPreset {
	return []onvif.PTZPreset{
		{Token: "p1", Name: "Gate"},
		{Token: "p2", Name: "Yard"},
		{Token: "p3", Name: "Dock"},
	}
}

func TestSaveTourRefusesWhatCannotBeWalked(t *testing.T) {
	cam := &fakePTZCamera{presets: threePresets()}
	svc, _, _, _, _ := newTourService(t, cam)
	ctx := context.Background()

	cases := []struct {
		name string
		req  PTZTourSave
		want string
	}{
		{
			name: "no stops",
			req:  PTZTourSave{CameraId: 7, Name: "Empty", DwellSeconds: 15},
			want: "at least two stops",
		},
		{
			// One stop is a preset recall wearing a patrol's clothes. Refusing it at save
			// time is where somebody is reading the answer; a one-stop tour would otherwise
			// run forever and never move.
			name: "one stop",
			req: PTZTourSave{CameraId: 7, Name: "Single", DwellSeconds: 15,
				Stops: []PTZTourStop{{Preset: "p1"}}},
			want: "at least two stops",
		},
		{
			name: "a preset the camera does not have",
			req: PTZTourSave{CameraId: 7, Name: "Ghost", DwellSeconds: 15,
				Stops: []PTZTourStop{{Preset: "p1"}, {Preset: "nope"}}},
			want: "no preset",
		},
		{
			name: "a dwell too short to record anything",
			req: PTZTourSave{CameraId: 7, Name: "Strobe", DwellSeconds: 1,
				Stops: []PTZTourStop{{Preset: "p1"}, {Preset: "p2"}}},
			want: "seconds",
		},
		{
			name: "no name",
			req: PTZTourSave{CameraId: 7, DwellSeconds: 15,
				Stops: []PTZTourStop{{Preset: "p1"}, {Preset: "p2"}}},
			want: "needs a name",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.SaveTour(ctx, tc.req)
			if err == nil {
				t.Fatal("want a refusal")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("refusal should say %q, said %q", tc.want, err.Error())
			}
		})
	}
}

func TestSaveTourStillWorksWhenTheCameraIsDown(t *testing.T) {
	// A camera that cannot be asked what presets it has is not a camera whose presets are
	// gone. Refusing the save would make a tour uneditable for exactly as long as its
	// camera is offline — which is when somebody is most likely to be fixing it.
	cam := &fakePTZCamera{unreachable: true}
	svc, _, _, _, _ := newTourService(t, cam)
	tour, err := svc.SaveTour(context.Background(), PTZTourSave{
		CameraId: 7, Name: "Perimeter", DwellSeconds: 20,
		Stops: []PTZTourStop{{Preset: "p1"}, {Preset: "p2"}},
	})
	if err != nil {
		t.Fatalf("save should succeed while the camera is down: %v", err)
	}
	if !tour.PresetsUnavailable {
		t.Fatal("the view must say the presets could not be checked")
	}
	for _, stop := range tour.Stops {
		if stop.Missing {
			t.Fatal("an unreachable camera must not report its presets as missing")
		}
	}
}

func TestTourWalksItsRouteAndWraps(t *testing.T) {
	cam := &fakePTZCamera{presets: threePresets()}
	svc, _, _, _, clock := newTourService(t, cam)
	ctx := context.Background()

	tour, err := svc.SaveTour(ctx, PTZTourSave{
		CameraId: 7, Name: "Perimeter", DwellSeconds: 10,
		Stops: []PTZTourStop{{Preset: "p1"}, {Preset: "p2"}, {Preset: "p3", DwellSeconds: 30}},
	})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := svc.SetTourRunning(ctx, tour.Id, true); err != nil {
		t.Fatalf("start: %v", err)
	}

	// The first step is immediate: an operator who pressed start expects the camera to go
	// somewhere, not to wait out a dwell first.
	svc.Step(ctx)
	if got := cam.visited; len(got) != 1 || got[0] != "p1" {
		t.Fatalf("first step should be p1, got %v", got)
	}
	// Stepping again before the dwell has passed must do nothing.
	svc.Step(ctx)
	if len(cam.visited) != 1 {
		t.Fatalf("stepped early: %v", cam.visited)
	}
	*clock += 10
	svc.Step(ctx)
	*clock += 10
	svc.Step(ctx)
	if got := strings.Join(cam.visited, ","); got != "p1,p2,p3" {
		t.Fatalf("route wrong: %s", got)
	}
	// p3 carries its own 30s dwell, so the tour must not wrap after the tour default of 10.
	*clock += 10
	svc.Step(ctx)
	if len(cam.visited) != 3 {
		t.Fatalf("per-stop dwell ignored: %v", cam.visited)
	}
	*clock += 20
	svc.Step(ctx)
	if got := strings.Join(cam.visited, ","); got != "p1,p2,p3,p1" {
		t.Fatalf("tour should wrap to the first stop: %s", got)
	}
}

func TestOperatorHoldsTheCameraAgainstThePatrol(t *testing.T) {
	cam := &fakePTZCamera{presets: threePresets()}
	svc, _, journal, _, clock := newTourService(t, cam)
	ctx := context.Background()

	tour, _ := svc.SaveTour(ctx, PTZTourSave{
		CameraId: 7, Name: "Perimeter", DwellSeconds: 10,
		Stops: []PTZTourStop{{Preset: "p1"}, {Preset: "p2"}},
	})
	_, _ = svc.SetTourRunning(ctx, tour.Id, true)
	svc.Step(ctx)
	if len(cam.visited) != 1 {
		t.Fatalf("expected the first step: %v", cam.visited)
	}

	// Somebody takes the ring.
	journal.ClaimManual(7, 30*time.Second)
	*clock += 20
	svc.Step(ctx)
	if len(cam.visited) != 1 {
		t.Fatalf("the patrol stepped while an operator had the camera: %v", cam.visited)
	}

	// An alarm arriving while they hold it must not move the camera either — they are
	// tracking something the appliance cannot see.
	if err := svc.Recall(ctx, PTZRecallRequest{CameraId: 7, PresetToken: "p3", Reason: "Gate"}); err != nil {
		t.Fatalf("recall should not error, only defer: %v", err)
	}
	if len(cam.visited) != 1 {
		t.Fatalf("an alarm took the camera from an operator: %v", cam.visited)
	}

	// Once their claim lapses the patrol resumes promptly rather than waiting out another
	// full dwell it already served.
	*clock += 20
	svc.Step(ctx)
	if got := strings.Join(cam.visited, ","); got != "p1,p2" {
		t.Fatalf("patrol did not resume: %s", got)
	}
}

func TestRecallSuspendsThePatrolForItsHold(t *testing.T) {
	// The failure this prevents: an alarm points the camera at the incident and the patrol
	// rotates it away three seconds later, so the recording shows the corridor next door.
	cam := &fakePTZCamera{presets: threePresets()}
	svc, _, _, _, clock := newTourService(t, cam)
	ctx := context.Background()

	tour, _ := svc.SaveTour(ctx, PTZTourSave{
		CameraId: 7, Name: "Perimeter", DwellSeconds: 10,
		Stops: []PTZTourStop{{Preset: "p1"}, {Preset: "p2"}},
	})
	_, _ = svc.SetTourRunning(ctx, tour.Id, true)
	svc.Step(ctx)

	if err := svc.Recall(ctx, PTZRecallRequest{CameraId: 7, PresetToken: "p3", HoldSeconds: 60, Reason: "Gate"}); err != nil {
		t.Fatalf("recall: %v", err)
	}
	if got := strings.Join(cam.visited, ","); got != "p1,p3" {
		t.Fatalf("recall did not move the camera: %s", got)
	}
	// Well past the tour's dwell, well inside the recall hold.
	*clock += 30
	svc.Step(ctx)
	if got := strings.Join(cam.visited, ","); got != "p1,p3" {
		t.Fatalf("the patrol rotated away from an alarm: %s", got)
	}
	*clock += 40
	svc.Step(ctx)
	if got := strings.Join(cam.visited, ","); got != "p1,p3,p2" {
		t.Fatalf("the patrol did not resume after the hold: %s", got)
	}
}

func TestPatrolStopsAndSaysSoWhenItsPresetsAreGone(t *testing.T) {
	cam := &fakePTZCamera{presets: threePresets()}
	svc, repo, journal, notif, clock := newTourService(t, cam)
	ctx := context.Background()

	tour, _ := svc.SaveTour(ctx, PTZTourSave{
		CameraId: 7, Name: "Perimeter", DwellSeconds: 10,
		Stops: []PTZTourStop{{Preset: "p1"}, {Preset: "p2"}},
	})
	_, _ = svc.SetTourRunning(ctx, tour.Id, true)
	svc.Step(ctx)

	// Somebody clears the camera's presets from its own web page.
	cam.presets = []onvif.PTZPreset{{Token: "p3", Name: "Dock"}}
	*clock += 20
	svc.Step(ctx)

	if repo.rows[0].IsRunning {
		t.Fatal("a tour that cannot be walked must not still claim to be running")
	}
	if journal.Motion(7).Touring {
		t.Fatal("the camera must not still be marked as patrolling")
	}
	// A patrol that has quietly stopped patrolling is a security failure: the screen still
	// says running and nobody is told.
	if len(notif.sent) != 1 {
		t.Fatalf("want exactly one notification, got %d", len(notif.sent))
	}
	if !strings.Contains(notif.sent[0].Body, "Yard dome") {
		t.Fatalf("the notification should name the camera: %q", notif.sent[0].Body)
	}

	// And it says so ONCE, not on every tick.
	*clock += 20
	svc.Step(ctx)
	svc.Step(ctx)
	if len(notif.sent) != 1 {
		t.Fatalf("the stop was announced %d times", len(notif.sent))
	}
}

func TestUnreachableCameraDoesNotStopThePatrol(t *testing.T) {
	// The trap this closes: an empty preset list satisfies every claim about its members,
	// so reading "the camera did not answer" as "it has no presets" would stop every patrol
	// in the building the moment the network hiccupped — and each one would announce itself
	// as broken.
	cam := &fakePTZCamera{presets: threePresets()}
	svc, repo, _, notif, clock := newTourService(t, cam)
	ctx := context.Background()

	tour, _ := svc.SaveTour(ctx, PTZTourSave{
		CameraId: 7, Name: "Perimeter", DwellSeconds: 10,
		Stops: []PTZTourStop{{Preset: "p1"}, {Preset: "p2"}},
	})
	_, _ = svc.SetTourRunning(ctx, tour.Id, true)
	svc.Step(ctx)

	cam.unreachable = true
	*clock += 20
	svc.Step(ctx)
	if !repo.rows[0].IsRunning {
		t.Fatal("an unreachable camera must not stop its tour")
	}
	if len(notif.sent) != 0 {
		t.Fatalf("nothing should have been announced: %+v", notif.sent)
	}

	cam.unreachable = false
	*clock += 20
	svc.Step(ctx)
	if got := strings.Join(cam.visited, ","); got != "p1,p2" {
		t.Fatalf("the patrol did not resume when the camera came back: %s", got)
	}
}

func TestFailedStepRetriesTheSameStopRatherThanSkippingIt(t *testing.T) {
	cam := &fakePTZCamera{presets: threePresets()}
	svc, _, _, _, clock := newTourService(t, cam)
	ctx := context.Background()

	tour, _ := svc.SaveTour(ctx, PTZTourSave{
		CameraId: 7, Name: "Perimeter", DwellSeconds: 10,
		Stops: []PTZTourStop{{Preset: "p1"}, {Preset: "p2"}},
	})
	_, _ = svc.SetTourRunning(ctx, tour.Id, true)

	cam.moveErr = errors.New("camera is rebooting")
	svc.Step(ctx)
	if len(cam.visited) != 0 {
		t.Fatalf("a failed move must not be recorded as a visit: %v", cam.visited)
	}
	// And it must not retry every tick: a dome that is rebooting would be hammered and the
	// log filled with one line.
	svc.Step(ctx)
	svc.Step(ctx)

	cam.moveErr = nil
	*clock += 10
	svc.Step(ctx)
	if got := strings.Join(cam.visited, ","); got != "p1" {
		t.Fatalf("the failed stop should be retried, not skipped: %s", got)
	}
}

func TestStartRefusesATourWhosePresetsAreGone(t *testing.T) {
	cam := &fakePTZCamera{presets: threePresets()}
	svc, _, _, _, _ := newTourService(t, cam)
	ctx := context.Background()

	tour, _ := svc.SaveTour(ctx, PTZTourSave{
		CameraId: 7, Name: "Perimeter", DwellSeconds: 10,
		Stops: []PTZTourStop{{Preset: "p1"}, {Preset: "p2"}},
	})
	cam.presets = []onvif.PTZPreset{{Token: "p1", Name: "Gate"}}
	if _, err := svc.SetTourRunning(ctx, tour.Id, true); err == nil {
		t.Fatal("starting a tour that cannot be walked must be refused where somebody reads the answer")
	}

	cam.unreachable = true
	if _, err := svc.SetTourRunning(ctx, tour.Id, true); err == nil {
		t.Fatal("starting a tour whose camera cannot be checked must be refused, not assumed fine")
	}
}

func TestTourViewReportsMissingStops(t *testing.T) {
	cam := &fakePTZCamera{presets: threePresets()}
	svc, _, _, _, _ := newTourService(t, cam)
	ctx := context.Background()

	tour, _ := svc.SaveTour(ctx, PTZTourSave{
		CameraId: 7, Name: "Perimeter", DwellSeconds: 10,
		Stops: []PTZTourStop{{Preset: "p1"}, {Preset: "p2"}},
	})
	cam.presets = []onvif.PTZPreset{{Token: "p1", Name: "Gate"}}

	view, err := svc.Tour(ctx, tour.Id)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if view.PresetsUnavailable {
		t.Fatal("the camera answered; the presets are available")
	}
	if view.Stops[0].Missing || !view.Stops[1].Missing {
		t.Fatalf("missing stops mis-reported: %+v", view.Stops)
	}
}

func TestParseRulePTZRecall(t *testing.T) {
	cases := []struct {
		name   string
		config string
		want   *PTZRecallRule
	}{
		{name: "absent", config: `{"classes":["person"]}`, want: nil},
		{name: "empty config", config: "", want: nil},
		{name: "unparseable", config: "{not json", want: nil},
		{
			// A recall with no preset names nowhere to go. Treated as absent rather than as
			// a recall to "" — which the camera would refuse once per alert, forever.
			name: "no preset", config: `{"ptzRecall":{"cameraId":3,"holdSeconds":30}}`, want: nil,
		},
		{
			name:   "full",
			config: `{"ptzRecall":{"cameraId":3,"preset":"p2","holdSeconds":45}}`,
			want:   &PTZRecallRule{CameraId: 3, Preset: "p2", HoldSeconds: 45},
		},
		{
			name:   "own camera",
			config: `{"ptzRecall":{"preset":"p2"}}`,
			want:   &PTZRecallRule{Preset: "p2"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseRulePTZRecall(tc.config)
			if tc.want == nil {
				if got != nil {
					t.Fatalf("want nil, got %+v", got)
				}
				return
			}
			if got == nil || *got != *tc.want {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestTourStopEncoding(t *testing.T) {
	stops := []PTZTourStop{{Preset: "p1"}, {Preset: "p2", DwellSeconds: 30}}
	encoded := encodeTourStops(stops)
	if encoded != "p1,p2:30" {
		t.Fatalf("encoded = %q", encoded)
	}
	back := decodeTourStops(encoded)
	if len(back) != 2 || back[0].Preset != "p1" || back[0].DwellSeconds != 0 ||
		back[1].Preset != "p2" || back[1].DwellSeconds != 30 {
		t.Fatalf("round trip lost something: %+v", back)
	}
	// A token containing a separator would decode into a different route than it encoded.
	// Dropping it is loud; silently corrupting a patrol is not.
	if encodeTourStops([]PTZTourStop{{Preset: "a:b"}, {Preset: "c,d"}}) != "" {
		t.Fatal("a token containing the separator must not be encoded")
	}
}

// THE RACE THE BENCH CAUGHT. A patrol that had been told to stop moved the camera once
// more: a tick reads the tour rows, then asks the camera what presets it has — an ONVIF
// round trip — and only then commands the move. A stop landing inside that gap is written
// to a row the tick has already read.
//
// A camera that swings away one beat AFTER an operator was told the patrol had stopped is
// worse than one that never stopped, because the screen and the dome disagree about who has
// the camera. It is also exactly when somebody is reaching for the ring.
func TestAStoppedPatrolDoesNotGetOneMoreMoveOut(t *testing.T) {
	cam := &fakePTZCamera{presets: threePresets()}
	svc, _, _, _, clock := newTourService(t, cam)
	ctx := context.Background()

	tour, _ := svc.SaveTour(ctx, PTZTourSave{
		CameraId: 7, Name: "Perimeter", DwellSeconds: 10,
		Stops: []PTZTourStop{{Preset: "p1"}, {Preset: "p2"}},
	})
	_, _ = svc.SetTourRunning(ctx, tour.Id, true)
	svc.Step(ctx)
	if len(cam.visited) != 1 {
		t.Fatalf("expected the first step: %v", cam.visited)
	}

	// The stop lands while the runner is mid-tick, after it has read the tour row.
	*clock += 20
	cam.duringPresetRead = func() { _, _ = svc.SetTourRunning(ctx, tour.Id, false) }
	svc.Step(ctx)
	if len(cam.visited) != 1 {
		t.Fatalf("a stopped patrol moved the camera anyway: %v", cam.visited)
	}
}

// The same gap, taken by a PERSON. An operator grabbing the ring while a tick is in flight
// must not get one more tour step in their face.
func TestAnOperatorTakingTheRingMidTickStopsTheStep(t *testing.T) {
	cam := &fakePTZCamera{presets: threePresets()}
	svc, _, journal, _, clock := newTourService(t, cam)
	ctx := context.Background()

	tour, _ := svc.SaveTour(ctx, PTZTourSave{
		CameraId: 7, Name: "Perimeter", DwellSeconds: 10,
		Stops: []PTZTourStop{{Preset: "p1"}, {Preset: "p2"}},
	})
	_, _ = svc.SetTourRunning(ctx, tour.Id, true)
	svc.Step(ctx)

	*clock += 20
	cam.duringPresetRead = func() { journal.ClaimManual(7, 30*time.Second) }
	svc.Step(ctx)
	if len(cam.visited) != 1 {
		t.Fatalf("the patrol stepped into an operator who had just taken the camera: %v", cam.visited)
	}
}

// Deleting a camera takes its patrols with it. Without this the runner keeps commanding a
// device that is no longer configured, every dwell, forever — and the tours are listed under
// an id nothing can render. W3-2 shipped this exact shape with its appearance descriptors.
func TestDeletingACameraTakesItsToursWithIt(t *testing.T) {
	cam := &fakePTZCamera{presets: threePresets()}
	svc, repo, journal, _, clock := newTourService(t, cam)
	ctx := context.Background()

	tour, _ := svc.SaveTour(ctx, PTZTourSave{
		CameraId: 7, Name: "Perimeter", DwellSeconds: 10,
		Stops: []PTZTourStop{{Preset: "p1"}, {Preset: "p2"}},
	})
	_, _ = svc.SaveTour(ctx, PTZTourSave{
		CameraId: 9, Name: "Other camera", DwellSeconds: 10,
		Stops: []PTZTourStop{{Preset: "p1"}, {Preset: "p2"}},
	})
	_, _ = svc.SetTourRunning(ctx, tour.Id, true)
	svc.Step(ctx)

	removed, err := svc.DeleteToursForCamera(ctx, 7)
	if err != nil || removed != 1 {
		t.Fatalf("delete: removed=%d err=%v", removed, err)
	}
	// Only that camera's tours: another camera's patrol must not be collateral.
	if len(repo.rows) != 1 || repo.rows[0].CameraId != 9 {
		t.Fatalf("wrong rows survived: %+v", repo.rows)
	}
	if journal.Motion(7).Touring {
		t.Fatal("the deleted camera is still marked as patrolling, which blinds the tamper monitor")
	}
	before := len(cam.visited)
	*clock += 30
	svc.Step(ctx)
	if len(cam.visited) != before {
		t.Fatalf("something is still commanding the deleted camera: %v", cam.visited)
	}
}
