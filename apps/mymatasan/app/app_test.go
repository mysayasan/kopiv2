package app

import (
	"testing"

	"github.com/mysayasan/kopiv2/infra/config"
	"github.com/mysayasan/kopiv2/infra/vision"
)

func TestMymatasanSharedAPIsExposeOnlyPublicVersion(t *testing.T) {
	cfg := New().(*module).SharedAPIs()
	if !cfg.Version {
		t.Fatalf("expected mymatasan version API to remain enabled: %+v", cfg)
	}
	if cfg.AppRegistry || cfg.ApiEndpoint || cfg.ApiEndpointRbac || cfg.FileStorage || cfg.CacheService || cfg.ApiLog || cfg.RuntimeLog {
		t.Fatalf("expected mymatasan shared APIs that require Auth/RBAC to be disabled: %+v", cfg)
	}
}

func TestVisionDetectorDefaultsToMotion(t *testing.T) {
	detector, err := visionDetectorFromAppConfig(&config.AppConfigModel{})
	if err != nil {
		t.Fatalf("visionDetectorFromAppConfig() error = %v", err)
	}
	if _, ok := detector.(*vision.MotionDetector); !ok {
		t.Fatalf("detector = %T, want *vision.MotionDetector", detector)
	}
}

func TestHybridVisionDetectorFallsBackToMotionWhenCommandMissing(t *testing.T) {
	cfg := &config.AppConfigModel{}
	cfg.Vision.Detector.Mode = vision.DetectorModeHybrid
	cfg.Vision.Detector.Command = "definitely-missing-ai-tool"

	detector, err := visionDetectorFromAppConfig(cfg)
	if err != nil {
		t.Fatalf("visionDetectorFromAppConfig() error = %v", err)
	}
	if _, ok := detector.(*vision.MotionDetector); !ok {
		t.Fatalf("detector = %T, want *vision.MotionDetector", detector)
	}
}

func TestPersistentVisionDetectorFallsBackToMotionWhenCommandMissing(t *testing.T) {
	cfg := &config.AppConfigModel{}
	cfg.Vision.Detector.Mode = vision.DetectorModePersistent
	cfg.Vision.Detector.Command = "definitely-missing-ai-tool"

	detector, err := visionDetectorFromAppConfig(cfg)
	if err != nil {
		t.Fatalf("visionDetectorFromAppConfig() error = %v", err)
	}
	if _, ok := detector.(*vision.MotionDetector); !ok {
		t.Fatalf("detector = %T, want *vision.MotionDetector", detector)
	}
}
