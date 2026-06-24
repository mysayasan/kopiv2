package vision

import (
	"context"
	"strings"
	"testing"
	"time"
)

// plateCandidate builds a localized-plate candidate as the worker LPR stage emits
// it: the plate label plus plate/ocr/vehicle attributes in free-form metadata.
func plateCandidate(plate string, ocr float64, vehicleType, color string) ObjectCandidate {
	return ObjectCandidate{
		Label:      defaultPlateLabel,
		Confidence: 0.9, // plate-detector localization score (distinct from OCR read)
		Box:        Box{X: 0.4, Y: 0.4, W: 0.1, H: 0.05},
		Metadata: map[string]any{
			"plate":         plate,
			"ocrConfidence": ocr,
			"vehicleType":   vehicleType,
			"color":         color,
		},
	}
}

func lprRule(ruleConfig string) DetectionRule {
	return DetectionRule{
		Id:              21,
		CameraId:        3,
		DetectionType:   DetectionLicensePlate,
		ZonePolygon:     `[[0,0],[1,0],[1,1],[0,1]]`,
		RuleConfig:      ruleConfig,
		Threshold:       0.5,
		MinFrames:       1,
		CooldownSeconds: 1,
		IsEnabled:       true,
	}
}

func detectOnce(t *testing.T, candidates []ObjectCandidate, rule DetectionRule) []Detection {
	t.Helper()
	detector := NewObjectRuleDetector(fakeObjectDetector{candidates: candidates}, ObjectRuleDetectorOptions{})
	detector.now = func() time.Time { return time.Unix(100, 0) }
	detections, err := detector.Detect(context.Background(), Frame{CameraId: 3}, []DetectionRule{rule})
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	return detections
}

func TestLPRAnyPlateFires(t *testing.T) {
	candidates := []ObjectCandidate{plateCandidate("WXY 1234", 0.92, "car", "white")}
	detections := detectOnce(t, candidates, lprRule(""))
	if len(detections) != 1 {
		t.Fatalf("detections = %d, want 1", len(detections))
	}
	d := detections[0]
	if d.DetectionType != DetectionLicensePlate {
		t.Fatalf("detectionType = %q", d.DetectionType)
	}
	if !strings.Contains(d.Label, "WXY1234") {
		t.Fatalf("label = %q, want normalized plate", d.Label)
	}
	if !strings.Contains(d.Label, "white car") {
		t.Fatalf("label = %q, want vehicle descriptor", d.Label)
	}
	for _, want := range []string{`"plate":"WXY1234"`, `"vehicleType":"car"`, `"color":"white"`} {
		if !strings.Contains(d.Metadata, want) {
			t.Fatalf("metadata = %q, missing %q", d.Metadata, want)
		}
	}
}

func TestLPRBelowOCRConfidenceDoesNotFire(t *testing.T) {
	candidates := []ObjectCandidate{plateCandidate("WXY1234", 0.3, "car", "white")}
	detections := detectOnce(t, candidates, lprRule(`{"minOcrConfidence":0.5}`))
	if len(detections) != 0 {
		t.Fatalf("detections = %d, want 0 (OCR below floor)", len(detections))
	}
}

func TestLPRIncludeWatchlistMatchesFuzzily(t *testing.T) {
	// Watchlist has "WXY1234"; OCR misread the 0/O — still within edit distance 1.
	candidates := []ObjectCandidate{plateCandidate("WXY12E4", 0.9, "car", "blue")}
	detections := detectOnce(t, candidates, lprRule(`{"plates":["WXY-1234"],"matchMode":"include"}`))
	if len(detections) != 1 {
		t.Fatalf("detections = %d, want 1 (fuzzy watchlist match)", len(detections))
	}
	if !strings.Contains(detections[0].Metadata, `"watchlisted":true`) {
		t.Fatalf("metadata = %q, want watchlisted true", detections[0].Metadata)
	}
}

func TestLPRIncludeRejectsNonListedPlate(t *testing.T) {
	candidates := []ObjectCandidate{plateCandidate("ZZZ9999", 0.9, "car", "red")}
	detections := detectOnce(t, candidates, lprRule(`{"plates":["WXY1234"],"matchMode":"include"}`))
	if len(detections) != 0 {
		t.Fatalf("detections = %d, want 0 (plate not on watchlist)", len(detections))
	}
}

func TestLPRExcludeFiresOnUnknownPlate(t *testing.T) {
	candidates := []ObjectCandidate{plateCandidate("ZZZ9999", 0.9, "car", "red")}
	detections := detectOnce(t, candidates, lprRule(`{"plates":["WXY1234"],"matchMode":"exclude"}`))
	if len(detections) != 1 {
		t.Fatalf("detections = %d, want 1 (unknown plate under exclude)", len(detections))
	}
	if !strings.Contains(detections[0].Metadata, `"watchlisted":false`) {
		t.Fatalf("metadata = %q, want watchlisted false", detections[0].Metadata)
	}
}

func TestLPRExcludeSuppressesKnownPlate(t *testing.T) {
	candidates := []ObjectCandidate{plateCandidate("WXY1234", 0.9, "car", "red")}
	detections := detectOnce(t, candidates, lprRule(`{"plates":["WXY1234"],"matchMode":"exclude"}`))
	if len(detections) != 0 {
		t.Fatalf("detections = %d, want 0 (known plate suppressed under exclude)", len(detections))
	}
}

func TestLPROutOfZonePlateIgnored(t *testing.T) {
	c := plateCandidate("WXY1234", 0.9, "car", "white")
	c.Box = Box{X: 0.9, Y: 0.9, W: 0.05, H: 0.03} // centre outside the restricted zone
	rule := lprRule("")
	rule.ZonePolygon = `[[0,0],[0.4,0],[0.4,0.4],[0,0.4]]`
	detections := detectOnce(t, []ObjectCandidate{c}, rule)
	if len(detections) != 0 {
		t.Fatalf("detections = %d, want 0 (plate outside zone)", len(detections))
	}
}

func TestLPRPicksHighestOCRConfidence(t *testing.T) {
	candidates := []ObjectCandidate{
		plateCandidate("AAA1111", 0.6, "car", "black"),
		plateCandidate("BBB2222", 0.95, "truck", "white"),
	}
	detections := detectOnce(t, candidates, lprRule(""))
	if len(detections) != 1 {
		t.Fatalf("detections = %d, want 1", len(detections))
	}
	if !strings.Contains(detections[0].Label, "BBB2222") {
		t.Fatalf("label = %q, want highest-OCR plate BBB2222", detections[0].Label)
	}
}

func TestValidateLPRRuleRequiresPlatesForInclude(t *testing.T) {
	rule := lprRule(`{"matchMode":"include"}`)
	if err := ValidateDetectionRule(rule); err == nil {
		t.Fatal("ValidateDetectionRule() = nil, want error for include without plates")
	}
}

func TestValidateLPRRuleAcceptsAnyMode(t *testing.T) {
	rule := lprRule(`{"matchMode":"any"}`)
	if err := ValidateDetectionRule(rule); err != nil {
		t.Fatalf("ValidateDetectionRule() = %v, want nil", err)
	}
}

func TestNormalizePlateStripsSeparators(t *testing.T) {
	if got := normalizePlate(" wxy-12 34 "); got != "WXY1234" {
		t.Fatalf("normalizePlate = %q, want WXY1234", got)
	}
}
