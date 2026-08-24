package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	"github.com/mysayasan/kopiv2/domain/notification"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	"github.com/mysayasan/kopiv2/infra/onvif"
	"github.com/mysayasan/kopiv2/infra/safego"
)

// Guard tours and alarm recall (W3-5).
//
// A PTZ camera that only jogs is a camera that needs somebody watching it. The two things
// that make it an unattended device are here: it PATROLS a route on its own, and it GOES
// SOMEWHERE when something happens. Both are expressed in the same currency — a named
// position stored on the camera — which is why they share a file and a runner.
//
// THE THREE CLAIMS ON ONE CAMERA, IN ORDER. A camera can be wanted by a person at the PTZ
// ring, by an alarm, and by its own patrol, and they will collide. The order is fixed:
//
//  1. A PERSON wins. Somebody driving a camera is tracking something the appliance cannot
//     see, and taking the camera off them loses it. An alarm arriving while they hold it
//     does NOT move the camera — it is reported, and they decide.
//  2. An ALARM beats the patrol. A patrol is a way of looking at nothing in particular; an
//     alarm is something in particular.
//  3. The PATROL runs when nobody else wants the camera.
//
// Getting this wrong is not a cosmetic bug. A patrol that steps during an alarm rotates the
// camera away from the incident three seconds after pointing it there, and the recording
// shows the empty corridor next door.

// ptzTourTick is how often the runner looks at its tours. It bounds how late a stop can be,
// and 2s is well inside the shortest dwell a tour may be saved with.
const ptzTourTick = 2 * time.Second

// Dwell bounds. Below the minimum a dome is still slewing when it is told to leave, so the
// tour records nothing but motion blur; above the maximum a "patrol" is a camera pointing
// one way for an hour, which is what a fixed camera is for.
const (
	ptzMinDwellSeconds = 5
	ptzMaxDwellSeconds = 3600
	ptzTourMaxStops    = 64
)

// ptzRecallDefaultHold is how long a camera stays where an alarm sent it before its patrol
// may resume. Long enough for an operator to be pulled in by the notification and look.
const ptzRecallDefaultHold = 60

// PTZTourStop is one leg of a patrol.
type PTZTourStop struct {
	// Preset is the DEVICE's token. See entities.PtzTour for why we store a handle we do
	// not own.
	Preset string `json:"preset"`
	// DwellSeconds is 0 when the stop inherits the tour's dwell.
	DwellSeconds int `json:"dwellSeconds"`
	// Missing is true when the camera no longer has this preset. Reported rather than
	// dropped, exactly as WallView reports a camera that no longer exists: a patrol that
	// silently visits five of its six places tells an operator the route is intact when
	// the one place that was removed is the one nobody is watching.
	Missing bool `json:"missing"`
}

// PTZTourView is a tour as the screen needs it.
type PTZTourView struct {
	*entities.PtzTour
	Stops []PTZTourStop `json:"stopList"`
	// PresetsUnavailable is true when the camera could not be asked what presets it has.
	//
	// It exists so an OFFLINE camera does not report every stop as missing. An empty
	// preset list satisfies every claim about its members, and "your whole patrol has
	// been deleted" is a much worse thing to say than "the camera did not answer".
	PresetsUnavailable bool `json:"presetsUnavailable"`
	// RunningStop is the index of the stop the camera is on, or -1 when the tour is not
	// running. NextStepAt is when it is due to move.
	RunningStop int   `json:"runningStop"`
	NextStepAt  int64 `json:"nextStepAt"`
	// LastError is the camera's own words about why the last step failed, if it did.
	LastError string `json:"lastError,omitempty"`
}

// PTZTourSave is a create or an update.
type PTZTourSave struct {
	Id           int64
	CameraId     int64
	Name         string
	Stops        []PTZTourStop
	DwellSeconds int
	Actor        CaseActor
}

// PTZRecallRequest sends a camera somewhere because something happened.
type PTZRecallRequest struct {
	CameraId    int64
	PresetToken string
	HoldSeconds int
	// Reason is what gets logged; it is the rule name, not a code, because the log is
	// read by whoever is asking why the camera moved.
	Reason string
}

// IPTZService is the tour and recall surface.
type IPTZService interface {
	// Tours lists a camera's patrols. cameraId 0 lists every camera's.
	Tours(ctx context.Context, cameraId int64) ([]PTZTourView, error)
	Tour(ctx context.Context, id int64) (*PTZTourView, error)
	SaveTour(ctx context.Context, req PTZTourSave) (*PTZTourView, error)
	DeleteTour(ctx context.Context, id int64) error
	SetTourRunning(ctx context.Context, id int64, running bool) (*PTZTourView, error)
	// DeleteToursForCamera is the camera-deletion cascade's leg. See the method.
	DeleteToursForCamera(ctx context.Context, cameraId int64) (int, error)
	// Recall is the alarm path. It refuses to move a camera an operator is holding.
	Recall(ctx context.Context, req PTZRecallRequest) error
	// Start runs the patrol loop.
	Start(ctx context.Context)
}

// ptzCameraController is the slice of ICameraService a tour needs. Declared at the
// consumer so a fake stubs five methods rather than forty.
type ptzCameraController interface {
	PTZPresets(ctx context.Context, id uint64) ([]onvif.PTZPreset, error)
	PTZGotoPreset(ctx context.Context, id uint64, presetToken string, speed float64) error
	DisplayName(ctx context.Context, id int64) string
}

type ptzService struct {
	repo     dbsql.IGenericRepo[entities.PtzTour]
	cameras  ptzCameraController
	journal  *PTZJournal
	notifier INotificationPublisher
	now      func() time.Time

	mu sync.Mutex
	// state is the runner's per-tour memory: where the patrol has got to, when it is due
	// to move, and what went wrong last time. Deliberately NOT persisted — resuming a
	// reboot mid-route buys nothing, and the first tick sends the camera to stop 0, which
	// is a defined place rather than wherever the power cut left it.
	state map[int64]*ptzTourState
	// recallUntil is when an alarm's claim on a camera expires.
	recallUntil map[int64]int64
	// gen counts how many times a tour has been stopped, edited or deleted.
	//
	// It closes a race the bench caught: a patrol that had been TOLD TO STOP moved the
	// camera once more. A tick reads the tour rows, then asks the camera what presets it
	// has — an ONVIF round trip — and only then commands the move. A stop landing inside
	// that gap is written to a row the tick has already read, so the move goes out anyway,
	// after the operator was told the patrol had stopped. Re-reading the row would cost
	// another query per tick; a counter bumped by every path that invalidates a step is
	// exact and free.
	gen map[int64]int64
}

type ptzTourState struct {
	stop       int
	nextStepAt int64
	lastError  string
	// stoppedReported guards the "this patrol has stopped" notification so a tour whose
	// presets were deleted says so once rather than every tick.
	stoppedReported bool
}

func NewPTZService(
	repo dbsql.IGenericRepo[entities.PtzTour],
	cameras ptzCameraController,
	journal *PTZJournal,
	notifier INotificationPublisher,
) IPTZService {
	return &ptzService{
		repo: repo, cameras: cameras, journal: journal, notifier: notifier,
		now:         time.Now,
		state:       map[int64]*ptzTourState{},
		recallUntil: map[int64]int64{},
		gen:         map[int64]int64{},
	}
}

// --- reads --------------------------------------------------------------------

func (s *ptzService) Tours(ctx context.Context, cameraId int64) ([]PTZTourView, error) {
	rows, err := s.list(ctx)
	if err != nil {
		return nil, err
	}
	// Presets are read once per camera, not once per tour: three tours on one dome is one
	// ONVIF round trip, not three.
	presetsByCamera := map[int64]map[string]bool{}
	out := make([]PTZTourView, 0, len(rows))
	for _, row := range rows {
		if row == nil || (cameraId > 0 && row.CameraId != cameraId) {
			continue
		}
		known, ok := presetsByCamera[row.CameraId]
		if !ok {
			known = s.knownPresets(ctx, row.CameraId)
			presetsByCamera[row.CameraId] = known
		}
		out = append(out, s.view(row, known))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CameraId != out[j].CameraId {
			return out[i].CameraId < out[j].CameraId
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

func (s *ptzService) Tour(ctx context.Context, id int64) (*PTZTourView, error) {
	row, err := s.get(ctx, id)
	if err != nil {
		return nil, err
	}
	view := s.view(row, s.knownPresets(ctx, row.CameraId))
	return &view, nil
}

// --- writes -------------------------------------------------------------------

func (s *ptzService) SaveTour(ctx context.Context, req PTZTourSave) (*PTZTourView, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, errors.New("a tour needs a name")
	}
	if req.CameraId <= 0 {
		return nil, errors.New("a tour needs a camera")
	}
	dwell := req.DwellSeconds
	if dwell == 0 {
		dwell = 15
	}
	if dwell < ptzMinDwellSeconds || dwell > ptzMaxDwellSeconds {
		return nil, fmt.Errorf("hold each stop for %d to %d seconds", ptzMinDwellSeconds, ptzMaxDwellSeconds)
	}
	stops := make([]PTZTourStop, 0, len(req.Stops))
	for _, stop := range req.Stops {
		token := strings.TrimSpace(stop.Preset)
		if token == "" {
			continue
		}
		if stop.DwellSeconds != 0 && (stop.DwellSeconds < ptzMinDwellSeconds || stop.DwellSeconds > ptzMaxDwellSeconds) {
			return nil, fmt.Errorf("hold each stop for %d to %d seconds", ptzMinDwellSeconds, ptzMaxDwellSeconds)
		}
		stops = append(stops, PTZTourStop{Preset: token, DwellSeconds: stop.DwellSeconds})
	}
	// REFUSED, not stored, when the route cannot be walked. A tour of one stop is a
	// camera pointing at one thing — which is a preset recall, not a patrol — and a tour
	// of none is a row that runs forever and visits nowhere. Same rule as a detection rule
	// that could never fire: say so at save time, where somebody is reading the answer.
	if len(stops) < 2 {
		return nil, errors.New("a tour needs at least two stops — one stop is a preset, not a patrol")
	}
	if len(stops) > ptzTourMaxStops {
		return nil, fmt.Errorf("a tour holds at most %d stops", ptzTourMaxStops)
	}
	// Every stop must name a preset the camera actually has. Checked against the DEVICE
	// rather than against a list we keep, because the device is the only authority — and
	// skipped entirely when the camera cannot be reached, so a tour is still editable
	// while its camera is down.
	if known := s.knownPresets(ctx, req.CameraId); known != nil {
		for _, stop := range stops {
			if !known[stop.Preset] {
				return nil, fmt.Errorf("this camera has no preset %q — save the position on the camera first", stop.Preset)
			}
		}
	}
	if err := s.nameIsFree(ctx, req.CameraId, name, req.Id); err != nil {
		return nil, err
	}

	now := s.now().UTC().Unix()
	row := entities.PtzTour{
		Id: req.Id, CameraId: req.CameraId, Name: name,
		Stops: encodeTourStops(stops), DwellSeconds: dwell, UpdatedAt: now,
	}
	if req.Id > 0 {
		existing, err := s.get(ctx, req.Id)
		if err != nil {
			return nil, err
		}
		row.CreatedBy, row.CreatedName, row.CreatedAt = existing.CreatedBy, existing.CreatedName, existing.CreatedAt
		// Editing a running tour keeps it running — an operator adjusting a dwell should
		// not silently stop the patrol — but the route it is walking has changed, so the
		// runner starts again from the first stop rather than from an index into a list
		// that no longer means the same thing.
		row.IsRunning = existing.IsRunning
		if _, err := s.repo.UpdateById(ctx, "", row); err != nil {
			return nil, err
		}
		s.resetState(row.Id)
	} else {
		row.CreatedBy, row.CreatedName, row.CreatedAt = req.Actor.Id, req.Actor.Name, now
		id, err := s.repo.Create(ctx, "", row)
		if err != nil {
			return nil, err
		}
		row.Id = int64(id)
	}
	view := s.view(&row, s.knownPresets(ctx, row.CameraId))
	return &view, nil
}

func (s *ptzService) DeleteTour(ctx context.Context, id int64) error {
	row, err := s.get(ctx, id)
	if err != nil {
		return err
	}
	if _, err := s.repo.DeleteById(ctx, "", uint64(id)); err != nil {
		return err
	}
	s.resetState(id)
	// The camera may have been left mid-route with nothing to move it on. Clearing the
	// touring flag matters to the tamper monitor, which suspends its MOVED verdict while
	// a camera is on patrol: leaving it set would blind the monitor on that camera for
	// the life of the process.
	s.clearTouringIfIdle(ctx, row.CameraId, id)
	return nil
}

// DeleteToursForCamera removes every tour on a camera being deleted, and forgets what the
// journal knew about it.
//
// Registered in the camera deletion cascade. Without it a deleted camera leaves its patrols
// behind: the runner keeps commanding a device that is no longer configured, logs the
// failure every dwell forever, and the tours are listed under an id nothing can render.
// The same shape as the appearance descriptors W3-2 shipped without a cascade — found there
// only because a bench finally deleted a camera.
func (s *ptzService) DeleteToursForCamera(ctx context.Context, cameraId int64) (int, error) {
	rows, err := s.list(ctx)
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, row := range rows {
		if row == nil || row.CameraId != cameraId {
			continue
		}
		if _, err := s.repo.DeleteById(ctx, "", uint64(row.Id)); err != nil {
			return removed, err
		}
		s.resetState(row.Id)
		removed++
	}
	// Forget, not SetTouring(false): the camera is GONE, so everything the journal knew
	// about it goes too — the touring flag, the operator's claim and the last commanded
	// move. Clearing the flag first would be dead code, which a mutation of this line
	// proved by surviving.
	s.journal.Forget(cameraId)
	return removed, nil
}

func (s *ptzService) SetTourRunning(ctx context.Context, id int64, running bool) (*PTZTourView, error) {
	row, err := s.get(ctx, id)
	if err != nil {
		return nil, err
	}
	stops := decodeTourStops(row.Stops)
	if running {
		// Starting a patrol whose presets have been deleted from the camera would produce
		// a tour that reports itself as running and never moves. Refuse where somebody is
		// reading the answer.
		known := s.knownPresets(ctx, row.CameraId)
		if known == nil {
			return nil, errors.New("cannot reach the camera to check its presets")
		}
		usable := 0
		for _, stop := range stops {
			if known[stop.Preset] {
				usable++
			}
		}
		if usable < 2 {
			return nil, errors.New("this tour's presets are no longer on the camera — edit the tour before starting it")
		}
	}
	row.IsRunning = running
	row.UpdatedAt = s.now().UTC().Unix()
	if _, err := s.repo.UpdateById(ctx, "", *row); err != nil {
		return nil, err
	}
	s.resetState(id)
	if running {
		// Step on the next tick rather than waiting a full dwell: an operator who pressed
		// start expects the camera to go somewhere.
		s.mu.Lock()
		s.state[id] = &ptzTourState{stop: -1, nextStepAt: 0}
		s.mu.Unlock()
		s.journal.SetTouring(row.CameraId, true)
	} else {
		s.clearTouringIfIdle(ctx, row.CameraId, 0)
	}
	view := s.view(row, s.knownPresets(ctx, row.CameraId))
	return &view, nil
}

// --- the alarm path -----------------------------------------------------------

func (s *ptzService) Recall(ctx context.Context, req PTZRecallRequest) error {
	if req.CameraId <= 0 || strings.TrimSpace(req.PresetToken) == "" {
		return errors.New("a recall needs a camera and a preset")
	}
	// A person at the ring outranks an alarm. They are tracking something this appliance
	// cannot see, and moving the camera under them loses it. The alarm still reaches them
	// through the notification feed, where they can act on it — which is the point: the
	// decision belongs to the person holding the camera.
	if s.journal.ManualHeld(req.CameraId) {
		log.Printf("ptz: cam%d: recall skipped (%s) — an operator has the camera", req.CameraId, req.Reason)
		return nil
	}
	hold := req.HoldSeconds
	if hold <= 0 {
		hold = ptzRecallDefaultHold
	}
	if err := s.cameras.PTZGotoPreset(ctx, uint64(req.CameraId), strings.TrimSpace(req.PresetToken), 0); err != nil {
		return err
	}
	s.mu.Lock()
	s.recallUntil[req.CameraId] = s.now().UTC().Unix() + int64(hold)
	s.mu.Unlock()
	return nil
}

// --- the patrol loop ----------------------------------------------------------

func (s *ptzService) Start(ctx context.Context) {
	safego.Supervise(ctx, "mymatasan.ptz.tours", func(ctx context.Context) {
		ticker := time.NewTicker(ptzTourTick)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.Step(ctx)
			}
		}
	})
}

// Step advances every running tour that is due. Exported so a test can drive the patrol
// without sleeping through a dwell.
func (s *ptzService) Step(ctx context.Context) {
	rows, err := s.list(ctx)
	if err != nil {
		return
	}
	now := s.now().UTC().Unix()
	// touring is recomputed from scratch each pass rather than toggled on transitions, so
	// a tour that was deleted, stopped or edited out of existence cannot leave a camera
	// marked as patrolling forever.
	touring := map[int64]bool{}
	for _, row := range rows {
		if row != nil && row.IsRunning {
			touring[row.CameraId] = true
		}
	}
	for cameraId, on := range touring {
		s.journal.SetTouring(cameraId, on)
	}

	for _, row := range rows {
		if row == nil || !row.IsRunning || ctx.Err() != nil {
			continue
		}
		s.stepTour(ctx, row, now)
	}
}

func (s *ptzService) stepTour(ctx context.Context, row *entities.PtzTour, now int64) {
	// Both claims that outrank a patrol, checked before anything else is computed.
	if s.journal.ManualHeld(row.CameraId) {
		return
	}
	s.mu.Lock()
	recallUntil := s.recallUntil[row.CameraId]
	st := s.state[row.Id]
	if st == nil {
		st = &ptzTourState{stop: -1}
		s.state[row.Id] = st
	}
	if recallUntil > now {
		s.mu.Unlock()
		return
	}
	if st.nextStepAt > now {
		s.mu.Unlock()
		return
	}
	current := st.stop
	gen := s.gen[row.Id]
	s.mu.Unlock()

	stops := decodeTourStops(row.Stops)
	known := s.knownPresets(ctx, row.CameraId)
	// A camera that cannot be asked is not a camera whose presets have gone. Skip this
	// tick and try again — the alternative reads an unreachable camera as an empty preset
	// list and stops the patrol.
	if known == nil {
		s.noteTourError(row.Id, "camera did not answer")
		return
	}
	usable := make([]PTZTourStop, 0, len(stops))
	for _, stop := range stops {
		if known[stop.Preset] {
			usable = append(usable, stop)
		}
	}
	if len(usable) < 2 {
		// A patrol that has quietly stopped patrolling is a security failure, not a
		// cosmetic one: the screen still says "running" and nobody is told the route is
		// gone. Stop it, persist that, and raise it once.
		s.haltTour(ctx, row, "its presets are no longer on the camera")
		return
	}

	next := (current + 1) % len(usable)
	stop := usable[next]
	dwell := stop.DwellSeconds
	if dwell <= 0 {
		dwell = row.DwellSeconds
	}
	if dwell < ptzMinDwellSeconds {
		dwell = ptzMinDwellSeconds
	}

	// THE LAST-MOMENT CHECK. Everything above this line is a decision made from state read
	// before an ONVIF round trip; everything below moves a physical camera. Between the two,
	// somebody may have stopped the tour, edited it, deleted it, or taken the ring — and a
	// camera that swings away one beat AFTER an operator was told the patrol had stopped is
	// worse than one that never stopped, because the screen and the dome disagree.
	s.mu.Lock()
	stale := s.gen[row.Id] != gen
	s.mu.Unlock()
	if stale || s.journal.ManualHeld(row.CameraId) {
		return
	}

	if err := s.cameras.PTZGotoPreset(ctx, uint64(row.CameraId), stop.Preset, 0); err != nil {
		// A failed step does not advance the route and does not retry immediately: a dome
		// that is rebooting would otherwise be hammered every two seconds and the log
		// filled with the same line. Wait a dwell and try the SAME stop again.
		s.mu.Lock()
		st := s.state[row.Id]
		if st != nil {
			st.lastError = err.Error()
			st.nextStepAt = now + int64(dwell)
		}
		s.mu.Unlock()
		log.Printf("ptz: cam%d tour%d: step to %q failed: %v", row.CameraId, row.Id, stop.Preset, err)
		return
	}

	s.mu.Lock()
	if st := s.state[row.Id]; st != nil {
		st.stop = next
		st.nextStepAt = now + int64(dwell)
		st.lastError = ""
	}
	s.mu.Unlock()
}

// haltTour stops a tour that can no longer be walked and says so once.
func (s *ptzService) haltTour(ctx context.Context, row *entities.PtzTour, why string) {
	row.IsRunning = false
	row.UpdatedAt = s.now().UTC().Unix()
	if _, err := s.repo.UpdateById(ctx, "", *row); err != nil {
		log.Printf("ptz: tour%d: could not persist stop: %v", row.Id, err)
	}
	s.journal.SetTouring(row.CameraId, false)

	s.mu.Lock()
	// Invalidate any step already in flight for this tour, for the same reason a manual
	// stop does: the patrol has ended and must not get one more move out.
	s.gen[row.Id]++
	st := s.state[row.Id]
	if st == nil {
		st = &ptzTourState{stop: -1}
		s.state[row.Id] = st
	}
	already := st.stoppedReported
	st.stoppedReported = true
	st.lastError = why
	s.mu.Unlock()
	if already || s.notifier == nil {
		return
	}
	name := s.cameras.DisplayName(ctx, row.CameraId)
	if strings.TrimSpace(name) == "" {
		name = "camera " + strconv.FormatInt(row.CameraId, 10)
	}
	s.notifier.Publish(ctx, notification.Notification{
		Category: notification.CategoryHealthCheck,
		Severity: notification.Warning,
		Title:    "Guard tour stopped",
		Body:     fmt.Sprintf("%s stopped patrolling on %s: %s.", row.Name, name, why),
		Source:   "ptz-tour",
		CameraId: row.CameraId,
		RefType:  "camera",
		RefId:    row.CameraId,
		Data: map[string]any{
			"cameraId": row.CameraId,
			"tourId":   row.Id,
			"tourName": row.Name,
			"reason":   why,
		},
	})
}

func (s *ptzService) noteTourError(tourId int64, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.state[tourId]
	if st == nil {
		st = &ptzTourState{stop: -1}
		s.state[tourId] = st
	}
	st.lastError = message
}

// --- internals ----------------------------------------------------------------

func (s *ptzService) list(ctx context.Context) ([]*entities.PtzTour, error) {
	rows, _, err := s.repo.Get(ctx, "", 500, 0, nil,
		[]sqldataenums.Sorter{{FieldName: "CameraId", Sort: sqldataenums.ASC}})
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (s *ptzService) get(ctx context.Context, id int64) (*entities.PtzTour, error) {
	if id <= 0 {
		return nil, errors.New("a tour id is required")
	}
	row, err := s.repo.GetById(ctx, "", uint64(id))
	if err != nil {
		if isNoResultFoundErr(err) {
			return nil, errors.New("no such tour")
		}
		return nil, err
	}
	if row == nil {
		return nil, errors.New("no such tour")
	}
	return row, nil
}

func (s *ptzService) nameIsFree(ctx context.Context, cameraId int64, name string, selfId int64) error {
	rows, err := s.list(ctx)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if row == nil || row.Id == selfId || row.CameraId != cameraId {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(row.Name), name) {
			return fmt.Errorf("this camera already has a tour called %q", row.Name)
		}
	}
	return nil
}

// knownPresets returns the camera's preset tokens, or NIL when the camera could not be
// asked. Nil and empty mean different things here and every caller checks which.
func (s *ptzService) knownPresets(ctx context.Context, cameraId int64) map[string]bool {
	presets, err := s.cameras.PTZPresets(ctx, uint64(cameraId))
	if err != nil {
		return nil
	}
	known := make(map[string]bool, len(presets))
	for _, preset := range presets {
		known[preset.Token] = true
	}
	return known
}

func (s *ptzService) view(row *entities.PtzTour, known map[string]bool) PTZTourView {
	stops := decodeTourStops(row.Stops)
	if known != nil {
		for i := range stops {
			stops[i].Missing = !known[stops[i].Preset]
		}
	}
	view := PTZTourView{PtzTour: row, Stops: stops, PresetsUnavailable: known == nil, RunningStop: -1}
	s.mu.Lock()
	if st := s.state[row.Id]; st != nil && row.IsRunning {
		view.RunningStop = st.stop
		view.NextStepAt = st.nextStepAt
		view.LastError = st.lastError
	}
	s.mu.Unlock()
	return view
}

// resetState forgets where a tour had got to AND invalidates any step already in flight.
// Called by every path that changes what the tour is or whether it should be walking.
func (s *ptzService) resetState(tourId int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.state, tourId)
	s.gen[tourId]++
}

// clearTouringIfIdle drops a camera's touring flag when no other tour on it is running.
// exceptId is a tour being deleted, which may still be in the table the caller read.
func (s *ptzService) clearTouringIfIdle(ctx context.Context, cameraId int64, exceptId int64) {
	rows, err := s.list(ctx)
	if err != nil {
		return
	}
	for _, row := range rows {
		if row == nil || row.Id == exceptId || row.CameraId != cameraId {
			continue
		}
		if row.IsRunning {
			return
		}
	}
	s.journal.SetTouring(cameraId, false)
}

// encodeTourStops renders the itinerary as "token:dwell,token:dwell".
//
// A colon separator is safe because ONVIF preset tokens are device-issued identifiers, but
// "safe because" is not "guaranteed", so a token containing the separator is refused rather
// than encoded into something that decodes differently. Silent corruption of a route is
// exactly the failure this format could produce.
func encodeTourStops(stops []PTZTourStop) string {
	parts := make([]string, 0, len(stops))
	for _, stop := range stops {
		token := strings.TrimSpace(stop.Preset)
		if token == "" || strings.ContainsAny(token, ":,") {
			continue
		}
		if stop.DwellSeconds > 0 {
			parts = append(parts, token+":"+strconv.Itoa(stop.DwellSeconds))
			continue
		}
		parts = append(parts, token)
	}
	return strings.Join(parts, ",")
}

func decodeTourStops(encoded string) []PTZTourStop {
	out := []PTZTourStop{}
	for _, part := range strings.Split(encoded, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		token, dwellText, hasDwell := strings.Cut(part, ":")
		stop := PTZTourStop{Preset: strings.TrimSpace(token)}
		if hasDwell {
			if dwell, err := strconv.Atoi(strings.TrimSpace(dwellText)); err == nil {
				stop.DwellSeconds = dwell
			}
		}
		if stop.Preset == "" {
			continue
		}
		out = append(out, stop)
	}
	return out
}

// ApplyRulePTZRecall points a camera at what a rule just detected, if the rule asks for it.
//
// ONE implementation, called from BOTH paths that raise an alert for a rule: the vision
// monitor and the manual create-alert API. The API path already goes out of its way to give
// a hand-raised alert parity with the monitor — the recorder trigger and the notification
// are both there — and a recall missing from it would mean "what happens when this rule
// fires" had two answers depending on which code raised it. It would also make the rule
// editor's own Test button prove nothing about the half of the rule that moves a camera.
//
// Diagnostics are deliberately NOT a caller. emitDiagnostics raises alert rows too, and a
// camera that swings to the gate because the detector failed to capture a frame would be a
// PTZ dome driven by the health of the software rather than by what it saw.
func ApplyRulePTZRecall(ctx context.Context, ptz PTZRecaller, ruleConfig string, ruleCameraId int64, ruleName string) error {
	if ptz == nil {
		return nil
	}
	recall := ParseRulePTZRecall(ruleConfig)
	if recall == nil {
		return nil
	}
	target := recall.CameraId
	if target <= 0 {
		// The common case: a PTZ camera watching its own scene, so the rule does not have
		// to name the camera it is already attached to.
		target = ruleCameraId
	}
	hold := recall.HoldSeconds
	if hold <= 0 {
		hold = ptzRecallDefaultHold
	}
	return ptz.Recall(ctx, PTZRecallRequest{
		CameraId: target, PresetToken: recall.Preset, HoldSeconds: hold, Reason: ruleName,
	})
}

// PTZRecaller is the slice of IPTZService the alert paths need: point a camera, nothing else.
type PTZRecaller interface {
	Recall(ctx context.Context, req PTZRecallRequest) error
}

// PTZRecallRule is a detection rule's "when this fires, point a camera at it" setting.
type PTZRecallRule struct {
	// CameraId is which camera to move. 0 means the rule's own camera, which is the
	// common case for a PTZ camera watching its own scene.
	CameraId int64 `json:"cameraId"`
	// Preset is the device token to recall.
	Preset      string `json:"preset"`
	HoldSeconds int    `json:"holdSeconds"`
}

// ParseRulePTZRecall extracts the PTZ recall from a rule's ruleConfig JSON, or nil when
// the rule does not ask for one.
//
// It rides in ruleConfig rather than in a column for the same reason the destinations list
// does: it is routing for the rule's OUTCOME, it is edited with the rule, and adding it
// costs no migration on an appliance already in the field.
func ParseRulePTZRecall(ruleConfig string) *PTZRecallRule {
	if strings.TrimSpace(ruleConfig) == "" {
		return nil
	}
	var parsed struct {
		PTZRecall *PTZRecallRule `json:"ptzRecall"`
	}
	if err := json.Unmarshal([]byte(ruleConfig), &parsed); err != nil {
		return nil
	}
	if parsed.PTZRecall == nil || strings.TrimSpace(parsed.PTZRecall.Preset) == "" {
		return nil
	}
	return parsed.PTZRecall
}
