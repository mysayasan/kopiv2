package services

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	"github.com/mysayasan/kopiv2/infra/recording"
)

// purgeBatchSize is how many expired segments one retention pass claims at a time.
// Purges drain in batches rather than one capped page, so a backlog can be caught up.
const purgeBatchSize = 500

type recordingService struct {
	segments dbsql.IGenericRepo[entities.RecordingSegment]
	configs  dbsql.IGenericRepo[entities.RecordingConfig]
	// shredPasses > 0 securely overwrites segment files before deleting them; 0 =
	// plain delete. Applies to manual deletes and the retention purge.
	shredPasses int
}

func NewRecordingService(
	segmentRepo dbsql.IGenericRepo[entities.RecordingSegment],
	configRepo dbsql.IGenericRepo[entities.RecordingConfig],
	shredPasses int,
) IRecordingService {
	return &recordingService{segments: segmentRepo, configs: configRepo, shredPasses: shredPasses}
}

func (s *recordingService) GetSegments(ctx context.Context, limit, offset uint64, cameraId, alertId, startedAfter, startedBefore int64) ([]*entities.RecordingSegment, uint64, error) {
	var filters []sqldataenums.Filter
	if cameraId > 0 {
		filters = append(filters, sqldataenums.Filter{FieldName: "CameraId", Compare: sqldataenums.Equal, Value: cameraId})
	}
	if alertId > 0 {
		filters = append(filters, sqldataenums.Filter{FieldName: "AlertId", Compare: sqldataenums.Equal, Value: alertId})
	}
	if startedAfter > 0 {
		filters = append(filters, sqldataenums.Filter{FieldName: "StartedAt", Compare: sqldataenums.GreaterThanOrEqualTo, Value: startedAfter})
	}
	if startedBefore > 0 {
		filters = append(filters, sqldataenums.Filter{FieldName: "StartedAt", Compare: sqldataenums.LessThan, Value: startedBefore})
	}
	sorters := []sqldataenums.Sorter{{FieldName: "StartedAt", Sort: sqldataenums.DESC}}
	return s.segments.Get(ctx, "", limit, offset, filters, sorters)
}

func (s *recordingService) GetSegmentById(ctx context.Context, id uint64) (*entities.RecordingSegment, error) {
	return s.segments.GetById(ctx, "", id)
}

func (s *recordingService) SaveSegment(ctx context.Context, seg recording.SegmentResult) error {
	// Deduplicate: if a record with the same file path already exists, skip.
	if strings.TrimSpace(seg.FilePath) != "" {
		filters := []sqldataenums.Filter{
			{FieldName: "FilePath", Compare: sqldataenums.Equal, Value: seg.FilePath},
		}
		if existing, _ := s.segments.GetSingle(ctx, "", filters); existing != nil {
			return nil
		}
	}
	now := time.Now().UTC().Unix()
	entity := entities.RecordingSegment{
		CameraId:  seg.CameraId,
		AlertId:   seg.AlertId,
		FilePath:  seg.FilePath,
		StartedAt: seg.StartedAt,
		EndedAt:   seg.EndedAt,
		FileSize:  seg.FileSize,
		Codec:     seg.Codec,
		Sha256:    seg.Sha256,
		CreatedAt: now,
	}
	_, err := s.segments.Create(ctx, "", entity)
	return err
}

func (s *recordingService) DeleteSegment(ctx context.Context, id uint64) error {
	seg, err := s.segments.GetById(ctx, "", id)
	if err != nil {
		return err
	}
	if _, err := s.segments.DeleteById(ctx, "", id); err != nil {
		return err
	}
	if p := strings.TrimSpace(seg.FilePath); p != "" {
		_ = recording.SecureRemove(p, s.shredPasses)
	}
	return nil
}

func (s *recordingService) GetConfig(ctx context.Context, cameraId int64) (*entities.RecordingConfig, error) {
	filters := []sqldataenums.Filter{
		{FieldName: "CameraId", Compare: sqldataenums.Equal, Value: cameraId},
	}
	cfg, err := s.configs.GetSingle(ctx, "", filters)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "no result") {
			return nil, nil
		}
		return nil, err
	}
	return cfg, nil
}

func (s *recordingService) ListConfigs(ctx context.Context) ([]*entities.RecordingConfig, error) {
	sorters := []sqldataenums.Sorter{{FieldName: "CameraId", Sort: sqldataenums.ASC}}
	cfgs, _, err := s.configs.Get(ctx, "", 1000, 0, nil, sorters)
	return cfgs, err
}

func (s *recordingService) SaveConfig(ctx context.Context, req SaveRecordingConfigRequest) (*entities.RecordingConfig, error) {
	if req.CameraId <= 0 {
		return nil, errors.New("cameraId is required")
	}
	now := time.Now().UTC().Unix()

	existing, err := s.GetConfig(ctx, req.CameraId)
	if err != nil {
		return nil, err
	}

	cfg := entities.RecordingConfig{
		CameraId:          req.CameraId,
		Enabled:           req.Enabled,
		PreRollSec:        req.PreRollSec,
		PostRollSec:       req.PostRollSec,
		StoragePath:       strings.TrimSpace(req.StoragePath),
		RetentionDays:     req.RetentionDays,
		SegmentMinutes:    req.SegmentMinutes,
		LiveStreamUrl:     strings.TrimSpace(req.LiveStreamUrl),
		StreamURL:         strings.TrimSpace(req.StreamURL),
		FallbackStreamUrl: strings.TrimSpace(req.FallbackStreamUrl),
		// Object-metadata capture is paired with recording: without footage there is
		// nothing for a metadata search to jump to, so it tracks Enabled directly
		// rather than being a separate toggle. Enable recording → object search works.
		MetadataEnabled:    req.Enabled,
		MetadataGapSeconds: req.MetadataGapSeconds,
		UpdatedAt:          now,
	}

	if existing != nil {
		cfg.Id = existing.Id
		cfg.CreatedAt = existing.CreatedAt
		if _, err := s.configs.UpdateById(ctx, "", cfg); err != nil {
			return nil, err
		}
		return &cfg, nil
	}

	cfg.CreatedAt = now
	id, err := s.configs.Create(ctx, "", cfg)
	if err != nil {
		return nil, err
	}
	cfg.Id = int64(id)
	return &cfg, nil
}

// DeleteConfigForCamera removes a camera's recording config. Used by the camera-delete
// cascade, after that camera's segments have been purged — dropping the config first
// would strand the footage, since retention is driven off the config row.
func (s *recordingService) DeleteConfigForCamera(ctx context.Context, cameraId int64) error {
	if cameraId <= 0 {
		return errors.New("cameraId is required")
	}
	cfg, err := s.GetConfig(ctx, cameraId)
	if err != nil || cfg == nil {
		return err
	}
	_, err = s.configs.DeleteById(ctx, "", uint64(cfg.Id))
	return err
}

func (s *recordingService) PurgeOldSegments(ctx context.Context) (int, error) {
	cfgs, err := s.ListConfigs(ctx)
	if err != nil {
		return 0, err
	}
	deleted := 0
	for _, cfg := range cfgs {
		// NOTE: retention deliberately does NOT check cfg.Enabled. Turning recording off
		// must not freeze that camera's existing footage on disk forever — it only stops
		// NEW segments being written. Gating the purge on Enabled meant a camera with 30
		// days of footage kept it indefinitely the moment an operator disabled recording.
		if cfg.RetentionDays <= 0 {
			continue
		}
		cutoff := time.Now().UTC().Add(-time.Duration(cfg.RetentionDays) * 24 * time.Hour).Unix()
		filters := []sqldataenums.Filter{
			{FieldName: "CameraId", Compare: sqldataenums.Equal, Value: cfg.CameraId},
			{FieldName: "StartedAt", Compare: sqldataenums.LessThan, Value: cutoff},
		}
		// Oldest first, and drain in batches until the camera has no expired segments
		// left. The previous single capped page meant a backlog (retention shortened, or
		// the app offline for a while) could never be caught up: each run deleted at most
		// one page and left the rest to accumulate.
		sorters := []sqldataenums.Sorter{{FieldName: "StartedAt", Sort: sqldataenums.ASC}}
		for {
			segs, _, err := s.segments.Get(ctx, "", purgeBatchSize, 0, filters, sorters)
			if err != nil || len(segs) == 0 {
				break
			}
			progressed := false
			for _, seg := range segs {
				if p := strings.TrimSpace(seg.FilePath); p != "" {
					_ = recording.SecureRemove(p, s.shredPasses)
				}
				if _, err := s.segments.DeleteById(ctx, "", uint64(seg.Id)); err == nil {
					deleted++
					progressed = true
				}
			}
			// Every row in the batch failed to delete — the next query would return the
			// same page forever, so stop rather than spin.
			if !progressed {
				break
			}
		}
	}
	return deleted, nil
}

// PurgeAllForCamera deletes EVERY recorded segment for one camera regardless of its
// retention/expiry, securely removing each file. It reads oldest-first batches (deleting
// each batch before the next read, so the window advances without offset drift). Returns
// the number of segments deleted. Used by the per-camera "Purge now" action.
func (s *recordingService) PurgeAllForCamera(ctx context.Context, cameraId int64) (int, error) {
	if cameraId <= 0 {
		return 0, errors.New("cameraId is required")
	}
	filters := []sqldataenums.Filter{
		{FieldName: "CameraId", Compare: sqldataenums.Equal, Value: cameraId},
	}
	sorters := []sqldataenums.Sorter{{FieldName: "StartedAt", Sort: sqldataenums.ASC}}
	deleted := 0
	for {
		batch, _, err := s.segments.Get(ctx, "", 500, 0, filters, sorters)
		if err != nil {
			return deleted, err
		}
		if len(batch) == 0 {
			break
		}
		for _, seg := range batch {
			if _, err := s.segments.DeleteById(ctx, "", uint64(seg.Id)); err != nil {
				return deleted, err
			}
			deleted++
			if p := strings.TrimSpace(seg.FilePath); p != "" {
				_ = recording.SecureRemove(p, s.shredPasses)
			}
		}
		if len(batch) < 500 {
			break
		}
	}
	return deleted, nil
}

func (s *recordingService) PurgeOldestSegments(ctx context.Context, keepAfter int64, wantBytes int64) (int, int64, error) {
	filters := []sqldataenums.Filter{
		{FieldName: "StartedAt", Compare: sqldataenums.LessThan, Value: keepAfter},
	}
	sorters := []sqldataenums.Sorter{{FieldName: "StartedAt", Sort: sqldataenums.ASC}}
	segs, _, err := s.segments.Get(ctx, "", 1000, 0, filters, sorters)
	if err != nil {
		return 0, 0, err
	}
	deleted := 0
	var freed int64
	for _, seg := range segs {
		if wantBytes > 0 && freed >= wantBytes {
			break
		}
		if p := strings.TrimSpace(seg.FilePath); p != "" {
			_ = recording.SecureRemove(p, s.shredPasses)
		}
		if _, err := s.segments.DeleteById(ctx, "", uint64(seg.Id)); err == nil {
			deleted++
			freed += seg.FileSize
		}
	}
	return deleted, freed, nil
}
