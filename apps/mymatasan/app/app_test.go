package app

import (
	"testing"

	mmconfig "github.com/mysayasan/kopiv2/apps/mymatasan/config"
	"github.com/mysayasan/kopiv2/infra/vision"
)

func TestMymatasanSharedAPIsExposeOnlyPublicVersion(t *testing.T) {
	cfg := New().(*module).SharedAPIs()
	if !cfg.Version {
		t.Fatalf("expected mymatasan version API to remain enabled: %+v", cfg)
	}
	if cfg.AppRegistry || cfg.ApiEndpoint || cfg.FileStorage || cfg.CacheService || cfg.ApiLog || cfg.RuntimeLog {
		t.Fatalf("expected mymatasan shared APIs that require Auth/RBAC to be disabled: %+v", cfg)
	}
}

// buildAndWrapDetector mirrors RegisterAppRoutes: build the shared object backend
// (nil on failure / motion mode) then wrap it into the live monitor detector.
func buildAndWrapDetector(cfg *mmconfig.Config) vision.Detector {
	backend, err := buildTrainingObjectDetector(cfg.Vision.Detector)
	if err != nil {
		backend = nil
	}
	return wrapMonitorDetector(cfg, backend)
}

func TestVisionDetectorDefaultsToMotion(t *testing.T) {
	detector := buildAndWrapDetector(&mmconfig.Config{})
	if _, ok := detector.(*vision.MotionDetector); !ok {
		t.Fatalf("detector = %T, want *vision.MotionDetector", detector)
	}
}

func TestHybridVisionDetectorFallsBackToMotionWhenCommandMissing(t *testing.T) {
	cfg := &mmconfig.Config{}
	cfg.Vision.Detector.Mode = vision.DetectorModeHybrid
	cfg.Vision.Detector.Command = "definitely-missing-ai-tool"

	if _, ok := buildAndWrapDetector(cfg).(*vision.MotionDetector); !ok {
		t.Fatalf("hybrid with missing command should fall back to motion")
	}
}

func TestPersistentVisionDetectorFallsBackToMotionWhenCommandMissing(t *testing.T) {
	cfg := &mmconfig.Config{}
	cfg.Vision.Detector.Mode = vision.DetectorModePersistent
	cfg.Vision.Detector.Command = "definitely-missing-ai-tool"

	if _, ok := buildAndWrapDetector(cfg).(*vision.MotionDetector); !ok {
		t.Fatalf("persistent with missing command should fall back to motion")
	}
}
