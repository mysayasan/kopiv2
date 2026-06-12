package vision

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/mysayasan/kopiv2/infra/externaltools"
)

type ExternalObjectDetectorOptions struct {
	Command   string
	Args      []string
	Timeout   time.Duration
	MaxOutput int64
}

type ExternalObjectDetector struct {
	command   string
	args      []string
	timeout   time.Duration
	maxOutput int64
}

func NewExternalObjectDetector(opts ExternalObjectDetectorOptions) (*ExternalObjectDetector, error) {
	command := strings.TrimSpace(opts.Command)
	if command == "" {
		return nil, fmt.Errorf("external detector command is required")
	}
	resolved, _, err := externaltools.ResolveExecutable(command, command, nil)
	if err != nil {
		return nil, fmt.Errorf("external detector command %q is not available: %w", command, err)
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	maxOutput := opts.MaxOutput
	if maxOutput <= 0 {
		maxOutput = 1024 * 1024
	}
	return &ExternalObjectDetector{
		command:   resolved,
		args:      append([]string(nil), opts.Args...),
		timeout:   timeout,
		maxOutput: maxOutput,
	}, nil
}

func (d *ExternalObjectDetector) DetectObjects(ctx context.Context, frame Frame) ([]ObjectCandidate, error) {
	runCtx := ctx
	cancel := func() {}
	if d.timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, d.timeout)
	}
	defer cancel()

	cmd := exec.CommandContext(runCtx, d.command, d.args...)
	cmd.Stdin = bytes.NewReader(frame.Data)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if runCtx.Err() != nil {
			return nil, runCtx.Err()
		}
		message := strings.TrimSpace(stderr.String())
		if message != "" {
			return nil, fmt.Errorf("external detector failed: %w: %s", err, message)
		}
		return nil, fmt.Errorf("external detector failed: %w", err)
	}
	if int64(stdout.Len()) > d.maxOutput {
		return nil, fmt.Errorf("external detector output exceeds %d bytes", d.maxOutput)
	}
	return parseObjectCandidates(bytes.NewReader(stdout.Bytes()))
}

func parseObjectCandidates(reader io.Reader) ([]ObjectCandidate, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil, nil
	}

	var direct []ObjectCandidate
	if err := json.Unmarshal(data, &direct); err == nil {
		return normalizeObjectCandidates(direct), nil
	}

	var errorResponse struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(data, &errorResponse); err == nil && strings.TrimSpace(errorResponse.Error) != "" {
		return nil, fmt.Errorf("external detector returned error: %s", strings.TrimSpace(errorResponse.Error))
	}

	var wrapped struct {
		Detections []ObjectCandidate `json:"detections"`
		Objects    []ObjectCandidate `json:"objects"`
	}
	if err := json.Unmarshal(data, &wrapped); err != nil {
		return nil, fmt.Errorf("parse external detector JSON failed: %w", err)
	}
	if wrapped.Detections != nil {
		return normalizeObjectCandidates(wrapped.Detections), nil
	}
	return normalizeObjectCandidates(wrapped.Objects), nil
}

func normalizeObjectCandidates(candidates []ObjectCandidate) []ObjectCandidate {
	result := make([]ObjectCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		candidate.Label = strings.ToLower(strings.TrimSpace(candidate.Label))
		candidate.Confidence = clamp(candidate.Confidence)
		candidate.Box = normalizeBox(candidate.Box)
		if candidate.Label == "" {
			continue
		}
		result = append(result, candidate)
	}
	return result
}
