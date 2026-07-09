package recording

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/mysayasan/kopiv2/infra/procutil"
)

// ExtractFrameJPEG grabs a single JPEG frame at seekSeconds from an mp4 stream and
// scales it to `width` px wide (aspect preserved) to keep thumbnails light. A pipe is
// not seekable, so the (already-decrypted) source is spooled to a temp file first,
// letting ffmpeg fast-seek by input option (-ss before -i) instead of decoding from
// the start. If the seek lands past the clip end (empty output), it retries at 0 so a
// thumbnail is still produced.
func ExtractFrameJPEG(ctx context.Context, ffmpegPath string, src io.Reader, seekSeconds int64, width int) ([]byte, error) {
	resolved, err := resolveFFmpeg(ffmpegPath)
	if err != nil {
		return nil, err
	}
	tmp, err := os.CreateTemp("", "mmthumb-*.mp4")
	if err != nil {
		return nil, err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := io.Copy(tmp, src); err != nil {
		tmp.Close()
		return nil, err
	}
	tmp.Close()

	if width <= 0 {
		width = 480
	}
	if seekSeconds < 0 {
		seekSeconds = 0
	}

	frame, err := runFrameGrab(ctx, resolved, tmpPath, seekSeconds, width)
	if err != nil {
		return nil, err
	}
	if len(frame) == 0 && seekSeconds > 0 {
		// Seek was past the end (or on a gap) — fall back to the first frame.
		frame, err = runFrameGrab(ctx, resolved, tmpPath, 0, width)
		if err != nil {
			return nil, err
		}
	}
	if len(frame) == 0 {
		return nil, fmt.Errorf("extract frame: empty output")
	}
	return frame, nil
}

func runFrameGrab(ctx context.Context, ffmpeg, srcPath string, seekSeconds int64, width int) ([]byte, error) {
	args := []string{"-hide_banner", "-loglevel", "error"}
	if seekSeconds > 0 {
		args = append(args, "-ss", strconv.FormatInt(seekSeconds, 10))
	}
	args = append(args,
		"-i", srcPath,
		"-frames:v", "1",
		"-vf", fmt.Sprintf("scale=%d:-2", width),
		"-f", "mjpeg",
		"pipe:1",
	)
	cmd := exec.CommandContext(ctx, ffmpeg, args...)
	procutil.HideWindow(cmd)
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("extract frame: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return out.Bytes(), nil
}
