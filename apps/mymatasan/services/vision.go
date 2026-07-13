package services

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	"github.com/mysayasan/kopiv2/infra/vision"
)

type visionService struct {
	rules  dbsql.IGenericRepo[entities.DetectionRule]
	alerts dbsql.IGenericRepo[entities.AlertEvent]
}

// NewVisionService creates a service for AI detection rules and alert events.
func NewVisionService(ruleRepo dbsql.IGenericRepo[entities.DetectionRule], alertRepo dbsql.IGenericRepo[entities.AlertEvent]) IVisionService {
	return &visionService{rules: ruleRepo, alerts: alertRepo}
}

func (s *visionService) GetRules(ctx context.Context, limit uint64, offset uint64) ([]*entities.DetectionRule, uint64, error) {
	sorters := []sqldataenums.Sorter{{FieldName: "UpdatedAt", Sort: sqldataenums.DESC}}
	return s.rules.Get(ctx, "", limit, offset, nil, sorters)
}

func (s *visionService) SaveRule(ctx context.Context, req DetectionRuleRequest, userId int64) (*entities.DetectionRule, error) {
	spec := vision.NormalizeDetectionRule(req)
	if err := vision.ValidateDetectionRule(spec); err != nil {
		return nil, err
	}
	rule := detectionRuleEntity(spec)
	now := time.Now().UTC().Unix()
	rule.UpdatedBy = userId
	rule.UpdatedAt = now

	if rule.Id > 0 {
		existing, err := s.rules.GetById(ctx, "", uint64(rule.Id))
		if err != nil {
			return nil, err
		}
		rule.CreatedAt = existing.CreatedAt
		rule.CreatedBy = existing.CreatedBy
		rule.LastTriggeredAt = existing.LastTriggeredAt
		if _, err := s.rules.UpdateById(ctx, "", rule); err != nil {
			return nil, err
		}
		return &rule, nil
	}

	rule.CreatedBy = userId
	rule.CreatedAt = now
	id, err := s.rules.Create(ctx, "", rule)
	if err != nil {
		return nil, err
	}
	rule.Id = int64(id)
	return &rule, nil
}

func (s *visionService) DeleteRule(ctx context.Context, id uint64) (uint64, error) {
	return s.rules.DeleteById(ctx, "", id)
}

// MarkRuleTriggered persists the moment a rule fired, so its cooldown survives a restart.
//
// Cooldown is otherwise process-local: it starts empty on every boot, so a rule with a
// 30-minute cooldown re-fires immediately after a restart, and a crash-restart loop turns
// that into an alert storm, a notification storm, and an ffmpeg storm (every alert
// extracts a clip). LastTriggeredAt was already being read back into the detector — it
// just was never written.
func (s *visionService) MarkRuleTriggered(ctx context.Context, ruleId int64, at int64) error {
	if ruleId <= 0 || at <= 0 {
		return nil
	}
	rule, err := s.rules.GetById(ctx, "", uint64(ruleId))
	if err != nil {
		return err
	}
	if rule == nil {
		return nil
	}
	// Samples can land out of order (a slow camera's frame arriving after a fast one);
	// never move the trigger time backwards or the cooldown would shorten.
	if rule.LastTriggeredAt >= at {
		return nil
	}
	rule.LastTriggeredAt = at
	_, err = s.rules.UpdateById(ctx, "", *rule)
	return err
}

// DeleteRulesForCamera removes every detection rule belonging to one camera. Used by
// the camera-delete cascade: an orphaned rule keeps the vision monitor sampling a
// camera that no longer exists, which fails every interval and writes a capture-failed
// diagnostic alert each time.
func (s *visionService) DeleteRulesForCamera(ctx context.Context, cameraId int64) (int, error) {
	if cameraId <= 0 {
		return 0, fmt.Errorf("cameraId is required")
	}
	filters := []sqldataenums.Filter{
		{FieldName: "CameraId", Compare: sqldataenums.Equal, Value: cameraId},
	}
	deleted := 0
	for {
		batch, _, err := s.rules.Get(ctx, "", 500, 0, filters, nil)
		if err != nil {
			return deleted, err
		}
		if len(batch) == 0 {
			return deleted, nil
		}
		for _, rule := range batch {
			if _, err := s.rules.DeleteById(ctx, "", uint64(rule.Id)); err != nil {
				return deleted, err
			}
			deleted++
		}
	}
}

// GetAlerts lists alert events with true DB-side filtering/sorting/paging. The camera
// and status are mandatory base constraints; extraFilters/extraSorters come straight
// from the client DataTable (column filters + sort), letting the grid drive the query.
// When no sort is supplied the newest events come first.
func (s *visionService) GetAlerts(ctx context.Context, limit uint64, offset uint64, cameraId int64, status string, extraFilters []sqldataenums.Filter, extraSorters []sqldataenums.Sorter) ([]*entities.AlertEvent, uint64, error) {
	var filters []sqldataenums.Filter
	if cameraId > 0 {
		filters = append(filters, sqldataenums.Filter{FieldName: "CameraId", Compare: sqldataenums.Equal, Value: cameraId})
	}
	switch status {
	case "active":
		// Real, unacknowledged detections (exclude diagnostics).
		filters = append(filters,
			sqldataenums.Filter{FieldName: "IsAcknowledged", Compare: sqldataenums.Equal, Value: false},
			sqldataenums.Filter{FieldName: "IsDiagnostic", Compare: sqldataenums.Equal, Value: false},
		)
	case "detections":
		// All real detections regardless of acknowledgement (exclude diagnostics).
		// The events feed and recording timeline use this so periodic "sampled"
		// diagnostics can never evict real alerts from a bounded result window.
		filters = append(filters, sqldataenums.Filter{FieldName: "IsDiagnostic", Compare: sqldataenums.Equal, Value: false})
	case "acknowledged":
		filters = append(filters, sqldataenums.Filter{FieldName: "IsAcknowledged", Compare: sqldataenums.Equal, Value: true})
	case "diagnostic":
		filters = append(filters, sqldataenums.Filter{FieldName: "IsDiagnostic", Compare: sqldataenums.Equal, Value: true})
	}
	// Column filters from the grid (createdAt range, label, ruleId, confidence, …).
	filters = append(filters, extraFilters...)
	sorters := extraSorters
	if len(sorters) == 0 {
		sorters = []sqldataenums.Sorter{{FieldName: "CreatedAt", Sort: sqldataenums.DESC}}
	}
	return s.alerts.Get(ctx, "", limit, offset, filters, sorters)
}

func (s *visionService) GetAlertById(ctx context.Context, id uint64) (*entities.AlertEvent, error) {
	return s.alerts.GetById(ctx, "", id)
}

func (s *visionService) CreateAlert(ctx context.Context, req AlertEventRequest, userId int64) (*entities.AlertEvent, error) {
	spec := vision.NormalizeAlertEvent(req)
	if err := vision.ValidateAlertEvent(spec); err != nil {
		return nil, err
	}
	alert := alertEventEntity(spec)
	alert.IsDiagnostic = isDiagnosticMetadata(alert.Metadata)
	now := time.Now().UTC().Unix()
	alert.CreatedBy = userId
	alert.CreatedAt = now
	alert.UpdatedBy = userId
	alert.UpdatedAt = now

	id, err := s.alerts.Create(ctx, "", alert)
	if err != nil {
		return nil, err
	}
	alert.Id = int64(id)
	return &alert, nil
}

func (s *visionService) AcknowledgeAlert(ctx context.Context, id uint64, userId int64) (*entities.AlertEvent, error) {
	alert, err := s.alerts.GetById(ctx, "", id)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC().Unix()
	alert.IsAcknowledged = true
	alert.AcknowledgedBy = userId
	alert.AcknowledgedAt = now
	alert.UpdatedBy = userId
	alert.UpdatedAt = now
	if _, err := s.alerts.UpdateById(ctx, "", *alert); err != nil {
		return nil, err
	}
	return alert, nil
}

// PurgeAlerts deletes alert events created before olderThan (unix seconds),
// reclaiming both the rows and their snapshot image files. When onlyDiagnostics
// is true, real detections are kept and only Vision-monitor diagnostics are
// removed (those carry no snapshot, so nothing is unlinked). Deletes in oldest-
// first batches so a large backlog cannot build an unbounded result set. Returns
// the number of rows deleted.
func (s *visionService) PurgeAlerts(ctx context.Context, olderThan int64, onlyDiagnostics bool) (int, error) {
	if olderThan <= 0 {
		return 0, fmt.Errorf("olderThan must be greater than zero")
	}
	filters := []sqldataenums.Filter{
		{FieldName: "CreatedAt", Compare: sqldataenums.LessThan, Value: olderThan},
	}
	if onlyDiagnostics {
		filters = append(filters, sqldataenums.Filter{FieldName: "IsDiagnostic", Compare: sqldataenums.Equal, Value: true})
	}
	sorters := []sqldataenums.Sorter{{FieldName: "CreatedAt", Sort: sqldataenums.ASC}}
	deleted := 0
	removedSnaps := map[string]bool{}
	for {
		// Always read the oldest batch at offset 0; each batch is deleted before the
		// next read, so the window naturally advances without offset drift.
		batch, _, err := s.alerts.Get(ctx, "", 500, 0, filters, sorters)
		if err != nil {
			return deleted, err
		}
		if len(batch) == 0 {
			break
		}
		for _, a := range batch {
			if _, err := s.alerts.DeleteById(ctx, "", uint64(a.Id)); err != nil {
				return deleted, err
			}
			deleted++
			// Snapshots are shared per frame (snap_<capturedAt>.jpg), so dedupe and
			// remove each file once. Alerts sharing a snapshot share a CreatedAt, so
			// they are always purged in the same sweep — the file is never orphaned
			// from a surviving alert.
			if p := strings.TrimSpace(a.SnapshotPath); p != "" && !removedSnaps[p] {
				removedSnaps[p] = true
				_ = os.Remove(p)
			}
		}
		if len(batch) < 500 {
			break
		}
	}
	return deleted, nil
}

// PurgeAlertsForCamera deletes EVERY alert event for one camera regardless of age,
// reclaiming both the rows and their snapshot image files (deduped, since snapshots are
// shared per frame). Reads oldest-first batches. Returns the number of alerts deleted.
// Used by the per-camera "Purge now" action.
func (s *visionService) PurgeAlertsForCamera(ctx context.Context, cameraId int64) (int, error) {
	if cameraId <= 0 {
		return 0, fmt.Errorf("cameraId is required")
	}
	filters := []sqldataenums.Filter{
		{FieldName: "CameraId", Compare: sqldataenums.Equal, Value: cameraId},
	}
	sorters := []sqldataenums.Sorter{{FieldName: "CreatedAt", Sort: sqldataenums.ASC}}
	deleted := 0
	removedSnaps := map[string]bool{}
	for {
		batch, _, err := s.alerts.Get(ctx, "", 500, 0, filters, sorters)
		if err != nil {
			return deleted, err
		}
		if len(batch) == 0 {
			break
		}
		for _, a := range batch {
			if _, err := s.alerts.DeleteById(ctx, "", uint64(a.Id)); err != nil {
				return deleted, err
			}
			deleted++
			if p := strings.TrimSpace(a.SnapshotPath); p != "" && !removedSnaps[p] {
				removedSnaps[p] = true
				_ = os.Remove(p)
			}
		}
		if len(batch) < 500 {
			break
		}
	}
	return deleted, nil
}

// PurgeAlertsOlderThanDays purges alerts older than the given number of days. A
// days value <= 0 purges everything up to now (olderThan = current time).
func (s *visionService) PurgeAlertsOlderThanDays(ctx context.Context, days int, onlyDiagnostics bool) (int, error) {
	cutoff := time.Now().UTC().Unix()
	if days > 0 {
		cutoff = time.Now().UTC().AddDate(0, 0, -days).Unix()
	}
	return s.PurgeAlerts(ctx, cutoff, onlyDiagnostics)
}

func detectionRuleEntity(rule vision.DetectionRule) entities.DetectionRule {
	return entities.DetectionRule{
		Id:              rule.Id,
		CameraId:        rule.CameraId,
		Name:            rule.Name,
		DetectionType:   rule.DetectionType,
		ZonePolygon:     rule.ZonePolygon,
		RuleConfig:      rule.RuleConfig,
		SchedulePolicy:  rule.SchedulePolicy,
		Threshold:       rule.Threshold,
		MinFrames:       rule.MinFrames,
		CooldownSeconds: rule.CooldownSeconds,
		SoundEnabled:    rule.SoundEnabled,
		IsEnabled:       rule.IsEnabled,
		LastTriggeredAt: rule.LastTriggeredAt,
	}
}

func alertEventEntity(alert vision.AlertEvent) entities.AlertEvent {
	return entities.AlertEvent{
		Id:             alert.Id,
		RuleId:         alert.RuleId,
		CameraId:       alert.CameraId,
		DetectionType:  alert.DetectionType,
		Label:          alert.Label,
		Confidence:     alert.Confidence,
		ZonePolygon:    alert.ZonePolygon,
		BoundingBox:    alert.BoundingBox,
		SnapshotPath:   alert.SnapshotPath,
		Metadata:       alert.Metadata,
		IsAcknowledged: alert.IsAcknowledged,
	}
}
