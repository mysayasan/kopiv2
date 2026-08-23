package services

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	"github.com/mysayasan/kopiv2/infra/safego"
	"github.com/mysayasan/kopiv2/infra/vision"
)

const (
	// defaultMetadataGapSeconds is how long a label may go unseen before its open
	// presence interval is closed and written. Roughly a few sample intervals, so a
	// brief occlusion or a dropped detection frame does not split one presence into
	// many rows.
	defaultMetadataGapSeconds = 5
	// metadataCloseTickSeconds is how often open intervals are checked for staleness
	// and the per-camera enable/gap config is refreshed.
	metadataCloseTickSeconds = 2
	// metadataWriteBuffer bounds the async write queue; closings are rare (one row per
	// presence span), so this only absorbs bursts (e.g. flush-all on shutdown).
	metadataWriteBuffer = 256
)

// RecordingConfigLister supplies the per-camera recording configs the metadata
// recorder reads its enable toggle and retention alignment from. Satisfied by
// IRecordingService.
type RecordingConfigLister interface {
	ListConfigs(ctx context.Context) ([]*entities.RecordingConfig, error)
}

// metaCamCfg is the cached per-camera metadata-recording config.
type metaCamCfg struct {
	enabled    bool
	gapSec     int
	appearance bool
}

// openObservation is an in-progress presence interval for one (camera,label).
type openObservation struct {
	label         string
	startedAt     int64
	lastSeen      int64
	maxConfidence float64
	maxCount      int
	sampleCount   int
	peakConf      float64
	peakBox       vision.Box
	peakAt        int64
	// peakAppearance is the appearance vector of the crop the peak box came from, kept so
	// the interval can be described by its CLEAREST view rather than by whichever frame
	// happened to close it. It moves with peakBox/peakAt and for the same reason: a
	// descriptor taken from a half-occluded final frame ranks badly against every future
	// query, and nothing downstream could tell that was why.
	peakAppearance      []float32
	peakAppearanceModel string
}

// MetadataRecorder records "what objects each camera saw" as presence intervals. It
// implements vision.ObservationSink: the detector forwards every candidate list here
// (reusing the alert inference — no second video decode), and this coalesces
// consecutive sightings of a label into one interval row, flushed once the label has
// been absent for the camera's gap window.
type MetadataRecorder struct {
	repo          dbsql.IGenericRepo[entities.ObjectObservation]
	configs       RecordingConfigLister
	minConfidence float64
	defaultGapSec int
	now           func() time.Time

	mu     sync.Mutex
	open   map[int64]map[string]*openObservation // camera -> label -> interval
	lastAt map[int64]int64                       // camera -> last observed capturedAt (dedup)

	cfgMu sync.RWMutex
	cfg   map[int64]metaCamCfg

	writeCh chan pendingObservation
	// appearance persists the peak crop's descriptor for each written observation. Nil
	// disables the whole appearance leg, which is the state on any install that has not
	// turned it on — the recorder must work exactly as before in that case.
	appearance AppearanceStore
}

// AppearanceStore is the slice of appearance persistence the recorder needs. Narrowed to
// one method so the recorder can be tested without a cipher, a repo or a search path.
type AppearanceStore interface {
	Store(ctx context.Context, rec AppearanceRecord) error
}

// pendingObservation is a row and the descriptor that belongs to it, queued together.
//
// They travel as one item because the descriptor is keyed by the observation's id, which
// does not exist until the row is inserted. Queuing them separately would mean holding a
// vector while hoping the matching insert succeeds, and pairing them afterwards by
// (camera, label, time) — a join on values that are not unique.
type pendingObservation struct {
	entity     entities.ObjectObservation
	appearance []float32
	model      string
}

// NewMetadataRecorder builds the recorder. minConfidence filters weak candidates out
// of the metadata (mirrors the detector's object-confidence floor).
// SetAppearanceStore wires appearance persistence. Optional and settable after
// construction, because the appearance service needs the at-rest cipher, which is built
// later in the wiring than the recorder is.
func (r *MetadataRecorder) SetAppearanceStore(store AppearanceStore) {
	r.appearance = store
}

func NewMetadataRecorder(repo dbsql.IGenericRepo[entities.ObjectObservation], configs RecordingConfigLister, minConfidence float64) *MetadataRecorder {
	gap := defaultMetadataGapSeconds
	return &MetadataRecorder{
		repo:          repo,
		configs:       configs,
		minConfidence: minConfidence,
		defaultGapSec: gap,
		now:           time.Now,
		open:          map[int64]map[string]*openObservation{},
		lastAt:        map[int64]int64{},
		cfg:           map[int64]metaCamCfg{},
		writeCh:       make(chan pendingObservation, metadataWriteBuffer),
	}
}

// Start launches the background config-refresh/interval-close ticker and the async
// DB writer. On ctx cancellation it flushes every open interval so no presence is
// lost on shutdown.
func (r *MetadataRecorder) Start(ctx context.Context) {
	// Supervised: the writer drains the observation queue, so if it dies the queue
	// backs up and every sighting is lost with no signal.
	safego.Supervise(ctx, "mymatasan.metadata.recorder", r.run)
	safego.Supervise(ctx, "mymatasan.metadata.writer", r.writer)
}

// Observe implements vision.ObservationSink. It is called per sampled frame with the
// full candidate list; gated by the per-camera toggle, deduped per frame, and folded
// into the open presence intervals.
func (r *MetadataRecorder) Observe(cameraID int64, capturedAt int64, candidates []vision.ObjectCandidate) {
	cfg, ok := r.cameraConfig(cameraID)
	if !ok || !cfg.enabled {
		return
	}
	if capturedAt <= 0 {
		capturedAt = r.now().UTC().Unix()
	}

	// Fold this frame's candidates per label first (outside the state lock): count,
	// best confidence and its box.
	type frameAgg struct {
		count    int
		bestConf float64
		bestBox  vision.Box
		// bestAppearance belongs to the SAME candidate as bestBox. Tracking it separately
		// (say, "the first vector seen this frame") would pair one object's descriptor with
		// another object's box on any frame holding two people.
		bestAppearance      []float32
		bestAppearanceModel string
	}
	perLabel := map[string]*frameAgg{}
	for _, c := range candidates {
		label := strings.ToLower(strings.TrimSpace(c.Label))
		if label == "" || c.Confidence < r.minConfidence {
			continue
		}
		a := perLabel[label]
		if a == nil {
			a = &frameAgg{}
			perLabel[label] = a
		}
		a.count++
		if c.Confidence > a.bestConf {
			a.bestConf = c.Confidence
			a.bestBox = c.Box
			a.bestAppearance = c.Appearance
			a.bestAppearanceModel = c.AppearanceModel
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	// The same frame can arrive from both the rule Detect path and the metadata-only
	// ObserveOnly path; ignore a repeat of the last capturedAt so it is counted once.
	if last := r.lastAt[cameraID]; last == capturedAt && capturedAt != 0 {
		return
	}
	r.lastAt[cameraID] = capturedAt

	labels := r.open[cameraID]
	if labels == nil {
		labels = map[string]*openObservation{}
		r.open[cameraID] = labels
	}
	for label, a := range perLabel {
		iv := labels[label]
		if iv == nil {
			iv = &openObservation{label: label, startedAt: capturedAt}
			labels[label] = iv
		}
		iv.lastSeen = capturedAt
		iv.sampleCount++
		if a.count > iv.maxCount {
			iv.maxCount = a.count
		}
		if a.bestConf > iv.maxConfidence {
			iv.maxConfidence = a.bestConf
		}
		if a.bestConf > iv.peakConf {
			iv.peakConf = a.bestConf
			iv.peakBox = a.bestBox
			iv.peakAt = capturedAt
			// Only overwrite the kept descriptor when this frame actually produced one.
			// The appearance stage skips crops that are too small or too uncertain, so a
			// frame can raise the peak confidence and carry no vector — and clearing the
			// one already held would lose the only description of an interval because a
			// later, marginally-better-scored frame happened to be a distant view.
			if len(a.bestAppearance) > 0 {
				iv.peakAppearance = a.bestAppearance
				iv.peakAppearanceModel = a.bestAppearanceModel
			}
		}
	}
}

// Observed reports whether the recorder already folded a candidate list for this
// exact (camera, frame). The live monitor uses it to decide whether a metadata-only
// ObserveOnly pass is still needed, so a frame is never inferred twice.
func (r *MetadataRecorder) Observed(cameraID int64, capturedAt int64) bool {
	if capturedAt == 0 {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastAt[cameraID] == capturedAt
}

// EnabledCameras returns the set of cameras with metadata recording on, so the live
// monitor samples them even when they have no alert rules.
func (r *MetadataRecorder) EnabledCameras() map[int64]bool {
	r.cfgMu.RLock()
	defer r.cfgMu.RUnlock()
	out := make(map[int64]bool, len(r.cfg))
	for id, c := range r.cfg {
		if c.enabled {
			out[id] = true
		}
	}
	return out
}

// IsEnabled reports whether metadata recording is on for a camera.
func (r *MetadataRecorder) IsEnabled(cameraID int64) bool {
	r.cfgMu.RLock()
	defer r.cfgMu.RUnlock()
	return r.cfg[cameraID].enabled
}

// IsAppearanceEnabled reports whether this camera should have appearance descriptors
// computed for its sightings. It is the per-camera compute gate the sampler reads, in the
// same shape as the LPR and face gates: the expensive stage runs only where it was asked
// for. It also returns false when the appearance store is not wired, so a build or an
// install without it never pays for vectors that have nowhere to go.
func (r *MetadataRecorder) IsAppearanceEnabled(cameraID int64) bool {
	if r == nil || r.appearance == nil {
		return false
	}
	r.cfgMu.RLock()
	defer r.cfgMu.RUnlock()
	return r.cfg[cameraID].appearance
}

func (r *MetadataRecorder) run(ctx context.Context) {
	r.refreshConfig(ctx)
	t := time.NewTicker(metadataCloseTickSeconds * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			r.flushAll()
			return
		case <-t.C:
			r.refreshConfig(ctx)
			r.closeStale()
		}
	}
}

// refreshConfig reloads the per-camera enable/gap config from the recording configs.
// The DB read happens outside every state lock; the result is swapped under cfgMu.
func (r *MetadataRecorder) refreshConfig(ctx context.Context) {
	cfgs, err := r.configs.ListConfigs(ctx)
	if err != nil {
		return
	}
	next := make(map[int64]metaCamCfg, len(cfgs))
	for _, c := range cfgs {
		if c == nil {
			continue
		}
		gap := c.MetadataGapSeconds
		if gap <= 0 {
			gap = r.defaultGapSec
		}
		// Appearance is meaningless without the observation row it attaches to, so it is
		// AND-ed with metadata recording here rather than trusted from the column alone.
		// A config that says appearance-on / metadata-off would otherwise make the worker
		// embed crops on every frame and then throw every vector away.
		next[c.CameraId] = metaCamCfg{
			enabled:    c.MetadataEnabled,
			gapSec:     gap,
			appearance: c.MetadataEnabled && c.AppearanceEnabled,
		}
	}
	r.cfgMu.Lock()
	r.cfg = next
	r.cfgMu.Unlock()
}

func (r *MetadataRecorder) cameraConfig(cameraID int64) (metaCamCfg, bool) {
	r.cfgMu.RLock()
	defer r.cfgMu.RUnlock()
	c, ok := r.cfg[cameraID]
	return c, ok
}

// closeStale writes and removes any open interval whose label has been absent for at
// least the camera's gap window.
func (r *MetadataRecorder) closeStale() {
	now := r.now().UTC().Unix()
	var toWrite []pendingObservation
	r.mu.Lock()
	for cam, labels := range r.open {
		cfg, _ := r.cameraConfig(cam)
		gap := cfg.gapSec
		if gap <= 0 {
			gap = r.defaultGapSec
		}
		for label, iv := range labels {
			if now-iv.lastSeen >= int64(gap) {
				toWrite = append(toWrite, r.toEntity(cam, iv))
				delete(labels, label)
			}
		}
		if len(labels) == 0 {
			delete(r.open, cam)
		}
	}
	r.mu.Unlock()
	for _, e := range toWrite {
		r.enqueue(e)
	}
}

// flushAll closes every open interval regardless of gap (shutdown path).
func (r *MetadataRecorder) flushAll() {
	var toWrite []pendingObservation
	r.mu.Lock()
	for cam, labels := range r.open {
		for _, iv := range labels {
			toWrite = append(toWrite, r.toEntity(cam, iv))
		}
		delete(r.open, cam)
	}
	r.mu.Unlock()
	for _, e := range toWrite {
		// Shutdown: write synchronously so nothing is lost when the writer goroutine
		// is also draining.
		r.write(e)
	}
}

func (r *MetadataRecorder) toEntity(cam int64, iv *openObservation) pendingObservation {
	boxJSON, _ := json.Marshal(iv.peakBox)
	return pendingObservation{
		entity: entities.ObjectObservation{
			CameraId:      cam,
			Label:         iv.label,
			StartedAt:     iv.startedAt,
			EndedAt:       iv.lastSeen,
			MaxConfidence: iv.maxConfidence,
			MaxCount:      iv.maxCount,
			SampleCount:   iv.sampleCount,
			PeakBox:       string(boxJSON),
			PeakAt:        iv.peakAt,
			CreatedAt:     r.now().UTC().Unix(),
		},
		appearance: iv.peakAppearance,
		model:      iv.peakAppearanceModel,
	}
}

func (r *MetadataRecorder) enqueue(e pendingObservation) {
	select {
	case r.writeCh <- e:
	default:
		// Queue full: write inline rather than drop a recorded presence.
		r.write(e)
	}
}

func (r *MetadataRecorder) writer(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			for {
				select {
				case e := <-r.writeCh:
					r.write(e)
				default:
					return
				}
			}
		case e := <-r.writeCh:
			r.write(e)
		}
	}
}

func (r *MetadataRecorder) write(p pendingObservation) {
	ctx := context.Background()
	id, err := r.repo.Create(ctx, "", p.entity)
	if err != nil {
		log.Printf("metadata recorder: write observation cam%d %q failed: %v",
			p.entity.CameraId, p.entity.Label, err)
		return
	}
	if r.appearance == nil || len(p.appearance) == 0 {
		return
	}
	// The descriptor is written AFTER the row it points at, and its failure is logged but
	// not propagated: losing the ability to rank one sighting by appearance is a much
	// smaller harm than losing the record that the sighting happened, and the two must not
	// share a fate. An orphaned descriptor is impossible in this order; an orphaned
	// observation is merely one that cannot be searched by appearance.
	rec := AppearanceRecord{
		ObservationId: int64(id),
		CameraId:      p.entity.CameraId,
		SeenAt:        p.entity.PeakAt,
		Label:         p.entity.Label,
		Confidence:    p.entity.MaxConfidence,
		Vector:        p.appearance,
		Model:         p.model,
	}
	if rec.SeenAt <= 0 {
		rec.SeenAt = p.entity.StartedAt
	}
	if serr := r.appearance.Store(ctx, rec); serr != nil {
		log.Printf("metadata recorder: store appearance for observation %d cam%d %q failed: %v",
			id, p.entity.CameraId, p.entity.Label, serr)
	}
}
