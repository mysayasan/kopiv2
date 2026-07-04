package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveDetectorScript(t *testing.T) {
	// A HomeDir with the worker under <home>/ai, mirroring both the dev tree
	// (apps/mymatasan/ai) and the staged bundle (bin/ai).
	home := t.TempDir()
	aiDir := filepath.Join(home, "ai")
	if err := os.MkdirAll(aiDir, 0o755); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(aiDir, "yolo_worker.py")
	if err := os.WriteFile(script, []byte("# worker"), 0o644); err != nil {
		t.Fatal(err)
	}
	wantAbs, _ := filepath.Abs(script)

	cases := []struct {
		name string
		arg  string
		want string
	}{
		{"home-relative", "ai/yolo_worker.py", wantAbs},
		{"legacy repo-relative recovered by basename", "./apps/mymatasan/ai/yolo_worker.py", wantAbs},
		{"non-script arg untouched", "--verbose", "--verbose"},
		{"missing script left as-is", "ai/nope.py", "ai/nope.py"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveDetectorScript(home, tc.arg)
			if got != tc.want {
				t.Fatalf("resolveDetectorScript(%q) = %q, want %q", tc.arg, got, tc.want)
			}
		})
	}
}

func TestResolveDetectorScriptAbsoluteUntouched(t *testing.T) {
	abs := filepath.Join(t.TempDir(), "custom", "worker.py")
	if got := resolveDetectorScript("/some/home", abs); got != abs {
		t.Fatalf("absolute path should be returned untouched: got %q", got)
	}
}
