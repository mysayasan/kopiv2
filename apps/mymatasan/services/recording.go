package services

import (
	"context"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	"github.com/mysayasan/kopiv2/infra/recording"
)

type recordingService struct {
	segments dbsql.IGenericRepo[entities.RecordingSegment]
	configs  dbsql.IGenericRepo[entities.RecordingConfig]
}

func NewRecordingService(
	segmentRepo dbsql.IGenericRepo[entities.RecordingSegment],
	configRepo dbsql.IGenericRepo[entities.RecordingConfig],
) IRecordingService {
	return &recordingService{segments: segmentRepo, configs: configRepo}
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
		_ = os.Remove(p)
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
		CameraId:       req.CameraId,
		Enabled:        req.Enabled,
		PreRollSec:     req.PreRollSec,
		PostRollSec:    req.PostRollSec,
		StoragePath:    strings.TrimSpace(req.StoragePath),
		RetentionDays:  req.RetentionDays,
		SegmentMinutes: req.SegmentMinutes,
		StreamURL:         strings.TrimSpace(req.StreamURL),
		FallbackStreamUrl: strings.TrimSpace(req.FallbackStreamUrl),
		UpdatedAt:      now,
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

func (s *recordingService) PurgeOldSegments(ctx context.Context) (int, error) {
	cfgs, err := s.ListConfigs(ctx)
	if err != nil {
		return 0, err
	}
	deleted := 0
	for _, cfg := range cfgs {
		if !cfg.Enabled || cfg.RetentionDays <= 0 {
			continue
		}
		cutoff := time.Now().UTC().Add(-time.Duration(cfg.RetentionDays) * 24 * time.Hour).Unix()
		filters := []sqldataenums.Filter{
			{FieldName: "CameraId", Compare: sqldataenums.Equal, Value: cfg.CameraId},
			{FieldName: "StartedAt", Compare: sqldataenums.LessThan, Value: cutoff},
		}
		segs, _, err := s.segments.Get(ctx, "", 1000, 0, filters, nil)
		if err != nil {
			continue
		}
		for _, seg := range segs {
			if p := strings.TrimSpace(seg.FilePath); p != "" {
				_ = os.Remove(p)
			}
			if _, err := s.segments.DeleteById(ctx, "", uint64(seg.Id)); err == nil {
				deleted++
			}
		}
	}
	return deleted, nil
}
