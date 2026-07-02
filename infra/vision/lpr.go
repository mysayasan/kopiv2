package vision

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

const (
	// defaultPlateLabel is the raw model label the LPR stage emits for a localized
	// plate (lowercase-with-spaces, matching the label-catalog convention). Rules
	// can override it via ruleConfig.plateLabel when a custom plate model uses a
	// different class name.
	defaultPlateLabel = "license plate"
	// defaultMinOCRConfidence gates which OCR reads are trustworthy enough to act
	// on. Reads below this are treated as "no plate" so a blurry/half-read plate
	// neither fires an any-plate rule nor (worse) false-matches a watchlist entry.
	defaultMinOCRConfidence = 0.5
	// plateMatchMaxEditDistance is the largest Levenshtein distance between a read
	// plate and a watchlist entry still treated as the same plate, absorbing common
	// single-character OCR confusions (O/0, I/1, B/8) without matching arbitrary
	// plates.
	plateMatchMaxEditDistance = 1
)

// LPR match modes select which OCR'd plates a rule fires on.
const (
	// lprMatchAny fires on every readable plate (function 1: log/snapshot all
	// vehicles with their plate). The default when no watchlist is configured.
	lprMatchAny = "any"
	// lprMatchInclude fires only when the read plate matches one in the watchlist
	// (function 2: alert on specific plates — VIP/family/fleet).
	lprMatchInclude = "include"
	// lprMatchExclude fires only when the read plate is NOT in the list (alert on
	// any plate that is not on a known/allowed list — e.g. an unknown car at a gate).
	lprMatchExclude = "exclude"
)

// lprConfig is the parsed ruleConfig for a license-plate rule.
type lprConfig struct {
	// Plates is the watchlist of plate strings (normalized on parse). Empty means
	// "every readable plate" regardless of MatchMode.
	Plates []string `json:"plates"`
	// MatchMode is one of any/include/exclude (see constants). Empty defaults to
	// "any" when Plates is empty, else "include".
	MatchMode string `json:"matchMode"`
	// MinOCRConfidence is the minimum OCR read confidence to act on (0 = default).
	MinOCRConfidence float64 `json:"minOcrConfidence"`
	// PlateLabel overrides which raw model label carries plate metadata (0 = the
	// stock "license plate" label).
	PlateLabel string `json:"plateLabel"`
}

// plateMatch carries the resolved attributes of a matched plate so the alert can
// render a meaningful label/metadata (plate text plus the enclosing vehicle's
// type and color when the worker associated them).
type plateMatch struct {
	Plate         string
	OCRConfidence float64
	VehicleType   string
	Color         string
	Watchlisted   bool
}

func isLPRType(value string) bool {
	return normalizedDetectionType(value) == DetectionLicensePlate
}

func parseLPRConfig(rule DetectionRule) (lprConfig, error) {
	var cfg lprConfig
	if strings.TrimSpace(rule.RuleConfig) != "" {
		if err := json.Unmarshal([]byte(rule.RuleConfig), &cfg); err != nil {
			return lprConfig{}, fmt.Errorf("ruleConfig must be valid LPR JSON: %w", err)
		}
	}
	cfg.Plates = normalizePlateList(cfg.Plates)
	cfg.MatchMode = strings.ToLower(strings.TrimSpace(cfg.MatchMode))
	switch cfg.MatchMode {
	case lprMatchAny, lprMatchInclude, lprMatchExclude:
		// explicit and valid
	case "":
		if len(cfg.Plates) > 0 {
			cfg.MatchMode = lprMatchInclude
		} else {
			cfg.MatchMode = lprMatchAny
		}
	}
	if cfg.MinOCRConfidence <= 0 {
		cfg.MinOCRConfidence = defaultMinOCRConfidence
	}
	cfg.PlateLabel = strings.ToLower(strings.TrimSpace(cfg.PlateLabel))
	if cfg.PlateLabel == "" {
		cfg.PlateLabel = defaultPlateLabel
	}
	return cfg, nil
}

func validateLPRRule(rule DetectionRule) error {
	if !isLPRType(rule.DetectionType) {
		return nil
	}
	cfg, err := parseLPRConfig(rule)
	if err != nil {
		return err
	}
	switch cfg.MatchMode {
	case lprMatchAny, lprMatchInclude, lprMatchExclude:
	default:
		return fmt.Errorf("ruleConfig.matchMode must be one of any/include/exclude")
	}
	if cfg.MatchMode != lprMatchAny && len(cfg.Plates) == 0 {
		return fmt.Errorf("ruleConfig.plates is required when matchMode is %q", cfg.MatchMode)
	}
	if cfg.MinOCRConfidence < 0 || cfg.MinOCRConfidence > 1 {
		return fmt.Errorf("ruleConfig.minOcrConfidence must be between 0 and 1")
	}
	return nil
}

// lprMatch selects the best plate candidate for a rule. A candidate qualifies when
// it carries the plate label, sits in the zone, has an OCR read above the
// confidence floor, and satisfies the rule's watchlist mode. The highest-OCR-
// confidence qualifying plate is returned as the representative.
func (d *ObjectRuleDetector) lprMatch(rule DetectionRule, cfg lprConfig, candidates []ObjectCandidate) (best ObjectCandidate, match *plateMatch, matched bool) {
	zones := parseZones(rule.ZonePolygon)
	for _, candidate := range candidates {
		candidate.Label = strings.ToLower(strings.TrimSpace(candidate.Label))
		if candidate.Label != cfg.PlateLabel {
			continue
		}
		plate, ocrConf, vehicleType, color := plateAttributes(candidate.Metadata)
		if plate == "" || ocrConf < cfg.MinOCRConfidence {
			continue
		}
		box := normalizeBox(candidate.Box)
		if !boxCenterInAnyZone(box, zones) {
			continue
		}
		onList := plateInList(plate, cfg.Plates)
		switch cfg.MatchMode {
		case lprMatchInclude:
			if !onList {
				continue
			}
		case lprMatchExclude:
			if onList {
				continue
			}
		}
		if matched && ocrConf <= best.Confidence {
			continue
		}
		candidate.Box = box
		// Surface the OCR confidence as the candidate confidence so downstream
		// threshold/streak machinery and the alert reflect read quality, not the
		// plate detector's localization score.
		candidate.Confidence = ocrConf
		best = candidate
		match = &plateMatch{
			Plate:         plate,
			OCRConfidence: ocrConf,
			VehicleType:   vehicleType,
			Color:         color,
			Watchlisted:   onList,
		}
		matched = true
	}
	return best, match, matched
}

// plateAttributes pulls the plate string and associated vehicle attributes out of
// a candidate's free-form metadata (emitted by the worker LPR stage). Values are
// tolerant of missing/typed JSON.
func plateAttributes(meta map[string]any) (plate string, ocrConfidence float64, vehicleType string, color string) {
	if meta == nil {
		return "", 0, "", ""
	}
	plate = normalizePlate(metaString(meta, "plate"))
	vehicleType = strings.ToLower(strings.TrimSpace(metaString(meta, "vehicleType")))
	color = strings.ToLower(strings.TrimSpace(metaString(meta, "color")))
	ocrConfidence = metaFloat(meta, "ocrConfidence")
	return plate, ocrConfidence, vehicleType, color
}

func metaString(meta map[string]any, key string) string {
	if v, ok := meta[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func metaFloat(meta map[string]any, key string) float64 {
	switch v := meta[key].(type) {
	case float64:
		return v
	case json.Number:
		f, _ := v.Float64()
		return f
	default:
		return 0
	}
}

// lprLabel renders the alert label for a plate detection, e.g.
// "Plate WXY1234 (white car)" or "Plate WXY1234".
func lprLabel(match *plateMatch) string {
	if match == nil || match.Plate == "" {
		return "License plate detected"
	}
	descriptor := strings.TrimSpace(match.Color + " " + match.VehicleType)
	if descriptor != "" {
		return fmt.Sprintf("Plate %s (%s)", match.Plate, descriptor)
	}
	return "Plate " + match.Plate
}

// normalizePlate uppercases a plate and strips separators/whitespace so reads and
// watchlist entries compare on alphanumerics only ("WXY 1234" == "wxy-1234").
func normalizePlate(value string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	var b strings.Builder
	for _, r := range value {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func normalizePlateList(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if p := normalizePlate(value); p != "" {
			result = append(result, p)
		}
	}
	return result
}

// plateInList reports whether plate matches any list entry exactly or within the
// allowed edit distance, absorbing single-character OCR errors.
func plateInList(plate string, list []string) bool {
	for _, entry := range list {
		if plate == entry {
			return true
		}
		if levenshtein(plate, entry) <= plateMatchMaxEditDistance {
			return true
		}
	}
	return false
}

// levenshtein is the standard edit distance, used to fuzzily match a read plate
// against a watchlist entry.
func levenshtein(a, b string) int {
	la, lb := len(a), len(b)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}
	prev := make([]int, lb+1)
	curr := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min3(prev[j]+1, curr[j-1]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[lb]
}

func min3(a, b, c int) int {
	return int(math.Min(math.Min(float64(a), float64(b)), float64(c)))
}
