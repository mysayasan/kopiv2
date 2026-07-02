package vision

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

const (
	// defaultCrowdMinCount is the number of qualifying objects that must be
	// present in a single frame for a crowd rule to count as a hit. Two means
	// "more than one person".
	defaultCrowdMinCount = 2
	maxCrowdMinCount     = 100
)

// crowdConfig is the parsed ruleConfig for a crowd detection rule.
type crowdConfig struct {
	// MinCount is the inclusive threshold: count >= MinCount is a hit.
	MinCount int `json:"minCount"`
	// Classes restricts which object labels are counted. Empty means "use the
	// rule's detection-type class map" (crowd defaults to person). "*" counts
	// any object.
	Classes []string `json:"classes"`
}

func isCrowdType(value string) bool {
	return normalizedDetectionType(value) == DetectionCrowd
}

func parseCrowdConfig(rule DetectionRule) (crowdConfig, error) {
	var cfg crowdConfig
	if strings.TrimSpace(rule.RuleConfig) != "" {
		if err := json.Unmarshal([]byte(rule.RuleConfig), &cfg); err != nil {
			return crowdConfig{}, fmt.Errorf("ruleConfig must be valid crowd JSON: %w", err)
		}
	}
	cfg.Classes = normalizeStringList(cfg.Classes)
	if cfg.MinCount <= 0 {
		cfg.MinCount = defaultCrowdMinCount
	}
	return cfg, nil
}

func validateCrowdRule(rule DetectionRule) error {
	if !isCrowdType(rule.DetectionType) {
		return nil
	}
	cfg, err := parseCrowdConfig(rule)
	if err != nil {
		return err
	}
	if cfg.MinCount < 2 {
		return fmt.Errorf("ruleConfig.minCount must be at least 2 for %s", DetectionCrowd)
	}
	if cfg.MinCount > maxCrowdMinCount {
		return fmt.Errorf("ruleConfig.minCount supports at most %d", maxCrowdMinCount)
	}
	return nil
}

// crowdMatch counts the qualifying candidates for a crowd rule (in-zone, above
// confidence, allowed class) and reports whether the count meets the configured
// minimum. The highest-confidence candidate is returned as the representative
// object for the alert, and boxes holds every qualifying box so the snapshot can
// outline each crowd member, not just the representative.
func (d *ObjectRuleDetector) crowdMatch(rule DetectionRule, cfg crowdConfig, candidates []ObjectCandidate) (best ObjectCandidate, count int, matched bool, boxes []MetaBox) {
	zones := parseZones(rule.ZonePolygon)
	minConfidence := rule.Threshold
	if minConfidence <= 0 {
		minConfidence = DefaultDetectionThreshold
	}
	if d.minObjectConfidence > 0 {
		minConfidence = math.Max(minConfidence, d.minObjectConfidence)
	}

	for _, candidate := range candidates {
		candidate.Label = strings.ToLower(strings.TrimSpace(candidate.Label))
		if candidate.Label == "" || candidate.Confidence < minConfidence {
			continue
		}
		if !d.crowdLabelAllowed(rule, cfg, candidate.Label) {
			continue
		}
		box := normalizeBox(candidate.Box)
		if !boxCenterInAnyZone(box, zones) {
			continue
		}
		candidate.Box = box
		count++
		boxes = append(boxes, MetaBox{Box: box, Label: candidate.Label, Confidence: candidate.Confidence})
		if count == 1 || candidate.Confidence > best.Confidence {
			best = candidate
		}
	}
	return best, count, count >= cfg.MinCount, boxes
}

// crowdLabel renders the alert label for a crowd detection, e.g.
// "Crowd detected (3 persons)".
func crowdLabel(objectLabel string, count int) string {
	objectLabel = strings.ToLower(strings.TrimSpace(objectLabel))
	if objectLabel == "" {
		objectLabel = DetectionPerson
	}
	noun := objectLabel
	if count != 1 {
		noun += "s"
	}
	return fmt.Sprintf("Crowd detected (%d %s)", count, noun)
}

func (d *ObjectRuleDetector) crowdLabelAllowed(rule DetectionRule, cfg crowdConfig, label string) bool {
	if len(cfg.Classes) > 0 {
		for _, class := range cfg.Classes {
			if class == "*" || label == class {
				return true
			}
		}
		return false
	}
	return d.labelAllowed(rule.DetectionType, label)
}
