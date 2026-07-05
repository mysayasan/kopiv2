package services

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"

	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	"github.com/mysayasan/kopiv2/infra/vision"
)

// Feedback verdicts on a live alert from an active skill.
const (
	TeachVerdictCorrect = "correct" // the model was right — reinforce the target class
	TeachVerdictWrong   = "wrong"   // false positive — teach it NOT to fire here
)

// TeachFeedbackAlert is one reviewable alert produced by a skill's rule.
type TeachFeedbackAlert struct {
	AlertId    int64   `json:"alertId"`
	Label      string  `json:"label"`
	Confidence float64 `json:"confidence"`
	CreatedAt  int64   `json:"createdAt"`
}

// ListSkillFeedback returns recent un-reviewed alerts from an active skill's
// rule, so the operator can confirm or correct them — the "keep teaching" loop.
func (s *teachService) ListSkillFeedback(ctx context.Context, skillId uint64) ([]*TeachFeedbackAlert, error) {
	skill, err := s.skills.GetById(ctx, "", skillId)
	if err != nil || skill == nil {
		return nil, errors.New("skill not found")
	}
	if skill.CameraId == 0 || s.vision == nil {
		return []*TeachFeedbackAlert{}, nil
	}
	slug := teachClassSlug(skill.Name)
	// Unacknowledged real detections on this skill's camera; filter to this
	// skill's class in Go (detectionType is the rule's slug, label is the raw
	// model label — either can carry the taught class).
	alerts, _, err := s.vision.GetAlerts(ctx, 60, 0, skill.CameraId, "active", nil, nil)
	if err != nil {
		return nil, err
	}
	out := make([]*TeachFeedbackAlert, 0, len(alerts))
	for _, a := range alerts {
		if a.SnapshotPath == "" {
			continue
		}
		if a.DetectionType != slug && strings.ToLower(a.Label) != slug {
			continue
		}
		out = append(out, &TeachFeedbackAlert{
			AlertId:    a.Id,
			Label:      a.Label,
			Confidence: a.Confidence,
			CreatedAt:  a.CreatedAt,
		})
	}
	return out, nil
}

// AddSkillFeedback files a verdict on an alert into the skill's dataset — the
// crux of the correction loop: confirmed detections reinforce the target
// class; false positives become a "not <slug>" example (inspect skills) or a
// background image (object skills) the model learns not to fire on. The alert
// is acknowledged so it leaves the review list. Returns the new sample count so
// the UI can nudge a re-check.
func (s *teachService) AddSkillFeedback(ctx context.Context, skillId uint64, alertId int64, verdict string, userId int64) error {
	if verdict != TeachVerdictCorrect && verdict != TeachVerdictWrong {
		return errors.New("verdict must be correct or wrong")
	}
	skill, err := s.skills.GetById(ctx, "", skillId)
	if err != nil || skill == nil {
		return errors.New("skill not found")
	}
	if skill.DatasetId == 0 || s.training == nil {
		return errors.New("skill has no dataset")
	}
	if s.vision == nil {
		return errors.New("alerts unavailable")
	}
	alert, err := s.vision.GetAlertById(ctx, uint64(alertId))
	if err != nil || alert == nil {
		return errors.New("alert not found")
	}
	data, err := s.readAlertSnapshot(alert)
	if err != nil {
		return err
	}

	slug := teachClassSlug(skill.Name)
	box := boxFromAlert(alert)
	switch verdict {
	case TeachVerdictCorrect:
		// Anomaly skills have no anomaly class to reinforce (they only model
		// normal); a confirmed alert is simply cleared. Other skills store the
		// snapshot as a confirmed example of the taught class.
		if skill.SkillType != TeachSkillTypeAnomaly {
			if _, err := s.training.StoreCapture(ctx, skill.DatasetId, data, []TrainingAnnotation{{
				ClassName: slug, X: box.X, Y: box.Y, W: box.W, H: box.H, Source: "feedback",
			}}, userId); err != nil {
				return err
			}
		}
	case TeachVerdictWrong:
		switch skill.SkillType {
		case TeachSkillTypeInspect:
			// The item was actually normal — teach the contrast class.
			if _, err := s.training.StoreCapture(ctx, skill.DatasetId, data, []TrainingAnnotation{{
				ClassName: "not " + slug, X: box.X, Y: box.Y, W: box.W, H: box.H, Source: "feedback",
			}}, userId); err != nil {
				return err
			}
		case TeachSkillTypeAnomaly:
			// A false alarm IS a normal moment the bank hasn't seen — add it to
			// the normal class so the next re-check absorbs it.
			if _, err := s.training.StoreCapture(ctx, skill.DatasetId, data, []TrainingAnnotation{{
				ClassName: "not " + slug, X: box.X, Y: box.Y, W: box.W, H: box.H, Source: "feedback",
			}}, userId); err != nil {
				return err
			}
		default:
			// Object skill: a false positive → a background image (nothing here).
			if _, err := s.training.StoreBackground(ctx, skill.DatasetId, data, userId); err != nil {
				return err
			}
		}
	}

	// Drop the alert off the review list.
	_, _ = s.vision.AcknowledgeAlert(ctx, uint64(alertId), userId)
	return nil
}

// readAlertSnapshot loads and decrypts an alert's stored snapshot.
func (s *teachService) readAlertSnapshot(alert *entities.AlertEvent) ([]byte, error) {
	if strings.TrimSpace(alert.SnapshotPath) == "" {
		return nil, errors.New("alert has no snapshot")
	}
	raw, err := os.ReadFile(alert.SnapshotPath)
	if err != nil {
		return nil, err
	}
	if s.snapshotCipher != nil {
		return s.snapshotCipher.DecryptBytes(raw)
	}
	return raw, nil
}

// boxFromAlert parses the alert's bounding box, falling back to the full frame
// when it is missing (still a valid, if loose, positive box).
func boxFromAlert(alert *entities.AlertEvent) vision.Box {
	if strings.TrimSpace(alert.BoundingBox) != "" {
		var b vision.Box
		if json.Unmarshal([]byte(alert.BoundingBox), &b) == nil && b.W > 0 && b.H > 0 {
			return b
		}
	}
	return vision.Box{X: 0, Y: 0, W: 1, H: 1}
}
