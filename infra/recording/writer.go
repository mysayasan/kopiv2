package recording

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/mysayasan/kopiv2/infra/procutil"
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

// resolveFFprobe locates the ffprobe binary, preferring one sitting next to the
// already-resolved ffmpeg executable and falling back to PATH. Returns "" when
// ffprobe cannot be found (callers then fall back to inferred durations).
func resolveFFprobe(ffmpegPath string) string {
	lower := strings.ToLower(ffmpegPath)
	switch {
	case strings.HasSuffix(lower, "ffmpeg.exe"):
		candidate := ffmpegPath[:len(ffmpegPath)-len("ffmpeg.exe")] + "ffprobe.exe"
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	case strings.HasSuffix(lower, "ffmpeg"):
		candidate := ffmpegPath[:len(ffmpegPath)-len("ffmpeg")] + "ffprobe"
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	if resolved, err := exec.LookPath("ffprobe"); err == nil {
		return resolved
	}
	return ""
}

// probeDurationSeconds returns the media duration of file in seconds, or 0 when
// it cannot be determined.
func probeDurationSeconds(ffprobePath, file string) float64 {
	if strings.TrimSpace(ffprobePath) == "" {
		return 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	probe := exec.CommandContext(ctx, ffprobePath,
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		file,
	)
	procutil.HideWindow(probe)
	out, err := probe.Output()
	if err != nil {
		return 0
	}
	seconds, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	if err != nil || seconds <= 0 {
		return 0
	}
	return seconds
}

// probeVideoCodec returns the first video stream's codec name (e.g. "h264",
// "hevc") for file, or "" when it cannot be determined. Used at remux time for
// copy mode, where the on-disk codec is whatever the camera sent, so the playback
// path can later decide whether the browser needs a transcode.
func probeVideoCodec(ffmpegPath, file string) string {
	ffprobePath := resolveFFprobe(ffmpegPath)
	if strings.TrimSpace(ffprobePath) == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	probe := exec.CommandContext(ctx, ffprobePath,
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=codec_name",
		"-of", "default=noprint_wrappers=1:nokey=1",
		file,
	)
	procutil.HideWindow(probe)
	out, err := probe.Output()
	if err != nil {
		return ""
	}
	return canonicalCodec(strings.TrimSpace(string(out)))
}

// segmentEndedAt returns startedAt plus the segment's true media duration when
// it can be probed, otherwise the inferred fallback (derived from the next
// segment's start time). Probing keeps the stored duration accurate even when
// ffmpeg exited mid-segment and restarted, so the playback timeline does not
// overstate a truncated segment's length — which otherwise surfaces as skipped
// time when scrubbing.
func segmentEndedAt(ffmpegPath, file string, startedAt, fallback int64) int64 {
	if duration := probeDurationSeconds(resolveFFprobe(ffmpegPath), file); duration > 0 {
		return startedAt + int64(duration+0.5)
	}
	return fallback
}
