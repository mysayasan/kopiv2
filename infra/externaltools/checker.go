package externaltools

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const defaultProbeTimeout = 2 * time.Second
const defaultMaxOutputBytes int64 = 8192

// ExecutableSpec describes an external executable that an app wants to find and probe.
type ExecutableSpec struct {
	Name           string
	ConfiguredPath string
	ExecutableName string
	CandidatePaths []string
	ProbeArgs      []string
	Timeout        time.Duration
	MaxOutputBytes int64
}

// ExecutableStatus reports whether an external executable is available and probeable.
type ExecutableStatus struct {
	Name        string `json:"name"`
	Found       bool   `json:"found"`
	Path        string `json:"path"`
	Source      string `json:"source"`
	ProbeOutput string `json:"probeOutput"`
	Error       string `json:"error"`
}

// CheckExecutable resolves an executable from configured path, PATH, and candidate paths,
// then optionally runs a short probe command against it.
func CheckExecutable(ctx context.Context, spec ExecutableSpec) ExecutableStatus {
	status := ExecutableStatus{Name: strings.TrimSpace(spec.Name)}
	resolved, source, err := ResolveExecutable(spec.ConfiguredPath, spec.ExecutableName, spec.CandidatePaths)
	if err != nil {
		status.Error = err.Error()
		return status
	}
	status.Found = true
	status.Path = resolved
	status.Source = source

	if len(spec.ProbeArgs) == 0 {
		return status
	}
	output, err := Probe(ctx, resolved, spec.ProbeArgs, spec.Timeout, spec.MaxOutputBytes)
	status.ProbeOutput = output
	if err != nil {
		status.Error = err.Error()
	}
	return status
}

// ResolveExecutable finds an executable from explicit path, PATH name, or known candidate paths.
func ResolveExecutable(configuredPath string, executableName string, candidatePaths []string) (string, string, error) {
	configuredPath = strings.TrimSpace(configuredPath)
	if configuredPath != "" {
		resolved, err := resolveOne(configuredPath)
		if err != nil {
			if looksLikePath(configuredPath) {
				return "", "", fmt.Errorf("configured executable %q not found: %w", configuredPath, err)
			}
			executableName = configuredPath
		} else {
			return resolved, "configured", nil
		}
	}

	executableName = strings.TrimSpace(executableName)
	if executableName != "" {
		resolved, err := exec.LookPath(executableName)
		if err == nil {
			return resolved, "path", nil
		}
	}

	for _, candidate := range candidatePaths {
		resolved, err := resolveCandidate(candidate)
		if err == nil {
			return resolved, "candidate", nil
		}
	}

	if executableName == "" {
		return "", "", errors.New("no executable name or configured path provided")
	}
	return "", "", fmt.Errorf("%s executable not found", executableName)
}

// Probe runs a bounded command and returns combined stdout/stderr output.
func Probe(ctx context.Context, executablePath string, args []string, timeout time.Duration, maxOutputBytes int64) (string, error) {
	if timeout <= 0 {
		timeout = defaultProbeTimeout
	}
	if maxOutputBytes <= 0 {
		maxOutputBytes = defaultMaxOutputBytes
	}
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(probeCtx, executablePath, args...)
	output, err := cmd.CombinedOutput()
	if int64(len(output)) > maxOutputBytes {
		output = output[:maxOutputBytes]
	}
	outputText := strings.TrimSpace(string(output))
	if probeCtx.Err() != nil {
		return outputText, probeCtx.Err()
	}
	if err != nil {
		if outputText != "" {
			return outputText, fmt.Errorf("%w: %s", err, outputText)
		}
		return outputText, err
	}
	return outputText, nil
}

func resolveOne(path string) (string, error) {
	if looksLikePath(path) {
		return resolveCandidate(path)
	}
	return exec.LookPath(path)
}

func resolveCandidate(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("empty executable path")
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("%q is a directory", path)
	}
	return filepath.Abs(path)
}

func looksLikePath(value string) bool {
	return strings.ContainsAny(value, `/\`) || filepath.IsAbs(value)
}

// LimitReader is exposed for callers that stream tool output and want the same default cap.
func LimitReader(r io.Reader) io.Reader {
	return io.LimitReader(r, defaultMaxOutputBytes)
}
