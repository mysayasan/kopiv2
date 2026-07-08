package services

import (
	"context"
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
// seek offset into it.
type ObservationResult struct {
	*entities.ObjectObservation
	SegmentId    int64  `json:"segmentId"`
	SegmentCodec string `json:"segmentCodec"`
	SeekSeconds  int64  `json:"seekSeconds"`
}

// ObservationService is the read/maintenance side of the metadata recorder: it
// searches recorded observations and resolves each to the footage covering it.
type ObservationService struct {
	repo      dbsql.IGenericRepo[entities.ObjectObservation]
	recording IRecordingService
}

// NewObservationService builds the query service. recording is used to resolve the
// footage segment covering each observation and to align retention with recordings.
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
	var filters []sqldataenums.Filter
	if cameraId > 0 {
		filters = append(filters, sqldataenums.Filter{FieldName: "CameraId", Compare: sqldataenums.Equal, Value: cameraId})
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
	results := make([]ObservationResult, 0, len(rows))
	for _, row := range rows {
		res := ObservationResult{ObjectObservation: row}
		if seg := s.coveringSegment(ctx, row.CameraId, row.StartedAt); seg != nil {
			res.SegmentId = seg.Id
			res.SegmentCodec = seg.Codec
			if row.StartedAt > seg.StartedAt {
				res.SeekSeconds = row.StartedAt - seg.StartedAt
			}
		}
		results = append(results, res)
	}
	return results, total, nil
}

// coveringSegment returns the recording segment whose time span contains `at`, or nil
// when the moment was not recorded (recording off, or a gap). It fetches the single
// segment with the greatest StartedAt <= at and checks it actually spans `at`.
func (s *ObservationService) coveringSegment(ctx context.Context, cameraId, at int64) *entities.RecordingSegment {
	if s.recording == nil || at <= 0 {
		return nil
	}
	segs, _, err := s.recording.GetSegments(ctx, 1, 0, cameraId, 0, 0, at+1)
	if err != nil || len(segs) == 0 {
		return nil
	}
	seg := segs[0]
	if seg == nil || seg.StartedAt > at || (seg.EndedAt > 0 && seg.EndedAt < at) {
		return nil
	}
	return seg
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
