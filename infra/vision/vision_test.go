package vision

import (
	"strings"
	"testing"
)

func TestNormalizeDetectionRuleDefaults(t *testing.T) {
	rule := NormalizeDetectionRule(DetectionRuleRequest{
		CameraId:      7,
		DetectionType: " Fire ",
		IsEnabled:     true,
		SoundEnabled:  true,
	})

	if rule.Name != "Fire detection" {
		t.Fatalf("name = %q", rule.Name)
	}
	if rule.DetectionType != DetectionFire {
		t.Fatalf("detectionType = %q", rule.DetectionType)
	}
	if rule.Threshold != DefaultDetectionThreshold {
		t.Fatalf("threshold = %v", rule.Threshold)
	}
	if rule.MinFrames != DefaultDetectionMinFrames {
		t.Fatalf("minFrames = %d", rule.MinFrames)
	}
	if rule.CooldownSeconds != DefaultDetectionCooldown {
		t.Fatalf("cooldownSeconds = %d", rule.CooldownSeconds)
	}
	if !rule.IsEnabled || !rule.SoundEnabled {
		t.Fatalf("expected enabled rule with sound, got %#v", rule)
	}
}

func TestValidateDetectionRuleRejectsInvalidInputs(t *testing.T) {
	tests := []struct {
		name string
		rule DetectionRule
		want string
	}{
		{
			name: "missing camera",
			rule: DetectionRule{DetectionType: DetectionFire, Threshold: 0.75, MinFrames: 3},
			want: "cameraId is required",
		},
		{
			name: "bad threshold",
			rule: DetectionRule{CameraId: 1, DetectionType: DetectionFire, Threshold: 1.25, MinFrames: 3},
			want: "threshold must be greater than 0 and at most 1",
		},
		{
			name: "bad zone json",
			rule: DetectionRule{CameraId: 1, DetectionType: DetectionFire, Threshold: 0.75, MinFrames: 3, ZonePolygon: "[bad"},
			want: "zonePolygon must be valid JSON",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateDetectionRule(tt.rule)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("ValidateDetectionRule() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestValidateAlertEventRejectsInvalidInputs(t *testing.T) {
	tests := []struct {
		name  string
		alert AlertEvent
		want  string
	}{
		{
			name:  "missing rule",
			alert: AlertEvent{CameraId: 1, DetectionType: DetectionFire, Confidence: 0.7},
			want:  "ruleId is required",
		},
		{
			name:  "bad confidence",
			alert: AlertEvent{RuleId: 1, CameraId: 1, DetectionType: DetectionFire, Confidence: 1.2},
			want:  "confidence must be between 0 and 1",
		},
		{
			name:  "bad metadata json",
			alert: AlertEvent{RuleId: 1, CameraId: 1, DetectionType: DetectionFire, Confidence: 0.7, Metadata: "{bad"},
			want:  "metadata must be valid JSON",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateAlertEvent(tt.alert)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("ValidateAlertEvent() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestValidateAlertEventAcceptsOptionalJSONFields(t *testing.T) {
	alert := NormalizeAlertEvent(AlertEventRequest{
		RuleId:        1,
		CameraId:      2,
		DetectionType: "Fire",
		Confidence:    0.82,
		ZonePolygon:   `[[0.1,0.1],[0.9,0.1],[0.9,0.9]]`,
		BoundingBox:   `{"x":0.25,"y":0.3,"w":0.4,"h":0.2}`,
		Metadata:      `{"source":"manual-test"}`,
	})

	if err := ValidateAlertEvent(alert); err != nil {
		t.Fatalf("ValidateAlertEvent() error = %v", err)
	}
	if alert.Label != DetectionFire {
		t.Fatalf("label = %q", alert.Label)
	}
}
