package rtsp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
)

// MJPEGOptions controls RTSP to multipart MJPEG transcoding.
type MJPEGOptions struct {
	FFmpegPath    string
	FPS           int
	MaxWidth      int
	RTSPTransport string
}

// StreamMJPEG converts an RTSP stream into multipart MJPEG and writes it to dst.
func StreamMJPEG(ctx context.Context, dst io.Writer, uri string, opts MJPEGOptions) error {
	uri = strings.TrimSpace(uri)
	if uri == "" {
		return errors.New("rtsp uri is required")
	}

	ffmpegPath, err := ResolveFFmpegPath(opts.FFmpegPath)
	if err != nil {
		return err
	}
	fps := opts.FPS
	if fps <= 0 {
		fps = 8
	}
	if fps > 15 {
		fps = 15
	}
	maxWidth := opts.MaxWidth
	if maxWidth < 0 {
		maxWidth = 0
	}
	if maxWidth > 1920 {
		maxWidth = 1920
	}
	transport := strings.TrimSpace(opts.RTSPTransport)
	if transport == "" {
		transport = "tcp"
	}

	filters := []string{fmt.Sprintf("fps=%d", fps)}
	if maxWidth > 0 {
		filters = append(filters, fmt.Sprintf("scale=%d:-2:flags=fast_bilinear", maxWidth))
	}

	args := []string{
		"-hide_banner",
		"-loglevel", "error",
		"-fflags", "nobuffer",
		"-flags", "low_delay",
		"-probesize", "1000000",
		"-analyzeduration", "1000000",
		"-rtsp_transport", transport,
		"-i", uri,
		"-an",
		"-r", strconv.Itoa(fps),
		"-vf", strings.Join(filters, ","),
		"-q:v", "7",
		"-threads", "1",
		"-flush_packets", "1",
		"-f", "mpjpeg",
		"-boundary_tag", "mymatasan",
		"pipe:1",
	}
	cmd := exec.CommandContext(ctx, ffmpegPath, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("open ffmpeg stdout failed: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("open ffmpeg stderr failed: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start ffmpeg failed: %w", err)
	}

	errCh := make(chan []byte, 1)
	go func() {
		data, _ := io.ReadAll(io.LimitReader(stderr, 4096))
		errCh <- data
	}()

	copyErr := copyWithContext(ctx, dst, stdout)
	waitErr := cmd.Wait()
	if ctx.Err() != nil {
		return nil
	}
	if copyErr != nil {
		return fmt.Errorf("stream ffmpeg mjpeg output failed: %w", copyErr)
	}
	if waitErr != nil {
		stderrText := strings.TrimSpace(string(<-errCh))
		if stderrText != "" {
			return fmt.Errorf("ffmpeg exited: %w: %s", waitErr, stderrText)
		}
		return fmt.Errorf("ffmpeg exited: %w", waitErr)
	}
	return nil
}

// ResolveFFmpegPath returns the configured ffmpeg executable path or resolves ffmpeg from PATH.
func ResolveFFmpegPath(configuredPath string) (string, error) {
	ffmpegPath := strings.TrimSpace(configuredPath)
	if ffmpegPath == "" {
		ffmpegPath = "ffmpeg"
	}
	resolved, err := exec.LookPath(ffmpegPath)
	if err != nil {
		return "", fmt.Errorf("ffmpeg executable not found at %q; set camera.ffmpegPath in mymatasan config: %w", ffmpegPath, err)
	}
	return resolved, nil
}

func copyWithContext(ctx context.Context, dst io.Writer, src io.Reader) error {
	buf := make([]byte, 32*1024)
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		n, readErr := src.Read(buf)
		if n > 0 {
			if _, writeErr := dst.Write(buf[:n]); writeErr != nil {
				return writeErr
			}
			if flusher, ok := dst.(interface{ Flush() }); ok {
				flusher.Flush()
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return nil
			}
			return readErr
		}
	}
}
