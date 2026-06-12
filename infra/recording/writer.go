package recording

import (
	"fmt"
	"os/exec"
	"strings"
)

func resolveFFmpeg(configuredPath string) (string, error) {
	path := strings.TrimSpace(configuredPath)
	if path == "" {
		path = "ffmpeg"
	}
	resolved, err := exec.LookPath(path)
	if err != nil {
		return "", fmt.Errorf("recording: ffmpeg not found at %q: %w", path, err)
	}
	return resolved, nil
}
