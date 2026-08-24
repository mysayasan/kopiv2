package services

import (
	"sync"
	"time"
)

// The PTZ motion journal: the one place that knows a camera's view changed because WE
// changed it.
//
// It exists because two features that never referred to each other are physically the same
// event. The tamper monitor's MOVED verdict is "this camera is no longer showing what it
// used to show" — which is the literal, intended, successful outcome of every preset
// recall, every tour step and every jog of the PTZ ring. Without this journal, turning on a
// guard tour makes the appliance alert that somebody has re-aimed the camera, every few
// minutes, forever; and the operator's fix is to switch tamper detection off, after which
// it protects nothing. The same is true, once, for every manual move.
//
// It is also the arbiter between the operator and the automation. A tour that steps while
// somebody is driving the camera takes it away mid-look; an alarm recall that a tour
// immediately overrides shows the alarm scene for three seconds. Both are settled by one
// question — "does a person currently have this camera?" — and this is where the answer
// lives.
//
// One object, constructed once and handed to everything that moves a camera or judges its
// picture: cameraService (jogs), ptzService (presets, tours, recalls) and
// CameraTamperMonitor (reads). A shared object rather than a call between services because
// the alternative is a dependency cycle — the tamper monitor already depends on the camera
// service — and because a fact recorded in one place cannot be recorded in only some of the
// paths that cause it.

// PTZJournal records, per camera, when we last commanded a move, whether a tour currently
// owns the camera, and until when an operator has it.
type PTZJournal struct {
	mu      sync.Mutex
	cameras map[int64]*ptzCameraMotion
	now     func() time.Time
}

type ptzCameraMotion struct {
	// lastCommandedAt is when we last told this camera to move, by any route. The tamper
	// monitor compares it against what it has already accounted for.
	lastCommandedAt int64
	// manualUntil is when an operator's claim on the camera expires. While it is in the
	// future, automation defers.
	manualUntil int64
	// touring is true while a tour is stepping this camera. Distinct from
	// lastCommandedAt: a tour means the view is SUPPOSED to keep changing indefinitely,
	// which no settling period covers.
	touring bool
}

// PTZMotion is what a reader gets back.
type PTZMotion struct {
	LastCommandedAt int64
	Touring         bool
	ManualUntil     int64
}

// NewPTZJournal creates an empty journal.
func NewPTZJournal() *PTZJournal {
	return &PTZJournal{cameras: map[int64]*ptzCameraMotion{}, now: time.Now}
}

// NoteCommandedMove records that we moved this camera. Called by every path that does:
// a jog, a preset recall, a home, an absolute move, a tour step.
func (j *PTZJournal) NoteCommandedMove(cameraId int64) {
	if j == nil || cameraId <= 0 {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	j.entry(cameraId).lastCommandedAt = j.now().UTC().Unix()
}

// ClaimManual records that a PERSON is driving this camera, and for how long automation
// should keep out of the way.
//
// The hold is refreshed on every manual command rather than set once, so an operator
// working a camera for two minutes keeps it for two minutes; the tour resumes a fixed
// interval after they stop, not after they start.
func (j *PTZJournal) ClaimManual(cameraId int64, hold time.Duration) {
	if j == nil || cameraId <= 0 {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	entry := j.entry(cameraId)
	now := j.now().UTC()
	entry.lastCommandedAt = now.Unix()
	until := now.Add(hold).Unix()
	// Never SHORTEN an existing claim. Two operators on the same camera, or a long hold
	// followed by a short one, must not hand the camera back early.
	if until > entry.manualUntil {
		entry.manualUntil = until
	}
}

// ReleaseManual drops an operator's claim immediately, so a tour resumes on the next tick
// instead of after the hold expires. Used when a person explicitly hands the camera back.
func (j *PTZJournal) ReleaseManual(cameraId int64) {
	if j == nil || cameraId <= 0 {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	j.entry(cameraId).manualUntil = 0
}

// SetTouring records whether a tour currently owns this camera.
func (j *PTZJournal) SetTouring(cameraId int64, touring bool) {
	if j == nil || cameraId <= 0 {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	j.entry(cameraId).touring = touring
}

// ManualHeld reports whether a person currently has this camera.
func (j *PTZJournal) ManualHeld(cameraId int64) bool {
	if j == nil {
		return false
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	entry, ok := j.cameras[cameraId]
	if !ok {
		return false
	}
	return entry.manualUntil > j.now().UTC().Unix()
}

// Motion reports what is known about a camera's PTZ activity. Safe on a nil journal, which
// is what an appliance with no PTZ wiring has — every reader then sees "never moved, not
// touring" and behaves exactly as it did before this file existed.
func (j *PTZJournal) Motion(cameraId int64) PTZMotion {
	if j == nil {
		return PTZMotion{}
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	entry, ok := j.cameras[cameraId]
	if !ok {
		return PTZMotion{}
	}
	return PTZMotion{
		LastCommandedAt: entry.lastCommandedAt,
		Touring:         entry.touring,
		ManualUntil:     entry.manualUntil,
	}
}

// Forget drops a camera's entry. Called when a camera is deleted so the map does not grow
// for the life of the process.
func (j *PTZJournal) Forget(cameraId int64) {
	if j == nil {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	delete(j.cameras, cameraId)
}

func (j *PTZJournal) entry(cameraId int64) *ptzCameraMotion {
	entry, ok := j.cameras[cameraId]
	if !ok {
		entry = &ptzCameraMotion{}
		j.cameras[cameraId] = entry
	}
	return entry
}
