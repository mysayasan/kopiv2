package services

import "testing"

func TestParseNvidiaSMILine(t *testing.T) {
	index, label := parseNvidiaSMILine("GPU 0: NVIDIA GeForce RTX 4060 (UUID: GPU-abc)")
	if index != "0" {
		t.Fatalf("index = %q, want 0", index)
	}
	if label != "CUDA GPU 0 - NVIDIA GeForce RTX 4060 (UUID: GPU-abc)" {
		t.Fatalf("label = %q", label)
	}
}

func TestContainsDeviceValue(t *testing.T) {
	devices := []DecoderGPUDeviceOption{{Value: "0", HWAccel: "cuda"}}
	if !containsDeviceValue(devices, "0", "CUDA") {
		t.Fatalf("containsDeviceValue() = false, want true")
	}
	if containsDeviceValue(devices, "0", "vaapi") {
		t.Fatalf("containsDeviceValue() = true, want false")
	}
}
