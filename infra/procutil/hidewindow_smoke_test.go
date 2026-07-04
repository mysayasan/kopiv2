package procutil

import (
	"bytes"
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

// TestHideWindowKeepsStdio verifies that hiding the console window does not break
// stdin/stdout redirection on a real child process — the exact pattern the ffmpeg /
// detector spawns rely on.
func TestHideWindowKeepsStdio(t *testing.T) {
	var name string
	var args []string
	switch runtime.GOOS {
	case "windows":
		// findstr echoes stdin lines that contain "x".
		name, args = "findstr", []string{"x"}
	default:
		name, args = "cat", nil
	}
	cmd := exec.Command(name, args...)
	HideWindow(cmd)
	cmd.Stdin = strings.NewReader("x-marks\ny-nope\n")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("run %s with hidden window: %v", name, err)
	}
	if !strings.Contains(out.String(), "x-marks") {
		t.Fatalf("stdio redirection broken with hidden window: got %q", out.String())
	}
}
