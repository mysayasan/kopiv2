package vision

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/mysayasan/kopiv2/infra/externaltools"
)

type PersistentObjectDetectorOptions struct {
	Command string
	Args    []string
	Timeout time.Duration
}

// PersistentObjectDetector keeps one model runner process alive and exchanges
// newline-delimited JSON requests with it.
type PersistentObjectDetector struct {
	mu         sync.Mutex
	command    string
	args       []string
	timeout    time.Duration
	cmd        *exec.Cmd
	stdin      io.WriteCloser
	stdoutPipe io.ReadCloser
	stdout     *bufio.Reader
	// paused stops the worker and prevents it relaunching, so an external process
	// (e.g. the in-app dependency installer) can replace PyTorch files the running
	// worker would otherwise hold locked on Windows.
	paused bool
}

func NewPersistentObjectDetector(opts PersistentObjectDetectorOptions) (*PersistentObjectDetector, error) {
	command := strings.TrimSpace(opts.Command)
	if command == "" {
		return nil, fmt.Errorf("persistent detector command is required")
	}
	resolved, _, err := externaltools.ResolveExecutable(command, command, nil)
	if err != nil {
		return nil, fmt.Errorf("persistent detector command %q is not available: %w", command, err)
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 8 * time.Second
	}
	return &PersistentObjectDetector{
		command: resolved,
		args:    append([]string(nil), opts.Args...),
		timeout: timeout,
	}, nil
}

func (d *PersistentObjectDetector) DetectObjects(ctx context.Context, frame Frame) ([]ObjectCandidate, error) {
	runCtx := ctx
	cancel := func() {}
	if d.timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, d.timeout)
	}
	defer cancel()

	d.mu.Lock()
	defer d.mu.Unlock()

	if d.paused {
		return nil, nil
	}

	if err := d.startLocked(); err != nil {
		return nil, err
	}

	request := struct {
		CameraID int64   `json:"cameraId"`
		Format   string  `json:"format"`
		Image    string  `json:"image"`
		Conf     float64 `json:"inferConf,omitempty"`
		Iou      float64 `json:"inferIou,omitempty"`
		Augment  bool    `json:"inferAugment,omitempty"`
		Imgsz    int     `json:"inferImgsz,omitempty"`
		Half     bool    `json:"inferHalf,omitempty"`
		MaxDet   int     `json:"inferMaxDet,omitempty"`
	}{
		CameraID: frame.CameraId,
		Format:   nonEmpty(frame.Format, "jpeg"),
		Image:    base64.StdEncoding.EncodeToString(frame.Data),
		Conf:     frame.Inference.Conf,
		Iou:      frame.Inference.Iou,
		Augment:  frame.Inference.Augment,
		Imgsz:    frame.Inference.Imgsz,
		Half:     frame.Inference.Half,
		MaxDet:   frame.Inference.MaxDet,
	}
	data, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	data = append(data, '\n')
	if _, err := d.stdin.Write(data); err != nil {
		_ = d.stopLocked()
		return nil, fmt.Errorf("persistent detector write failed: %w", err)
	}

	type readResult struct {
		line string
		err  error
	}
	stdout := d.stdout
	resultCh := make(chan readResult, 1)
	go func() {
		line, err := stdout.ReadString('\n')
		resultCh <- readResult{line: line, err: err}
	}()

	select {
	case <-runCtx.Done():
		_ = d.stopLocked()
		return nil, runCtx.Err()
	case result := <-resultCh:
		if result.err != nil {
			_ = d.stopLocked()
			return nil, fmt.Errorf("persistent detector read failed: %w", result.err)
		}
		return parseObjectCandidates(bytes.NewReader([]byte(result.line)))
	}
}

func (d *PersistentObjectDetector) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.stopLocked()
}

// Reload stops the worker process so the next inference relaunches it. The worker
// re-reads the active-model pointer on startup, so this hot-swaps the loaded
// model after the training "activate" action rewrites that pointer.
func (d *PersistentObjectDetector) Reload() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.stopLocked()
}

// Pause stops the worker and blocks it from relaunching until Resume is called.
// While paused, DetectObjects is a no-op (returns no candidates) so the live
// monitor keeps running without re-locking the model files mid-install.
func (d *PersistentObjectDetector) Pause() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.paused = true
	_ = d.stopLocked()
}

// Resume re-enables the worker; the next DetectObjects relaunches it.
func (d *PersistentObjectDetector) Resume() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.paused = false
}

func (d *PersistentObjectDetector) startLocked() error {
	if d.cmd != nil {
		return nil
	}
	cmd := exec.Command(d.command, d.args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return err
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdoutPipe.Close()
		return fmt.Errorf("start persistent detector failed: %w", err)
	}
	d.cmd = cmd
	d.stdin = stdin
	d.stdoutPipe = stdoutPipe
	d.stdout = bufio.NewReader(stdoutPipe)
	return nil
}

func (d *PersistentObjectDetector) stopLocked() error {
	var result error
	if d.stdin != nil {
		result = d.stdin.Close()
		d.stdin = nil
	}
	if d.stdoutPipe != nil {
		if err := d.stdoutPipe.Close(); result == nil {
			result = err
		}
		d.stdoutPipe = nil
	}
	d.stdout = nil
	if d.cmd != nil {
		if d.cmd.Process != nil {
			_ = d.cmd.Process.Kill()
		}
		if err := d.cmd.Wait(); result == nil {
			result = err
		}
		d.cmd = nil
	}
	return result
}
