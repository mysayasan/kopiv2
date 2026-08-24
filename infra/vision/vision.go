package vision

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const (
	DefaultDetectionThreshold = 0.75
	DefaultDetectionMinFrames = 3
	DefaultDetectionCooldown  = 30
)

const (
	DetectionFire              = "fire"
	DetectionSmoke             = "smoke"
	DetectionPerson            = "person"
	DetectionVehicle           = "vehicle"
	DetectionAnimal            = "animal"
	DetectionPresence          = "presence"
	DetectionCrowd             = "crowd"
	DetectionIntrusion         = "intrusion"
	DetectionLineCrossing      = "line_crossing"
	DetectionMultiLineCrossing = "multi_line_crossing"
	// The time-based rules (W3-4) live in dwell.go: DetectionLoitering,
	// DetectionLeftBehind and DetectionDirection.
	// DetectionLicensePlate is automatic license-plate recognition (ALPR/LPR). The
	// worker localizes a plate, OCRs the crop, and (optionally) associates the
	// enclosing vehicle's type and color; the plate string and attributes ride the
	// candidate metadata. Rules can fire on any readable plate or only on a
	// watchlist — see lpr.go.
	DetectionLicensePlate = "lpr"
	// DetectionFace is face recognition. The worker detects faces, embeds each, and
	// matches it against the GLOBAL enrolled gallery; the recognized person (or
	// "unknown") plus a match confidence ride the candidate metadata. Rules can fire
	// on any recognized person, only a chosen set, or only unknown faces — see face.go.
	DetectionFace = "face"
)

// DetectionRuleRequest is the reusable request shape for configuring a visual detector.
type DetectionRuleRequest struct {
	Id              int64   `json:"id"`
	CameraId        int64   `json:"cameraId"`
	Name            string  `json:"name"`
	DetectionType   string  `json:"detectionType"`
	ZonePolygon     string  `json:"zonePolygon"`
	RuleConfig      string  `json:"ruleConfig"`
	SchedulePolicy  string  `json:"schedulePolicy"`
	Threshold       float64 `json:"threshold"`
	MinFrames       int     `json:"minFrames"`
	CooldownSeconds int     `json:"cooldownSeconds"`
	SoundEnabled    bool    `json:"soundEnabled"`
	ArchiveClip     bool    `json:"archiveClip"`
	IsEnabled       bool    `json:"isEnabled"`
}

// DetectionRule is the reusable normalized detector rule.
type DetectionRule struct {
	Id              int64   `json:"id"`
	CameraId        int64   `json:"cameraId"`
	Name            string  `json:"name"`
	DetectionType   string  `json:"detectionType"`
	ZonePolygon     string  `json:"zonePolygon"`
	RuleConfig      string  `json:"ruleConfig"`
	SchedulePolicy  string  `json:"schedulePolicy"`
	Threshold       float64 `json:"threshold"`
	MinFrames       int     `json:"minFrames"`
	CooldownSeconds int     `json:"cooldownSeconds"`
	SoundEnabled    bool    `json:"soundEnabled"`
	// ArchiveClip asks the fleet to keep a copy of this rule's event clip OFF the
	// appliance. See entities.DetectionRule.ArchiveClip for why it is per rule.
	ArchiveClip     bool  `json:"archiveClip"`
	IsEnabled       bool  `json:"isEnabled"`
	LastTriggeredAt int64 `json:"lastTriggeredAt"`
}

// AlertEventRequest is the reusable request shape for detector-generated events.
type AlertEventRequest struct {
	RuleId        int64   `json:"ruleId"`
	CameraId      int64   `json:"cameraId"`
	DetectionType string  `json:"detectionType"`
	Label         string  `json:"label"`
	Confidence    float64 `json:"confidence"`
	ZonePolygon   string  `json:"zonePolygon"`
	BoundingBox   string  `json:"boundingBox"`
	SnapshotPath  string  `json:"snapshotPath"`
	Metadata      string  `json:"metadata"`
}

// AlertEvent is the reusable normalized detector event.
type AlertEvent struct {
	Id             int64   `json:"id"`
	RuleId         int64   `json:"ruleId"`
	CameraId       int64   `json:"cameraId"`
	DetectionType  string  `json:"detectionType"`
	Label          string  `json:"label"`
	Confidence     float64 `json:"confidence"`
	ZonePolygon    string  `json:"zonePolygon"`
	BoundingBox    string  `json:"boundingBox"`
	SnapshotPath   string  `json:"snapshotPath"`
	Metadata       string  `json:"metadata"`
	IsAcknowledged bool    `json:"isAcknowledged"`
}

// InferenceParams holds per-frame YOLO inference overrides forwarded to the external worker.
// Zero values mean "use the worker's own default" (env vars or compiled-in).
type InferenceParams struct {
	Conf    float64 `json:"conf,omitempty"`
	Iou     float64 `json:"iou,omitempty"`
	Augment bool    `json:"augment,omitempty"`
	Imgsz   int     `json:"imgsz,omitempty"`
	Half    bool    `json:"half,omitempty"`
	MaxDet  int     `json:"maxDet,omitempty"`
}

// Frame is the app-neutral video/image payload handed to detector implementations.
type Frame struct {
	CameraId   int64             `json:"cameraId"`
	Data       []byte            `json:"-"`
	Format     string            `json:"format"`
	Width      int               `json:"width"`
	Height     int               `json:"height"`
	CapturedAt int64             `json:"capturedAt"`
	Metadata   map[string]string `json:"metadata"`
	Inference  InferenceParams   `json:"inference"`
	// WantLPR requests the worker's license-plate stage (plate localization + OCR +
	// vehicle attributes) for this frame. Set only for cameras that have an active
	// LPR rule so the expensive OCR path never runs on object-detection-only
	// cameras — this is the per-camera compute gate.
	WantLPR bool `json:"-"`
	// WantFace requests the worker's face stage (detect faces + embed + match the enrolled gallery)
	// for this frame. Set only for cameras with an active face rule — the per-camera compute gate,
	// exactly like WantLPR, so the embedding path never runs on cameras that don't need it.
	WantFace bool `json:"-"`
	// WantAppearance requests an appearance vector on each person/vehicle detection, so those
	// sightings can later be ranked by "find more like this". The same per-camera compute gate
	// as the two above: it is a forward pass per crop, and a camera nobody searches by
	// appearance should not pay for one.
	WantAppearance bool `json:"-"`
}

// Detection is one detector result before it is persisted as an alert event.
type Detection struct {
	RuleId        int64   `json:"ruleId"`
	CameraId      int64   `json:"cameraId"`
	DetectionType string  `json:"detectionType"`
	Label         string  `json:"label"`
	Confidence    float64 `json:"confidence"`
	ZonePolygon   string  `json:"zonePolygon"`
	BoundingBox   string  `json:"boundingBox"`
	Metadata      string  `json:"metadata"`
	// FrameCapturedAt is the Unix timestamp (seconds) of the frame that produced
	// this detection. It is set by each Detect implementation and must be used as
	// the recording trigger anchor so that YOLO latency does not shift the clip window.
	FrameCapturedAt int64 `json:"frameCapturedAt"`
}

// Detector is implemented by reusable AI backends such as fire, person, or intrusion detectors.
type Detector interface {
	Detect(ctx context.Context, frame Frame, rules []DetectionRule) ([]Detection, error)
}

// AlertSink is implemented by apps that persist or forward detector events.
type AlertSink interface {
	CreateAlert(ctx context.Context, event AlertEventRequest) error
}

func NormalizeDetectionRule(req DetectionRuleRequest) DetectionRule {
	threshold := req.Threshold
	if threshold <= 0 {
		threshold = DefaultDetectionThreshold
	}
	minFrames := req.MinFrames
	if minFrames <= 0 {
		minFrames = DefaultDetectionMinFrames
	}
	cooldown := req.CooldownSeconds
	if cooldown <= 0 {
		cooldown = DefaultDetectionCooldown
	}
	name := strings.TrimSpace(req.Name)
	detectionType := strings.ToLower(strings.TrimSpace(req.DetectionType))
	if name == "" && detectionType != "" {
		name = strings.ToUpper(detectionType[:1]) + detectionType[1:] + " detection"
	}
	return DetectionRule{
		Id:              req.Id,
		CameraId:        req.CameraId,
		Name:            name,
		DetectionType:   detectionType,
		ZonePolygon:     strings.TrimSpace(req.ZonePolygon),
		RuleConfig:      strings.TrimSpace(req.RuleConfig),
		SchedulePolicy:  strings.TrimSpace(req.SchedulePolicy),
		Threshold:       threshold,
		MinFrames:       minFrames,
		CooldownSeconds: cooldown,
		SoundEnabled:    req.SoundEnabled,
		ArchiveClip:     req.ArchiveClip,
		IsEnabled:       req.IsEnabled,
	}
}

func ValidateDetectionRule(rule DetectionRule) error {
	if rule.CameraId <= 0 {
		return errors.New("cameraId is required")
	}
	if strings.TrimSpace(rule.DetectionType) == "" {
		return errors.New("detectionType is required")
	}
	if rule.Threshold <= 0 || rule.Threshold > 1 {
		return errors.New("threshold must be greater than 0 and at most 1")
	}
	if rule.MinFrames <= 0 {
		return errors.New("minFrames must be greater than 0")
	}
	if rule.CooldownSeconds < 0 {
		return errors.New("cooldownSeconds cannot be negative")
	}
	if err := validateJSONField("zonePolygon", rule.ZonePolygon); err != nil {
		return err
	}
	if err := validateJSONField("ruleConfig", rule.RuleConfig); err != nil {
		return err
	}
	if err := validateLineCrossingRule(rule); err != nil {
		return err
	}
	if err := validateCrowdRule(rule); err != nil {
		return err
	}
	if err := validateLPRRule(rule); err != nil {
		return err
	}
	if err := validateFaceRule(rule); err != nil {
		return err
	}
	if err := validateDwellRule(rule); err != nil {
		return err
	}
	return ValidateSchedulePolicy(rule.SchedulePolicy)
}

func NormalizeAlertEvent(req AlertEventRequest) AlertEvent {
	label := strings.TrimSpace(req.Label)
	detectionType := strings.ToLower(strings.TrimSpace(req.DetectionType))
	if label == "" {
		label = detectionType
	}
	return AlertEvent{
		RuleId:        req.RuleId,
		CameraId:      req.CameraId,
		DetectionType: detectionType,
		Label:         label,
		Confidence:    req.Confidence,
		ZonePolygon:   strings.TrimSpace(req.ZonePolygon),
		BoundingBox:   strings.TrimSpace(req.BoundingBox),
		SnapshotPath:  strings.TrimSpace(req.SnapshotPath),
		Metadata:      strings.TrimSpace(req.Metadata),
	}
}

func ValidateAlertEvent(alert AlertEvent) error {
	if alert.RuleId <= 0 {
		return errors.New("ruleId is required")
	}
	if alert.CameraId <= 0 {
		return errors.New("cameraId is required")
	}
	if strings.TrimSpace(alert.DetectionType) == "" {
		return errors.New("detectionType is required")
	}
	if alert.Confidence < 0 || alert.Confidence > 1 {
		return errors.New("confidence must be between 0 and 1")
	}
	if err := validateJSONField("zonePolygon", alert.ZonePolygon); err != nil {
		return err
	}
	if err := validateJSONField("boundingBox", alert.BoundingBox); err != nil {
		return err
	}
	return validateJSONField("metadata", alert.Metadata)
}

func validateJSONField(name string, value string) error {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	var raw any
	if err := json.Unmarshal([]byte(value), &raw); err != nil {
		return fmt.Errorf("%s must be valid JSON: %w", name, err)
	}
	return nil
}
