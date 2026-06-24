package vision

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strings"
	"sync"
	"time"
)

const (
	DetectorModeMotion     = "motion"
	DetectorModeExternal   = "external"
	DetectorModeHybrid     = "hybrid"
	DetectorModePersistent = "persistent"
)

type Box struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	W float64 `json:"w"`
	H float64 `json:"h"`
}

// MetaBox is one detection's box plus its label and confidence, recorded in an
// alert's metadata "boxes" list so a snapshot can outline and label each detection
// (e.g. every crowd member) with its own confidence.
type MetaBox struct {
	Box
	Label      string  `json:"label,omitempty"`
	Confidence float64 `json:"confidence,omitempty"`
}

type ObjectCandidate struct {
	Label      string         `json:"label"`
	Confidence float64        `json:"confidence"`
	Box        Box            `json:"box"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

type ObjectDetector interface {
	DetectObjects(ctx context.Context, frame Frame) ([]ObjectCandidate, error)
}

type ObjectRuleDetectorOptions struct {
	ClassMap            map[string][]string
	MinObjectConfidence float64
	Source              string
}

type objectRuleState struct {
	hitsByRule    map[int64]int
	lastTriggered map[int64]int64
	lineRules     map[int64]*lineCrossingRuleState
}

// ObjectRuleDetector maps object detector candidates to configured detection rules.
type ObjectRuleDetector struct {
	mu                  sync.Mutex
	backend             ObjectDetector
	classMap            map[string]map[string]bool
	minObjectConfidence float64
	source              string
	byCamera            map[int64]*objectRuleState
	now                 func() time.Time
}

func NewObjectRuleDetector(backend ObjectDetector, opts ObjectRuleDetectorOptions) *ObjectRuleDetector {
	return &ObjectRuleDetector{
		backend:             backend,
		classMap:            normalizeClassMap(opts.ClassMap),
		minObjectConfidence: opts.MinObjectConfidence,
		source:              nonEmpty(opts.Source, "object-detector"),
		byCamera:            map[int64]*objectRuleState{},
		now:                 time.Now,
	}
}

func (d *ObjectRuleDetector) Detect(ctx context.Context, frame Frame, rules []DetectionRule) ([]Detection, error) {
	if d.backend == nil {
		return nil, fmt.Errorf("object detector backend is not configured")
	}
	candidates, err := d.backend.DetectObjects(ctx, frame)
	if err != nil {
		return nil, err
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	state := d.byCamera[frame.CameraId]
	if state == nil {
		state = &objectRuleState{
			hitsByRule:    map[int64]int{},
			lastTriggered: map[int64]int64{},
			lineRules:     map[int64]*lineCrossingRuleState{},
		}
		d.byCamera[frame.CameraId] = state
	}

	now := d.now().UTC().Unix()
	detections := make([]Detection, 0)
	for _, rule := range rules {
		if !rule.IsEnabled {
			continue
		}
		if isLineCrossingType(rule.DetectionType) {
			lineDetections, err := d.detectLineCrossing(rule, candidates, state, now)
			if err != nil {
				return nil, err
			}
			detections = append(detections, lineDetections...)
			continue
		}
		var candidate ObjectCandidate
		var matched bool
		crowdCount := 0
		var crowdBoxes []MetaBox
		var plate *plateMatch
		switch {
		case isCrowdType(rule.DetectionType):
			cfg, err := parseCrowdConfig(rule)
			if err != nil {
				return nil, err
			}
			candidate, crowdCount, matched, crowdBoxes = d.crowdMatch(rule, cfg, candidates)
		case isLPRType(rule.DetectionType):
			cfg, err := parseLPRConfig(rule)
			if err != nil {
				return nil, err
			}
			candidate, plate, matched = d.lprMatch(rule, cfg, candidates)
		default:
			candidate, matched = d.bestCandidate(rule, candidates)
		}

		minFrames := rule.MinFrames
		if minFrames <= 0 {
			minFrames = DefaultDetectionMinFrames
		}
		cooldown := rule.CooldownSeconds
		if cooldown <= 0 {
			cooldown = DefaultDetectionCooldown
		}

		if matched {
			// Cap the streak at minFrames so it triggers promptly and clears
			// within minFrames misses once the object leaves.
			if state.hitsByRule[rule.Id] < minFrames {
				state.hitsByRule[rule.Id]++
			}
		} else {
			// Decay instead of hard-resetting to 0. A single dropped frame
			// (YOLO confidence flicker, brief occlusion, person at the frame
			// edge) should not wipe accumulated evidence — at realistic sample
			// rates that means the alert never fires even while the object is
			// continuously present.
			if state.hitsByRule[rule.Id] > 0 {
				state.hitsByRule[rule.Id]--
			}
			continue
		}

		if state.hitsByRule[rule.Id] < minFrames {
			continue
		}
		if last := state.lastTriggered[rule.Id]; last > 0 && now-last < int64(cooldown) {
			continue
		}
		state.lastTriggered[rule.Id] = now

		boundingBox, _ := json.Marshal(candidate.Box)
		meta := map[string]any{
			"source":      d.source,
			"objectLabel": candidate.Label,
			"objectMeta":  candidate.Metadata,
		}
		label := detectionLabel(rule.DetectionType, candidate.Label)
		if crowdCount > 0 {
			meta["crowdCount"] = crowdCount
			// Carry every qualifying box so the alert snapshot outlines each crowd
			// member, not only the representative one.
			if len(crowdBoxes) > 0 {
				meta["boxes"] = crowdBoxes
			}
			label = crowdLabel(candidate.Label, crowdCount)
		}
		if plate != nil {
			// Promote the plate string and associated vehicle attributes to top-level
			// metadata so notifications can template {{plate}}/{{vehicleType}}/{{color}}
			// and the plate-history view can filter on them without reaching into
			// objectMeta.
			meta["plate"] = plate.Plate
			meta["ocrConfidence"] = plate.OCRConfidence
			meta["watchlisted"] = plate.Watchlisted
			if plate.VehicleType != "" {
				meta["vehicleType"] = plate.VehicleType
			}
			if plate.Color != "" {
				meta["color"] = plate.Color
			}
			label = lprLabel(plate)
		}
		metadata, _ := json.Marshal(meta)
		detections = append(detections, Detection{
			RuleId:        rule.Id,
			CameraId:      rule.CameraId,
			DetectionType: rule.DetectionType,
			Label:         label,
			Confidence:    candidate.Confidence,
			ZonePolygon:   rule.ZonePolygon,
			BoundingBox:   string(boundingBox),
			Metadata:      string(metadata),
		})
	}
	for i := range detections {
		detections[i].FrameCapturedAt = frame.CapturedAt
	}
	return detections, nil
}

func (d *ObjectRuleDetector) Close() error {
	if closer, ok := d.backend.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

func (d *ObjectRuleDetector) bestCandidate(rule DetectionRule, candidates []ObjectCandidate) (ObjectCandidate, bool) {
	zone := parseZone(rule.ZonePolygon)
	minConfidence := rule.Threshold
	if minConfidence <= 0 {
		minConfidence = DefaultDetectionThreshold
	}
	if d.minObjectConfidence > 0 {
		minConfidence = math.Max(minConfidence, d.minObjectConfidence)
	}

	var best ObjectCandidate
	matched := false
	for _, candidate := range candidates {
		candidate.Label = strings.ToLower(strings.TrimSpace(candidate.Label))
		if candidate.Label == "" || candidate.Confidence < minConfidence {
			continue
		}
		if !d.ruleLabelAllowed(rule, candidate.Label) {
			continue
		}
		box := normalizeBox(candidate.Box)
		if !boxCenterInZone(box, zone) {
			continue
		}
		candidate.Box = box
		if !matched || candidate.Confidence > best.Confidence {
			best = candidate
			matched = true
		}
	}
	return best, matched
}

// ruleLabelAllowed decides whether a candidate label satisfies a rule's target
// classes. When the rule carries an explicit class list in ruleConfig (the
// two-axis "Target" — already resolved to raw model labels by the caller), that
// list wins and "*" means "any object". Legacy rules with no ruleConfig.classes
// fall back to the static classMap keyed by detectionType, so existing
// person/vehicle/animal/fire/smoke rules keep working unchanged.
func (d *ObjectRuleDetector) ruleLabelAllowed(rule DetectionRule, label string) bool {
	classes := parseRuleClasses(rule.RuleConfig)
	if len(classes) > 0 {
		for _, class := range classes {
			if class == "*" || class == label {
				return true
			}
		}
		return false
	}
	return d.labelAllowed(rule.DetectionType, label)
}

// parseRuleClasses extracts a generic {"classes":[...]} target list from a rule
// config. It returns nil when the config is empty or has no classes, so the
// caller falls back to the static classMap.
func parseRuleClasses(ruleConfig string) []string {
	if strings.TrimSpace(ruleConfig) == "" {
		return nil
	}
	var cfg struct {
		Classes []string `json:"classes"`
	}
	if err := json.Unmarshal([]byte(ruleConfig), &cfg); err != nil {
		return nil
	}
	return normalizeStringList(cfg.Classes)
}

func (d *ObjectRuleDetector) labelAllowed(detectionType string, label string) bool {
	detectionType = strings.ToLower(strings.TrimSpace(detectionType))
	label = strings.ToLower(strings.TrimSpace(label))
	allowed := d.classMap[detectionType]
	if len(allowed) == 0 {
		return label == detectionType
	}
	return allowed[label]
}

func normalizeClassMap(raw map[string][]string) map[string]map[string]bool {
	vehicleClasses := []string{"vehicle", "car", "truck", "bus", "motorcycle", "bicycle"}
	animalClasses := []string{
		"animal", "bird", "cat", "dog", "horse", "sheep", "cow", "elephant", "bear", "zebra", "giraffe",
		"mouse", "rat", "rabbit", "deer", "goat", "pig", "monkey",
	}

	defaults := map[string][]string{
		DetectionFire:              {DetectionFire},
		DetectionSmoke:             {DetectionSmoke},
		DetectionPerson:            {DetectionPerson},
		DetectionVehicle:           vehicleClasses,
		DetectionAnimal:            animalClasses,
		DetectionCrowd:             {DetectionPerson},
		DetectionLicensePlate:      {defaultPlateLabel},
		DetectionIntrusion:         append([]string{"person"}, vehicleClasses...),
		DetectionLineCrossing:      append([]string{"person"}, vehicleClasses...),
		DetectionMultiLineCrossing: append([]string{"person"}, vehicleClasses...),
	}
	for key, values := range raw {
		defaults[strings.ToLower(strings.TrimSpace(key))] = values
	}
	result := map[string]map[string]bool{}
	for key, values := range defaults {
		key = strings.ToLower(strings.TrimSpace(key))
		if key == "" {
			continue
		}
		result[key] = map[string]bool{}
		for _, value := range values {
			value = strings.ToLower(strings.TrimSpace(value))
			if value != "" {
				result[key][value] = true
			}
		}
	}
	return result
}

func normalizeBox(box Box) Box {
	return Box{
		X: clamp(box.X),
		Y: clamp(box.Y),
		W: clamp(box.W),
		H: clamp(box.H),
	}
}

func boxCenterInZone(box Box, zone [][2]float64) bool {
	centerX := clamp(box.X + box.W/2)
	centerY := clamp(box.Y + box.H/2)
	return pointInPolygon(centerX, centerY, zone)
}

func detectionLabel(detectionType string, objectLabel string) string {
	detectionType = strings.ToLower(strings.TrimSpace(detectionType))
	objectLabel = strings.ToLower(strings.TrimSpace(objectLabel))
	if detectionType == "" {
		return objectLabel
	}
	if objectLabel == "" || objectLabel == detectionType {
		return title(detectionType) + " detected"
	}
	return title(detectionType) + " detected (" + objectLabel + ")"
}

func title(value string) string {
	value = strings.ReplaceAll(strings.TrimSpace(value), "_", " ")
	if value == "" {
		return value
	}
	return strings.ToUpper(value[:1]) + value[1:]
}

func nonEmpty(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}
