package vision

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Face recognition is the two-stage detect-then-recognize sibling of LPR: the WORKER localizes each
// face, embeds it, and matches it against the global enrolled gallery, emitting a candidate labelled
// "face" whose metadata carries the recognized personId/personName (empty = unknown) and a match
// confidence. This file is the Go half — it applies the RULE'S policy: which faces are worth an
// alert. (Compare lpr.go, where the OCR is in the worker and the watchlist match is here.)

const (
	// defaultFaceLabel is the raw label the worker's face stage emits for a detected face.
	defaultFaceLabel = "face"
	// defaultMinFaceConfidence gates which matches are trustworthy enough to name someone. Below it
	// a face is treated as unknown rather than risk naming the WRONG person — the dangerous failure.
	defaultMinFaceConfidence = 0.6
)

// Face rule match modes select which faces a rule fires on.
const (
	// faceMatchKnown fires on ANY recognized (enrolled) person — "tell me when someone we know is here".
	faceMatchKnown = "known"
	// faceMatchInclude fires only on a chosen set of people (People) — a VIP/watchlist alert.
	faceMatchInclude = "include"
	// faceMatchUnknown fires only on UNrecognized faces — stranger detection. It deliberately does not
	// name anyone; it reports "an unknown face appeared", which is the honest thing to say.
	faceMatchUnknown = "unknown"
)

// faceConfig is the parsed ruleConfig for a face-recognition rule.
type faceConfig struct {
	// People is the set of person NAMES this rule targets (normalized). Used with matchMode=include.
	People []string `json:"people"`
	// MatchMode is one of known/include/unknown (see constants). Empty defaults to "known".
	MatchMode string `json:"matchMode"`
	// MinConfidence is the minimum match confidence to treat a face as recognized (0 = default).
	MinConfidence float64 `json:"minConfidence"`
}

// faceMatchInfo carries the resolved identity of a matched face for the alert label/metadata.
type faceMatchInfo struct {
	PersonId   int64
	PersonName string
	Confidence float64
	Recognized bool // true = matched an enrolled person; false = an unknown face
}

func isFaceType(value string) bool {
	return normalizedDetectionType(value) == DetectionFace
}

func parseFaceConfig(rule DetectionRule) (faceConfig, error) {
	var cfg faceConfig
	if strings.TrimSpace(rule.RuleConfig) != "" {
		if err := json.Unmarshal([]byte(rule.RuleConfig), &cfg); err != nil {
			return faceConfig{}, fmt.Errorf("ruleConfig must be valid face JSON: %w", err)
		}
	}
	cfg.People = normalizeNameList(cfg.People)
	cfg.MatchMode = strings.ToLower(strings.TrimSpace(cfg.MatchMode))
	switch cfg.MatchMode {
	case faceMatchKnown, faceMatchInclude, faceMatchUnknown:
	case "":
		if len(cfg.People) > 0 {
			cfg.MatchMode = faceMatchInclude
		} else {
			cfg.MatchMode = faceMatchKnown
		}
	}
	if cfg.MinConfidence <= 0 {
		cfg.MinConfidence = defaultMinFaceConfidence
	}
	return cfg, nil
}

func validateFaceRule(rule DetectionRule) error {
	if !isFaceType(rule.DetectionType) {
		return nil
	}
	cfg, err := parseFaceConfig(rule)
	if err != nil {
		return err
	}
	switch cfg.MatchMode {
	case faceMatchKnown, faceMatchInclude, faceMatchUnknown:
	default:
		return fmt.Errorf("ruleConfig.matchMode must be one of known/include/unknown")
	}
	if cfg.MatchMode == faceMatchInclude && len(cfg.People) == 0 {
		return fmt.Errorf("ruleConfig.people is required when matchMode is %q", faceMatchInclude)
	}
	if cfg.MinConfidence < 0 || cfg.MinConfidence > 1 {
		return fmt.Errorf("ruleConfig.minConfidence must be between 0 and 1")
	}
	return nil
}

// faceMatch selects the best face candidate for a rule. A candidate qualifies when it carries the
// face label, sits in the zone, and satisfies the rule's mode: a recognized person (known/include)
// above the confidence floor, or an unknown face (unknown). The highest-confidence qualifying face
// is returned.
func (d *ObjectRuleDetector) faceMatch(rule DetectionRule, cfg faceConfig, candidates []ObjectCandidate) (best ObjectCandidate, match *faceMatchInfo, matched bool) {
	zones := parseZones(rule.ZonePolygon)
	for _, candidate := range candidates {
		if strings.ToLower(strings.TrimSpace(candidate.Label)) != defaultFaceLabel {
			continue
		}
		personId, personName, confidence := faceAttributes(candidate.Metadata)
		recognized := personName != "" && confidence >= cfg.MinConfidence

		switch cfg.MatchMode {
		case faceMatchKnown:
			if !recognized {
				continue
			}
		case faceMatchInclude:
			if !recognized || !nameInList(personName, cfg.People) {
				continue
			}
		case faceMatchUnknown:
			if recognized {
				continue
			}
		}

		box := normalizeBox(candidate.Box)
		if !boxCenterInAnyZone(box, zones) {
			continue
		}
		// Rank: recognized faces by match confidence; unknown faces by detection confidence.
		if matched && confidence <= best.Confidence {
			continue
		}
		candidate.Box = box
		candidate.Confidence = maxFloat(confidence, 0.5)
		best = candidate
		match = &faceMatchInfo{PersonId: personId, PersonName: personName, Confidence: confidence, Recognized: recognized}
		matched = true
	}
	return best, match, matched
}

// faceAttributes pulls the recognized identity out of a candidate's worker-emitted metadata.
func faceAttributes(meta map[string]any) (personId int64, personName string, confidence float64) {
	if meta == nil {
		return 0, "", 0
	}
	personName = strings.TrimSpace(metaString(meta, "personName"))
	personId = int64(metaFloat(meta, "personId"))
	confidence = metaFloat(meta, "confidence")
	return personId, personName, confidence
}

// faceLabel renders the alert label: "Alice (94%)" for a recognized person, "Unknown face" otherwise.
func faceLabel(match *faceMatchInfo) string {
	if match == nil {
		return "Face detected"
	}
	if match.Recognized && match.PersonName != "" {
		return fmt.Sprintf("%s (%d%%)", match.PersonName, int(match.Confidence*100+0.5))
	}
	return "Unknown face"
}

// normalizeName lowercases and single-spaces a person name so rule People entries and worker-emitted
// names compare consistently.
func normalizeName(v string) string {
	return strings.ToLower(strings.Join(strings.Fields(v), " "))
}

func normalizeNameList(values []string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		if n := normalizeName(v); n != "" {
			out = append(out, n)
		}
	}
	return out
}

func nameInList(name string, list []string) bool {
	n := normalizeName(name)
	for _, e := range list {
		if n == e {
			return true
		}
	}
	return false
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
