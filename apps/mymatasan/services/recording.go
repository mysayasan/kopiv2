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
	// guard is the case-file footage hold: footage an OPEN case points at is not deleted
	// by any of the three purges below. Passed in as a pointer that the composition root
	// fills in later, because the case service needs this service — see case_hold.go.
	// nil blocks nothing.
	guard *FootageGuard
}

func NewRecordingService(
	segmentRepo dbsql.IGenericRepo[entities.RecordingSegment],
	configRepo dbsql.IGenericRepo[entities.RecordingConfig],
	shredPasses int,
	guard *FootageGuard,
) IRecordingService {
	return &recordingService{segments: segmentRepo, configs: configRepo, shredPasses: shredPasses, guard: guard}
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
		// Appearance is NOT paired with recording the way metadata is. Metadata is free
		// (it reuses an inference the detector already ran); appearance is a neural-network
		// forward pass per person or vehicle in every sampled frame, so switching recording
		// on must not switch it on too. It stays exactly where the operator left it.
		AppearanceEnabled: req.AppearanceEnabled && req.Enabled,
		UpdatedAt:         now,
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
		// One hold read per camera, not one per segment.
		held := s.guard.CameraHolds(ctx, cfg.CameraId)
		// heldSoFar counts the expired segments this camera kept because a case wants
		// them. They stay at the front of the oldest-first window forever, so without
		// paging past them the loop would re-read the same batch until it gave up — and
		// every segment behind them would outlive its retention.
		var heldSoFar uint64
		for {
			segs, _, err := s.segments.Get(ctx, "", purgeBatchSize, heldSoFar, filters, sorters)
			if err != nil || len(segs) == 0 {
				break
			}
			progressed := false
			for _, seg := range segs {
				if blocked, _ := held.BlockedSegment(seg); blocked {
					heldSoFar++
					continue
				}
				if p := strings.TrimSpace(seg.FilePath); p != "" {
					_ = recording.SecureRemove(p, s.shredPasses)
				}
				if _, err := s.segments.DeleteById(ctx, "", uint64(seg.Id)); err == nil {
					deleted++
					progressed = true
				}
			}
			// Nothing in the batch could be deleted — either every row failed, or every
			// row is held. The next query would return the same page forever, so stop
			// rather than spin.
			if !progressed {
				break
			}
		}
	}
	return deleted, nil
}

// PurgeAllForCamera deletes EVERY recorded segment for one camera regardless of its
// retention/expiry OR of any case hold, securely removing each file. It is the
// camera-delete cascade's purge; the operator-facing "Purge now" is PurgeCameraFootage,
// which keeps footage an open case is holding. It reads oldest-first batches, deleting
// each batch before the next read. Returns the number of segments deleted.
func (s *recordingService) PurgeAllForCamera(ctx context.Context, cameraId int64) (int, error) {
	if cameraId <= 0 {
		return 0, errors.New("cameraId is required")
	}
	res, err := s.purgeCamera(ctx, cameraId, &HeldSpans{})
	return res.Deleted, err
}

// PurgeCameraFootage is the per-camera "Purge now" action: the same destruction as
// PurgeAllForCamera, except that footage an OPEN case is holding is kept and reported.
//
// The two are separate methods rather than a flag because the difference is a policy
// decision and it should be visible at the call site. The cascade behind DELETE camera
// takes the unconditional one — the camera is being removed, and footage held by a case
// nobody can find or release afterwards is worse than footage that is gone. The operator's
// destroy button takes this one: an open investigation's evidence must not be one click
// from gone, and the way to destroy it is to close or empty the case, which is a decision
// with somebody's name on it.
func (s *recordingService) PurgeCameraFootage(ctx context.Context, cameraId int64) (PurgeFootageResult, error) {
	if cameraId <= 0 {
		return PurgeFootageResult{}, errors.New("cameraId is required")
	}
	return s.purgeCamera(ctx, cameraId, s.guard.CameraHolds(ctx, cameraId))
}

// purgeCamera drains one camera's segments oldest-first, skipping anything held.
func (s *recordingService) purgeCamera(ctx context.Context, cameraId int64, held *HeldSpans) (PurgeFootageResult, error) {
	filters := []sqldataenums.Filter{
		{FieldName: "CameraId", Compare: sqldataenums.Equal, Value: cameraId},
	}
	sorters := []sqldataenums.Sorter{{FieldName: "StartedAt", Sort: sqldataenums.ASC}}
	var res PurgeFootageResult
	// Held rows stay at the front of the window, so the read has to page past them or the
	// loop re-reads the same batch and everything behind them survives a purge that
	// reported success.
	var heldSoFar uint64
	for {
		batch, _, err := s.segments.Get(ctx, "", 500, heldSoFar, filters, sorters)
		if err != nil {
			return res, err
		}
		if len(batch) == 0 {
			break
		}
		before := res.Deleted
		for _, seg := range batch {
			if blocked, reason := held.BlockedSegment(seg); blocked {
				res.Kept++
				if res.Reason == "" {
					res.Reason = reason
				}
				heldSoFar++
				continue
			}
			if _, err := s.segments.DeleteById(ctx, "", uint64(seg.Id)); err != nil {
				return res, err
			}
			res.Deleted++
			if p := strings.TrimSpace(seg.FilePath); p != "" {
				_ = recording.SecureRemove(p, s.shredPasses)
			}
		}
		if len(batch) < 500 || res.Deleted == before {
			break
		}
	}
	return res, nil
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
	// The disk-pressure sweeper is the one that hurts to get wrong: it runs BECAUSE the
	// appliance is short of space, so the temptation is to let it take whatever it needs.
	// It must not take evidence. If the holds mean it cannot free enough, it frees what it
	// can and the caller escalates — the same rule the fleet clip archive follows, where a
	// full archive stops rather than evicting evidence.
	byCamera := map[int64]*HeldSpans{}
	for _, seg := range segs {
		if wantBytes > 0 && freed >= wantBytes {
			break
		}
		held, ok := byCamera[seg.CameraId]
		if !ok {
			held = s.guard.CameraHolds(ctx, seg.CameraId)
			byCamera[seg.CameraId] = held
		}
		if blocked, _ := held.BlockedSegment(seg); blocked {
			continue
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
