package services

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSidecarMissingReason(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "llama-server.exe")
	os.WriteFile(real, []byte("stub"), 0o755)
	model := filepath.Join(dir, "m.gguf")
	os.WriteFile(model, []byte("gguf"), 0o644)

	cases := []struct {
		binary, model string
		wantEmpty     bool
	}{
		{"", "", false},
		{real, "", false},
		{"", model, false},
		{filepath.Join(dir, "gone.exe"), model, false},
		{real, filepath.Join(dir, "gone.gguf"), false},
		{real, model, true},
	}
	for _, tc := range cases {
		got := sidecarMissingReason(tc.binary, tc.model)
		if (got == "") != tc.wantEmpty {
			t.Errorf("sidecarMissingReason(%q,%q) = %q", tc.binary, tc.model, got)
		}
	}
}

func TestResolveSidecarModelFallbacks(t *testing.T) {
	llmDir := t.TempDir()
	models := llmDirModels(llmDir)
	os.MkdirAll(models, 0o755)

	// Explicit configuration always wins, even when nothing exists yet.
	if got := resolveSidecarModel("C:\\somewhere\\else.gguf", llmDir); got != "C:\\somewhere\\else.gguf" {
		t.Fatalf("configured path must win, got %q", got)
	}
	// Empty dir: nothing to resolve.
	if got := resolveSidecarModel("", llmDir); got != "" {
		t.Fatalf("empty models dir should resolve to nothing, got %q", got)
	}
	// A single gguf resolves by itself.
	one := filepath.Join(models, "only.gguf")
	os.WriteFile(one, []byte("g"), 0o644)
	if got := resolveSidecarModel("", llmDir); got != one {
		t.Fatalf("single model should resolve, got %q", got)
	}
	// Two ggufs and no configuration: refuse to guess.
	os.WriteFile(filepath.Join(models, "second.gguf"), []byte("g"), 0o644)
	if got := resolveSidecarModel("", llmDir); got != "" {
		t.Fatalf("ambiguous models must not auto-resolve, got %q", got)
	}
	// Unless the DEFAULT model is one of them — that one is the pin.
	def := filepath.Join(models, defaultModelFile)
	os.WriteFile(def, []byte("g"), 0o644)
	if got := resolveSidecarModel("", llmDir); got != def {
		t.Fatalf("default model should win among many, got %q", got)
	}
}

func TestSidecarDisabledStaysOff(t *testing.T) {
	s := NewLLMSidecar(SidecarConfig{Enabled: false}, t.TempDir(), nil)
	state, _ := s.Status()
	if state != StateOff {
		t.Fatalf("disabled sidecar state = %q, want off", state)
	}
	if s.Endpoint() != "http://127.0.0.1:49540/v1" {
		t.Fatalf("default endpoint = %q", s.Endpoint())
	}
}

func TestSidecarDefaultsApplied(t *testing.T) {
	s := NewLLMSidecar(SidecarConfig{Enabled: true, Port: 0, CtxSize: 0}, t.TempDir(), nil)
	if s.cfg.Port != sidecarDefaultPort || s.cfg.CtxSize != sidecarDefaultCtxSize {
		t.Fatalf("defaults not applied: %+v", s.cfg)
	}
}
