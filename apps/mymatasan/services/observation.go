package services

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// defaultObservationRetentionDays bounds metadata growth for cameras whose recording
// config has no explicit retention (metadata on, continuous NVR off). Per-camera
// recording retention, when set, wins so metadata is purged in step with its footage.
const defaultObservationRetentionDays = 30

// ObservationResult is one presence interval enriched with the footage link the UI
// needs for click-to-play: the covering recording segment (0 when none) and the
// seek offset into it. FootagePending marks a sighting that was recorded but whose
// segment file has not been finalized to disk/DB yet (the current, still-writing
// segment) — it has no playable link *yet*, but footage exists and is coming.
type ObservationResult struct {
	*entities.ObjectObservation
	SegmentId      int64  `json:"segmentId"`
	SegmentCodec   string `json:"segmentCodec"`
	SeekSeconds    int64  `json:"seekSeconds"`
	FootagePending bool   `json:"footagePending,omitempty"`
}

// ObservationService is the read/maintenance side of the metadata recorder: it
// searches recorded observations and resolves each to the footage covering it.
// AppearanceReaper is the slice of appearance storage the observation retention paths
// need. A descriptor MUST NOT outlive the sighting it describes: the footage and the index
// are gone, and what is left behind is a searchable record of a person the retention policy
// says has been forgotten. Nil disables the leg (installs without appearance search).
type AppearanceReaper interface {
	DeleteForObservations(ctx context.Context, observationIds []int64) (int, error)
	DeleteForCamera(ctx context.Context, cameraId int64) (int, error)
}

type ObservationService struct {
	repo      dbsql.IGenericRepo[entities.ObjectObservation]
	recording IRecordingService
	// appearance purges descriptors alongside the sightings they describe. Nil = off.
	appearance AppearanceReaper
}

// NewObservationService builds the query service. recording is used to resolve the
// footage segment covering each observation and to align retention with recordings.
// SetAppearanceReaper wires appearance purging. Set after construction because the
// appearance service needs the at-rest cipher, which is built later in the wiring.
func (s *ObservationService) SetAppearanceReaper(r AppearanceReaper) {
	s.appearance = r
}

func NewObservationService(repo dbsql.IGenericRepo[entities.ObjectObservation], recording IRecordingService) *ObservationService {
	return &ObservationService{repo: repo, recording: recording}
}

// GetObservations returns presence intervals for a camera with true server-side
// filtering/sorting/paging: the camera is the mandatory base constraint, and
// extraFilters/extraSorters come straight from the client DataTable (column filters +
// sort — e.g. a Time daterange, an object Label, a MaxCount) so paging runs over the
// real filtered set. Each result is resolved to its covering footage segment. When no
// sort is supplied the newest intervals come first.
func (s *ObservationService) GetObservations(ctx context.Context, limit, offset uint64, cameraId int64, extraFilters []sqldataenums.Filter, extraSorters []sqldataenums.Sorter) ([]ObservationResult, uint64, error) {
	if limit == 0 {
		limit = 50
	}
	// Metadata recording is independent of NVR footage recording: a camera can log
	// observations in detect-only mode with continuous recording off, and detect-only
	// recorders keep no segments — so those sightings have no footage and never will.
	// Restrict the search to cameras actually recording footage so their footage-less
	// sightings don't fill pages with un-openable / forever-"Finalizing" rows. Doing it
	// at the query level (not post-fetch) keeps paging and the total count correct.
	// recordingOn is nil only when the config can't be read, in which case we don't
	// restrict (fail open — show everything rather than hide real results).
	recordingOn := s.camerasRecording(ctx)

	var filters []sqldataenums.Filter
	if cameraId > 0 {
		// Explicit camera pick: if we know it isn't recording, it has no footage to show.
		if recordingOn != nil && !recordingOn[cameraId] {
			return []ObservationResult{}, 0, nil
		}
		filters = append(filters, sqldataenums.Filter{FieldName: "CameraId", Compare: sqldataenums.Equal, Value: cameraId})
	} else if recordingOn != nil {
		ids := make([]int64, 0, len(recordingOn))
		for id := range recordingOn {
			ids = append(ids, id)
		}
		if len(ids) == 0 {
			return []ObservationResult{}, 0, nil // no camera is recording → nothing to show
		}
		filters = append(filters, sqldataenums.Filter{FieldName: "CameraId", Compare: sqldataenums.In, Value: ids})
	}
	filters = append(filters, extraFilters...)
	sorters := extraSorters
	if len(sorters) == 0 {
		sorters = []sqldataenums.Sorter{{FieldName: "StartedAt", Sort: sqldataenums.DESC}}
	}
	rows, total, err := s.repo.Get(ctx, "", limit, offset, filters, sorters)
	if err != nil {
		return nil, 0, err
	}
	// Resolve every row's covering footage segment in one batched sweep (grouped by
	// camera) rather than a query per row — the old per-row lookup was an N+1 that
	// scaled badly once the table grew. newestEnd[camera] is where that camera's saved
	// footage currently ends, used to tell a still-recording tail from a real gap.
	segs, newestEnd := s.resolveCoveringSegments(ctx, rows)
	results := make([]ObservationResult, 0, len(rows))
	for i, row := range rows {
		seg := segs[i]
		if seg != nil {
			// Recorded footage exists and is finalized — link it for click-to-play.
			// Seek to the peak-confidence frame (when known) rather than the interval
			// start, so the object is at its clearest and lines up with the drawn box.
			seekAt := row.StartedAt
			if row.PeakAt > 0 && row.PeakAt >= seg.StartedAt {
				seekAt = row.PeakAt
			}
			res := ObservationResult{ObjectObservation: row, SegmentId: seg.Id, SegmentCodec: seg.Codec}
			if seekAt > seg.StartedAt {
				res.SeekSeconds = seekAt - seg.StartedAt
			}
			results = append(results, res)
			continue
		}
		// No finalized segment covers the sighting. The camera is recording (query is
		// restricted to those), so two cases remain:
		//   • it is newer than the camera's newest saved footage → it falls in the
		//     current, still-writing segment (segments persist only on close), so the
		//     footage exists but isn't playable yet — surface it as pending, not gone.
		//   • it sits in an older gap (footage since purged) → nothing to play, so omit
		//     it rather than show an un-openable row.
		if row.StartedAt >= newestEnd[row.CameraId] {
			results = append(results, ObservationResult{ObjectObservation: row, FootagePending: true})
		}
	}
	return results, total, nil
}

// camerasRecording returns the set of cameras whose continuous NVR recording is on, so
// GetObservations can restrict the search to cameras that actually produce footage
// (detect-only cameras record metadata but keep no segments — nothing to watch). It
// returns a non-nil (possibly empty) map when the configs were read, and nil only on a
// read error, so callers can distinguish "nothing is recording" (empty map → show no
// results) from "unknown" (nil → don't restrict, fail open).
func (s *ObservationService) camerasRecording(ctx context.Context) map[int64]bool {
	if s.recording == nil {
		return nil
	}
	cfgs, err := s.recording.ListConfigs(ctx)
	if err != nil {
		return nil
	}
	on := make(map[int64]bool, len(cfgs))
	for _, cfg := range cfgs {
		if cfg != nil && cfg.Enabled {
			on[cfg.CameraId] = true
		}
	}
	return on
}

// resolveCoveringSegments maps each row (by position) to the recording segment that
// covers its sighting moment, or nil when the moment was not recorded. It groups the
// page's rows by camera and fetches each camera's candidate segments once — over the
// [earliest..latest] sighting window — instead of issuing one query per row. Segments
// come back newest-first; pickCovering then matches each row in memory. It also
// returns, per camera, where that camera's saved footage currently ends (the newest
// candidate's end), so the caller can distinguish a still-writing tail from a gap.
func (s *ObservationService) resolveCoveringSegments(ctx context.Context, rows []*entities.ObjectObservation) ([]*entities.RecordingSegment, map[int64]int64) {
	out := make([]*entities.RecordingSegment, len(rows))
	newestEnd := map[int64]int64{}
	if s.recording == nil || len(rows) == 0 {
		return out, newestEnd
	}
	type camSpan struct {
		min, max int64
		idxs     []int
	}
	byCam := map[int64]*camSpan{}
	for i, row := range rows {
		if row == nil || row.StartedAt <= 0 {
			continue
		}
		sp := byCam[row.CameraId]
		if sp == nil {
			sp = &camSpan{min: row.StartedAt, max: row.StartedAt}
			byCam[row.CameraId] = sp
		}
		if row.StartedAt < sp.min {
			sp.min = row.StartedAt
		}
		if row.StartedAt > sp.max {
			sp.max = row.StartedAt
		}
		sp.idxs = append(sp.idxs, i)
	}
	for cam, sp := range byCam {
		candidates := s.fetchCoveringCandidates(ctx, cam, sp.min, sp.max)
		for _, seg := range candidates {
			if seg == nil {
				continue
			}
			end := seg.EndedAt
			if end == 0 {
				end = seg.StartedAt // open/in-progress segment
			}
			if end > newestEnd[cam] {
				newestEnd[cam] = end
			}
		}
		for _, i := range sp.idxs {
			out[i] = pickCovering(candidates, rows[i].StartedAt)
		}
	}
	return out, newestEnd
}

// FootagePoint is one (camera, moment) to resolve to footage.
type FootagePoint struct {
	CameraId int64
	At       int64
}

// FootageRef is where a moment lives, or zeroes when nothing covers it.
type FootageRef struct {
	SegmentId int64 `json:"segmentId"`
	Seek      int64 `json:"seek"`
}

// ResolveFootageFor batches a set of moments to their covering segments.
//
// Appearance search needs this: a ranked hit an operator cannot open is a hit they cannot
// act on. It goes through the SAME pickCovering the object search uses — including the
// preference for continuous footage over a short event clip — so a sighting found by
// ranking opens the same file the Objects grid would open for it.
//
// Batched per camera because the naive version is one segment page-walk per hit, and a
// shortlist is up to two hundred of them.
func (s *ObservationService) ResolveFootageFor(ctx context.Context, points []FootagePoint) []FootageRef {
	out := make([]FootageRef, len(points))
	if s == nil || s.recording == nil || len(points) == 0 {
		return out
	}
	type camSpan struct {
		min, max int64
		idxs     []int
	}
	byCam := map[int64]*camSpan{}
	for i, p := range points {
		if p.CameraId <= 0 || p.At <= 0 {
			continue
		}
		sp := byCam[p.CameraId]
		if sp == nil {
			sp = &camSpan{min: p.At, max: p.At}
			byCam[p.CameraId] = sp
		}
		if p.At < sp.min {
			sp.min = p.At
		}
		if p.At > sp.max {
			sp.max = p.At
		}
		sp.idxs = append(sp.idxs, i)
	}
	for cam, sp := range byCam {
		candidates := coveringSegmentCandidates(ctx, s.recording, cam, sp.min, sp.max)
		for _, i := range sp.idxs {
			if seg := pickCovering(candidates, points[i].At); seg != nil {
				out[i] = FootageRef{SegmentId: seg.Id, Seek: points[i].At - seg.StartedAt}
			}
		}
	}
	return out
}

// fetchCoveringCandidates loads a camera's segments that could cover any moment in
// [minAt, maxAt], newest-first.
func (s *ObservationService) fetchCoveringCandidates(ctx context.Context, cameraId, minAt, maxAt int64) []*entities.RecordingSegment {
	return coveringSegmentCandidates(ctx, s.recording, cameraId, minAt, maxAt)
}

// segmentPager is the one read coveringSegmentCandidates needs. Narrowing it to this
// keeps the timeline's seek path callable from recordingService itself, which cannot
// depend on IRecordingService without a cycle.
type segmentPager interface {
	GetSegments(ctx context.Context, limit, offset uint64, cameraId, alertId, startedAfter, startedBefore int64) ([]*entities.RecordingSegment, uint64, error)
}

// coveringSegmentCandidates loads a camera's segments that could cover any moment in
// [minAt, maxAt], newest-first. It pages back (the repo caps each read at 100 rows)
// only until it reaches a *continuous* segment starting at/before minAt — the nearest
// full-footage segment preceding the earliest sighting is enough to cover it — then
// stops, so the sweep stays bounded regardless of how far back footage extends. The
// stop deliberately ignores short event clips: one can start after (yet end before)
// the moment a continuous segment actually covers, so stopping on it would miss the
// covering footage at a page boundary.
//
// Shared by object search (resolve a sighting to its footage) and by the playback
// timeline's seek (resolve a scrubbed moment to its footage). Those must agree: a
// sighting the search screen can play and the timeline cannot is the same footage
// described two ways.
func coveringSegmentCandidates(ctx context.Context, src segmentPager, cameraId, minAt, maxAt int64) []*entities.RecordingSegment {
	if src == nil {
		return nil
	}
	const pageSize = 100
	const maxPages = 50 // safety bound: at most ~5000 candidate segments
	var candidates []*entities.RecordingSegment
	for page := 0; page < maxPages; page++ {
		batch, _, err := src.GetSegments(ctx, pageSize, uint64(page*pageSize), cameraId, 0, 0, maxAt+1)
		if err != nil || len(batch) == 0 {
			break
		}
		reachedStart := false
		for _, seg := range batch {
			candidates = append(candidates, seg)
			if seg != nil && seg.AlertId == 0 && seg.StartedAt <= minAt {
				reachedStart = true
			}
		}
		if reachedStart || len(batch) < pageSize {
			break
		}
	}
	return candidates
}

// pickCovering returns the segment from candidates (ordered newest-first) whose span
// contains `at`, preferring a continuous segment (full footage) over a short event
// clip. Mirrors the original per-row logic, just over an in-memory slice.
func pickCovering(candidates []*entities.RecordingSegment, at int64) *entities.RecordingSegment {
	if at <= 0 {
		return nil
	}
	var fallback *entities.RecordingSegment
	for _, seg := range candidates {
		if seg == nil || seg.StartedAt > at {
			continue
		}
		// A segment covers `at` when it started at/before it and has not ended before it
		// (EndedAt == 0 means a still-open/in-progress segment).
		if seg.EndedAt != 0 && seg.EndedAt < at {
			continue
		}
		if seg.AlertId == 0 {
			return seg // continuous footage — best for context + accurate seeking
		}
		if fallback == nil {
			fallback = seg // an event clip that spans it, used only if no continuous one does
		}
	}
	return fallback
}

// Labels returns the distinct object labels observed for a camera (all cameras when
// cameraId <= 0), so the search UI can offer a filter list. It scans a bounded window
// of recent rows rather than a DISTINCT query, which keeps it engine-agnostic.
func (s *ObservationService) Labels(ctx context.Context, cameraId int64) ([]string, error) {
	var filters []sqldataenums.Filter
	if cameraId > 0 {
		filters = append(filters, sqldataenums.Filter{FieldName: "CameraId", Compare: sqldataenums.Equal, Value: cameraId})
	}
	sorters := []sqldataenums.Sorter{{FieldName: "StartedAt", Sort: sqldataenums.DESC}}
	rows, _, err := s.repo.Get(ctx, "", 2000, 0, filters, sorters)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	labels := make([]string, 0)
	for _, row := range rows {
		l := strings.TrimSpace(row.Label)
		if l == "" || seen[l] {
			continue
		}
		seen[l] = true
		labels = append(labels, l)
	}
	return labels, nil
}

// PurgeAllForCamera deletes every observation belonging to one camera, regardless of
// age. Used by the camera-delete cascade — retention is driven off the camera's
// recording config, so once that is gone these rows would never be purged again.
func (s *ObservationService) PurgeAllForCamera(ctx context.Context, cameraId int64) (int, error) {
	if cameraId <= 0 {
		return 0, fmt.Errorf("cameraId is required")
	}
	filters := []sqldataenums.Filter{
		{FieldName: "CameraId", Compare: sqldataenums.Equal, Value: cameraId},
	}
	// Descriptors go FIRST, by camera, in one statement. Deleting them per observation id
	// as the loop below progresses would leave every descriptor whose row-delete failed
	// stranded with no owner to find it by, and a camera-wide delete cannot miss any.
	if s.appearance != nil {
		if _, err := s.appearance.DeleteForCamera(ctx, cameraId); err != nil {
			return 0, err
		}
	}
	deleted := 0
	for {
		batch, _, err := s.repo.Get(ctx, "", 500, 0, filters, nil)
		if err != nil {
			return deleted, err
		}
		if len(batch) == 0 {
			return deleted, nil
		}
		progressed := false
		for _, row := range batch {
			if _, err := s.repo.DeleteById(ctx, "", uint64(row.Id)); err == nil {
				deleted++
				progressed = true
			}
		}
		if !progressed {
			return deleted, nil
		}
	}
}

// PurgeOldObservations deletes presence intervals past retention. Per-camera recording
// retention (when set) wins so metadata is dropped in step with its footage; cameras
// without a retention fall back to defaultObservationRetentionDays so metadata never
// grows unbounded. Returns the number of rows deleted.
func (s *ObservationService) PurgeOldObservations(ctx context.Context) (int, error) {
	cfgs, err := s.recording.ListConfigs(ctx)
	if err != nil {
		return 0, err
	}
	deleted := 0
	for _, cfg := range cfgs {
		if cfg == nil {
			continue
		}
		days := cfg.RetentionDays
		if days <= 0 {
			days = defaultObservationRetentionDays
		}
		cutoff := time.Now().UTC().AddDate(0, 0, -days).Unix()
		filters := []sqldataenums.Filter{
			{FieldName: "CameraId", Compare: sqldataenums.Equal, Value: cfg.CameraId},
			{FieldName: "EndedAt", Compare: sqldataenums.LessThan, Value: cutoff},
		}
		sorters := []sqldataenums.Sorter{{FieldName: "EndedAt", Sort: sqldataenums.ASC}}
		for {
			batch, _, err := s.repo.Get(ctx, "", 500, 0, filters, sorters)
			if err != nil {
				return deleted, err
			}
			if len(batch) == 0 {
				break
			}
			// Descriptors before rows, for the same reason as above: once an observation
			// row is gone, nothing points at its descriptor and no later sweep can find
			// it. Retention would then quietly stop applying to the appearance index.
			if s.appearance != nil {
				ids := make([]int64, 0, len(batch))
				for _, row := range batch {
					if row != nil {
						ids = append(ids, row.Id)
					}
				}
				if _, err := s.appearance.DeleteForObservations(ctx, ids); err != nil {
					return deleted, err
				}
			}
			for _, row := range batch {
				if _, err := s.repo.DeleteById(ctx, "", uint64(row.Id)); err == nil {
					deleted++
				}
			}
			if len(batch) < 500 {
				break
			}
		}
	}
	return deleted, nil
}
