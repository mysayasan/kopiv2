package services

import (
	"context"
	"strings"
	"testing"

	"github.com/mysayasan/kopiv2/infra/vision"
)

func TestCheckVisionToolReportsNativeMotionModeReady(t *testing.T) {
	status := CheckVisionTool(context.Background(), VisionToolSettings{Mode: vision.DetectorModeMotion})

	if !status.Available || !status.NativeFallback || status.Required {
		t.Fatalf("status = %+v", status)
	}
	if !strings.Contains(status.Summary, "Native motion detector") {
		t.Fatalf("summary = %q", status.Summary)
	}
}

func TestCheckVisionToolReportsMissingAICommandWithFallback(t *testing.T) {
	status := CheckVisionTool(context.Background(), VisionToolSettings{
		Mode:              vision.DetectorModePersistent,
		Command:           "definitely-missing-ai-tool",
		UseMotionFallback: true,
	})

	if status.Available {
		t.Fatalf("Available = true, want false")
	}
	if !status.NativeFallback {
		t.Fatalf("NativeFallback = false, want true")
	}
	if status.CommandFound {
		t.Fatalf("CommandFound = true, want false")
	}
	if !strings.Contains(status.Summary, "native fallback remains available") {
		t.Fatalf("summary = %q", status.Summary)
	}
}
