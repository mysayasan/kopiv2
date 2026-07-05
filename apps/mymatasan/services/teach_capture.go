package services

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	"github.com/mysayasan/kopiv2/infra/vision"
)

// Teaching-session tuning. One session runs at a time (one camera being taught);
// frames are sampled about once a second, kept only when something is present in
// the ROI, spaced out, deduplicated by perceptual hash, and hard-capped so a
// forgotten session can't flood the disk.
const (
	teachTickInterval    = 900 * time.Millisecond
	teachKeepMinInterval = 1200 * time.Millisecond
	teachMotionMinRatio  = 0.02 // ≥2% of sampled ROI cells changed = something is there
	// Dedup threshold is deliberately tight (near-identical only): on low-texture
	// scenes (plain conveyor, flat wall) most of the frame hashes identically and
	// distinct objects can sit within a few bits of each other — verified with
	// synthetic flat-background frames, where ≤10 swallowed most keeps.
	teachDedupHamming = 4
	teachDedupWindow     = 12   // recent keeps compared for dedup
	teachSessionMaxKeeps = 250
	teachSessionMaxTime  = 15 * time.Minute

	// Sample-coach thresholds: below min the class isn't reliably learnable.
	teachCoachMinPerClass = 25
	teachCoachDiversity   = 10 // avg hamming between consecutive keeps below this = too-similar samples
)

// TeachCoachHint is one machine-readable coaching message; the frontend maps
// Code (+ params) to a localized sentence.
type TeachCoachHint struct {
	Code  string `json:"code"` // needMore | imbalance | lowDiversity | ready
	Class string `json:"class,omitempty"`
	// Have/Need must serialize even at zero — "I have 0 of the 25 samples" is
	// exactly the message a brand-new class needs.
	Have int `json:"have"`
	Need int `json:"need"`
}

// TeachSessionInfo is the wizard's poll payload for a skill's teaching state.
type TeachSessionInfo struct {
	Active      bool             `json:"active"`
	ClassLabel  string           `json:"classLabel,omitempty"`
	Captured    int              `json:"captured"`
	Seen        int              `json:"seen"`
	StartedAt   int64            `json:"startedAt,omitempty"`
	LastImageId int64            `json:"lastImageId,omitempty"`
	LastError   string           `json:"lastError,omitempty"`
	DatasetId   int64            `json:"datasetId"`
	Classes     []string         `json:"classes"`
	Counts      map[string]int   `json:"counts"`
	Coach       []TeachCoachHint `json:"coach"`
}

// TeachActiveSession identifies a camera that is currently in a teaching
// session (drives the side-nav "learning" badge).
type TeachActiveSession struct {
	SkillId    int64  `json:"skillId"`
	CameraId   int64  `json:"cameraId"`
	ClassLabel string `json:"classLabel"`
}

// teachSessionRecord is one completed session appended to the skill's Sessions
// JSON — the audit trail of what was shown when.
type teachSessionRecord struct {
	Class     string `json:"class"`
	StartedAt int64  `json:"startedAt"`
	EndedAt   int64  `json:"endedAt"`
	Captured  int    `json:"captured"`
}

// teachRuntime is the in-memory state of the single active session.
type teachRuntime struct {
	skillId    int64
	cameraId   int64
	datasetId  int64
	classLabel string
	userId     int64
	// requireMotion gates keeps on ROI presence (object/inspect sessions show
	// items to the camera). Anomaly sessions record everyday operation, which
	// may be perfectly still — they keep on cadence + dedup instead.
	requireMotion bool
	cancel        context.CancelFunc
	done          chan struct{}

	mu          sync.Mutex
	captured    int
	seen        int
	startedAt   int64
	lastImageId int64
	lastError   string
	keptHashes  []uint64
	hamSum      int
	hamCount    int
}

func (r *teachRuntime) snapshot() (captured, seen int, startedAt, lastImageId int64, lastError string, avgHam int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	avg := 0
	if r.hamCount > 0 {
		avg = r.hamSum / r.hamCount
	}
	return r.captured, r.seen, r.startedAt, r.lastImageId, r.lastError, avg
}

// teachSkillClasses derives the class labels a skill teaches. Inspect skills
// train a contrast pair — the target ("defective bottle cap") plus its negative
// ("not defective bottle cap") — so the model learns the difference; object
// skills teach a single class; anomaly skills collect ONLY the normal side
// ("not <slug>") — the target class is whatever deviates from it, so it never
// has samples. Slugs stay lowercase-with-spaces (label rule).
func teachSkillClasses(skill *entities.TeachSkill) []string {
	slug := teachClassSlug(skill.Name)
	if slug == "" {
		return nil
	}
	switch skill.SkillType {
	case TeachSkillTypeInspect:
		return []string{slug, "not " + slug}
	case TeachSkillTypeAnomaly:
		return []string{"not " + slug}
	default:
		return []string{slug}
	}
}

func (s *teachService) StartSession(ctx context.Context, skillId uint64, classLabel string, userId int64) (*TeachSessionInfo, error) {
	skill, err := s.skills.GetById(ctx, "", skillId)
	if err != nil {
		return nil, err
	}
	if skill == nil {
		return nil, errors.New("skill not found")
	}
	if skill.CameraId == 0 {
		return nil, errors.New("pick a camera for this skill first")
	}
	classLabel = strings.TrimSpace(strings.ToLower(classLabel))
	classes := teachSkillClasses(skill)
	valid := false
	for _, c := range classes {
		if c == classLabel {
			valid = true
			break
		}
	}
	if !valid {
		return nil, errors.New("unknown class for this skill")
	}
	if s.training == nil || s.source == nil {
		return nil, errors.New("teaching is not available (capture backend missing)")
	}

	s.sessMu.Lock()
	if s.runtime != nil {
		s.sessMu.Unlock()
		return nil, errors.New("another teaching session is already running — stop it first")
	}

	// First session materializes the skill's backing dataset.
	if skill.DatasetId == 0 {
		dataset, derr := s.training.SaveDataset(ctx, TrainingDatasetRequest{
			Name:        "Skill: " + skill.Name,
			Description: "Auto-created by the Teach wizard",
			Classes:     classes,
		}, userId)
		if derr != nil {
			s.sessMu.Unlock()
			return nil, derr
		}
		skill.DatasetId = dataset.Id
	}
	skill.Status = TeachStatusCollecting
	if skill.Step < teachStepCollect {
		skill.Step = teachStepCollect
	}
	skill.UpdatedBy = userId
	skill.UpdatedAt = s.now().UTC().Unix()
	if _, err := s.skills.UpdateById(ctx, "", *skill); err != nil {
		s.sessMu.Unlock()
		return nil, err
	}

	runCtx, cancel := context.WithCancel(context.Background())
	rt := &teachRuntime{
		skillId:       skill.Id,
		cameraId:      skill.CameraId,
		datasetId:     skill.DatasetId,
		classLabel:    classLabel,
		userId:        userId,
		requireMotion: skill.SkillType != TeachSkillTypeAnomaly,
		cancel:        cancel,
		done:          make(chan struct{}),
		startedAt:     s.now().UTC().Unix(),
	}
	s.runtime = rt
	s.sessMu.Unlock()

	go s.captureLoop(runCtx, rt, skill.RoiPolygon)
	return s.SessionInfo(ctx, skillId)
}

// captureLoop is the session engine: grab → presence-gate → space out → dedup →
// store with the session's class label. It finalizes the session record and
// clears the runtime slot on exit (stop, cap, timeout, or cancellation).
func (s *teachService) captureLoop(ctx context.Context, rt *teachRuntime, roiPolygon string) {
	defer func() {
		s.finalizeSession(rt)
		s.sessMu.Lock()
		if s.runtime == rt {
			s.runtime = nil
		}
		s.sessMu.Unlock()
		close(rt.done)
	}()

	gate := vision.NewPresenceGate(roiPolygon)
	fallbackBox := vision.ZoneBounds(roiPolygon)
	deadline := time.Now().Add(teachSessionMaxTime)
	ticker := time.NewTicker(teachTickInterval)
	defer ticker.Stop()
	var lastKeep time.Time

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if time.Now().After(deadline) {
			return
		}

		grabCtx, grabCancel := context.WithTimeout(ctx, 5*time.Second)
		frame, err := s.source.Capture(grabCtx, rt.cameraId)
		grabCancel()
		if err != nil {
			rt.mu.Lock()
			rt.lastError = err.Error()
			rt.mu.Unlock()
			continue
		}
		result, err := gate.Feed(frame.Data)
		rt.mu.Lock()
		rt.seen++
		if err != nil {
			rt.lastError = err.Error()
			rt.mu.Unlock()
			continue
		}
		rt.lastError = ""
		dup := false
		for i := len(rt.keptHashes) - 1; i >= 0 && i >= len(rt.keptHashes)-teachDedupWindow; i-- {
			if vision.HammingDistance(rt.keptHashes[i], result.Hash) <= teachDedupHamming {
				dup = true
				break
			}
		}
		rt.mu.Unlock()

		if !result.Primed {
			continue
		}
		if rt.requireMotion && (!result.HasMotion || result.MotionRatio < teachMotionMinRatio) {
			continue
		}
		if time.Since(lastKeep) < teachKeepMinInterval || dup {
			continue
		}

		box := result.Box
		if box.W*box.H < 0.01 {
			box = fallbackBox
		}
		annotation := TrainingAnnotation{
			ClassName: rt.classLabel,
			X:         box.X, Y: box.Y, W: box.W, H: box.H,
			Source: "auto",
		}
		storeCtx, storeCancel := context.WithTimeout(context.Background(), 10*time.Second)
		img, err := s.training.StoreCapture(storeCtx, rt.datasetId, frame.Data, []TrainingAnnotation{annotation}, rt.userId)
		storeCancel()
		if err != nil {
			rt.mu.Lock()
			rt.lastError = err.Error()
			rt.mu.Unlock()
			continue
		}
		lastKeep = time.Now()

		rt.mu.Lock()
		if n := len(rt.keptHashes); n > 0 {
			rt.hamSum += vision.HammingDistance(rt.keptHashes[n-1], result.Hash)
			rt.hamCount++
		}
		rt.keptHashes = append(rt.keptHashes, result.Hash)
		rt.captured++
		rt.lastImageId = img.Id
		captured := rt.captured
		rt.mu.Unlock()

		if captured >= teachSessionMaxKeeps {
			return
		}
	}
}

// finalizeSession appends the completed session to the skill's audit trail.
func (s *teachService) finalizeSession(rt *teachRuntime) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	skill, err := s.skills.GetById(ctx, "", uint64(rt.skillId))
	if err != nil || skill == nil {
		return
	}
	var records []teachSessionRecord
	if strings.TrimSpace(skill.Sessions) != "" {
		_ = json.Unmarshal([]byte(skill.Sessions), &records)
	}
	captured, _, startedAt, _, _, _ := rt.snapshot()
	records = append(records, teachSessionRecord{
		Class:     rt.classLabel,
		StartedAt: startedAt,
		EndedAt:   s.now().UTC().Unix(),
		Captured:  captured,
	})
	if encoded, err := json.Marshal(records); err == nil {
		skill.Sessions = string(encoded)
	}
	skill.UpdatedBy = rt.userId
	skill.UpdatedAt = s.now().UTC().Unix()
	_, _ = s.skills.UpdateById(ctx, "", *skill)
}

func (s *teachService) StopSession(ctx context.Context, skillId uint64, userId int64) (*TeachSessionInfo, error) {
	s.sessMu.Lock()
	rt := s.runtime
	s.sessMu.Unlock()
	if rt != nil && rt.skillId == int64(skillId) {
		rt.cancel()
		select {
		case <-rt.done:
		case <-time.After(3 * time.Second):
		}
	}
	return s.SessionInfo(ctx, skillId)
}

func (s *teachService) SessionInfo(ctx context.Context, skillId uint64) (*TeachSessionInfo, error) {
	skill, err := s.skills.GetById(ctx, "", skillId)
	if err != nil {
		return nil, err
	}
	if skill == nil {
		return nil, errors.New("skill not found")
	}
	classes := teachSkillClasses(skill)
	counts, _ := s.classCounts(ctx, skill, classes)
	info := &TeachSessionInfo{
		DatasetId: skill.DatasetId,
		Classes:   classes,
		Counts:    counts,
	}

	avgHam := 0
	s.sessMu.Lock()
	rt := s.runtime
	s.sessMu.Unlock()
	if rt != nil && rt.skillId == skill.Id {
		info.Active = true
		info.ClassLabel = rt.classLabel
		var lastError string
		info.Captured, info.Seen, info.StartedAt, info.LastImageId, lastError, avgHam = rt.snapshot()
		info.LastError = lastError
	}

	info.Coach = buildTeachCoach(classes, info.Counts, info.Active, info.ClassLabel, info.Captured, avgHam)
	return info, nil
}

// buildTeachCoach turns raw counts into the plain-language coaching hints.
func buildTeachCoach(classes []string, counts map[string]int, active bool, activeClass string, sessionKeeps int, avgHam int) []TeachCoachHint {
	hints := make([]TeachCoachHint, 0, len(classes)+2)
	allReady := len(classes) > 0
	minCount, maxCount := -1, 0
	minClass := ""
	for _, c := range classes {
		have := counts[c]
		if have < teachCoachMinPerClass {
			hints = append(hints, TeachCoachHint{Code: "needMore", Class: c, Have: have, Need: teachCoachMinPerClass})
			allReady = false
		}
		if minCount < 0 || have < minCount {
			minCount, minClass = have, c
		}
		if have > maxCount {
			maxCount = have
		}
	}
	if maxCount >= teachCoachMinPerClass && minCount >= 0 && minCount < maxCount/5 {
		hints = append(hints, TeachCoachHint{Code: "imbalance", Class: minClass, Have: minCount})
	}
	if active && sessionKeeps >= 8 && avgHam > 0 && avgHam < teachCoachDiversity {
		hints = append(hints, TeachCoachHint{Code: "lowDiversity", Class: activeClass})
	}
	if allReady {
		hints = append(hints, TeachCoachHint{Code: "ready"})
	}
	return hints
}

func (s *teachService) ActiveSessions(ctx context.Context) []TeachActiveSession {
	s.sessMu.Lock()
	defer s.sessMu.Unlock()
	if s.runtime == nil {
		return []TeachActiveSession{}
	}
	return []TeachActiveSession{{
		SkillId:    s.runtime.skillId,
		CameraId:   s.runtime.cameraId,
		ClassLabel: s.runtime.classLabel,
	}}
}
