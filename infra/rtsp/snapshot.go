package rtsp

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
)

// CaptureJPEG captures one JPEG frame from an RTSP stream with ffmpeg.
func CaptureJPEG(ctx context.Context, uri string, opts MJPEGOptions) ([]byte, error) {
	uri = strings.TrimSpace(uri)
	if uri == "" {
		return nil, fmt.Errorf("rtsp uri is required")
	}
	ffmpegPath, err := ResolveFFmpegPath(opts.FFmpegPath)
	if err != nil {
		return nil, err
	}
	maxWidth := opts.MaxWidth
	if maxWidth <= 0 {
		maxWidth = 640
	}
	if maxWidth > 1920 {
		maxWidth = 1920
	}
	quality := opts.Quality
	if quality <= 0 {
		quality = 5
	}
	args := append(baseFFmpegArgs(opts, uri),
		"-an",
		"-frames:v", "1",
		"-vf", "scale="+strconv.Itoa(maxWidth)+":-2:flags=fast_bilinear",
		"-q:v", strconv.Itoa(quality),
		"-f", "image2pipe",
		"-vcodec", "mjpeg",
		"pipe:1",
	)
	cmd := exec.CommandContext(ctx, ffmpegPath, args...)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("open ffmpeg stderr failed: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("open ffmpeg stdout failed: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start ffmpeg failed: %w", err)
	}
	frame, readErr := io.ReadAll(io.LimitReader(stdout, 8*1024*1024))
	stderrData, _ := io.ReadAll(io.LimitReader(stderr, 4096))
	waitErr := cmd.Wait()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if readErr != nil {
		return nil, fmt.Errorf("read ffmpeg frame failed: %w", readErr)
	}
	if waitErr != nil {
		stderrText := strings.TrimSpace(string(stderrData))
		if stderrText != "" {
			return nil, fmt.Errorf("ffmpeg capture failed: %w: %s", waitErr, stderrText)
		}
		return nil, fmt.Errorf("ffmpeg capture failed: %w", waitErr)
	}
	if len(frame) == 0 {
		return nil, fmt.Errorf("ffmpeg returned empty frame")
	}
	return frame, nil
}
