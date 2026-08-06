package services

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mysayasan/kopiv2/infra/procutil"
	"github.com/mysayasan/kopiv2/infra/safego"
)

// LLMSidecar supervises a llama.cpp llama-server child process on loopback.
//
// The supervision contract, learned the hard way elsewhere in the suite:
//   - EVERY spawn goes through procutil.HideWindow — a console child whose parent
//     has no console gets a fresh console window per spawn on Windows, and a
//     crash-restart loop turns that into a window storm.
//   - stderr is ALWAYS a pipe drained by us, never os.Stderr — under a detached
//     or service parent os.Stderr is an invalid handle and the child dies at
//     stdio init before it ever logs why.
//   - A missing binary or model parks the sidecar in StateOff with a reason; it
//     must NOT enter the restart loop, because restarting cannot conjure files.
//   - Crashes restart with exponential backoff (3s → 30s), forever, because the
//     operator may fix the cause (OOM, replaced model) without touching the app.
type LLMSidecar struct {
	llmDir string
	logf   func(format string, args ...any)
	// onRestart is an optional hook (metrics) fired on every crash-restart.
	onRestart func()

	mu       sync.Mutex
	cfg      SidecarConfig
	state    SidecarState
	lastErr  string
	paused   bool
	cmd      *exec.Cmd
	stopCmd  context.CancelFunc
	tail     []string // ring buffer of the child's last stderr lines
	started  bool
	wake     chan struct{} // nudges the supervisor out of its idle/backoff sleep
	restarts int64
}

// SidecarState is the lifecycle state surfaced to the UI.
type SidecarState string

const (
	// StateOff: not configured to run, or binary/model missing (see LastError).
	StateOff SidecarState = "off"
	// StateStarting: process spawned, waiting for /health to pass (model load).
	StateStarting SidecarState = "starting"
	// StateReady: /health passed; completions can be served.
	StateReady SidecarState = "ready"
	// StateFailed: the child exited or never became healthy; backoff then retry.
	StateFailed SidecarState = "failed"
	// StatePaused: an installer is replacing files; the child is held down.
	StatePaused SidecarState = "paused"
	// StateStopped: the app is shutting down.
	StateStopped SidecarState = "stopped"
)

// SidecarConfig is the resolved runtime shape of the child process.
type SidecarConfig struct {
	Enabled    bool // mode == "sidecar"
	Port       int
	CtxSize    int
	Threads    int
	BinaryPath string
	ModelPath  string
}

const (
	sidecarDefaultPort    = 49540
	sidecarDefaultCtxSize = 8192
	// sidecarHealthTimeout is how long a spawn may take to pass /health. Model
	// load from a cold page cache on a slow disk is the long pole — minutes-class
	// hardware exists, but 120s covers everything the default 1.1 GB model needs.
	sidecarHealthTimeout   = 120 * time.Second
	sidecarHealthInterval  = 2 * time.Second
	sidecarBackoffFloor    = 3 * time.Second
	sidecarBackoffCeiling  = 30 * time.Second
	sidecarStderrTailLines = 100
)

// NewLLMSidecar builds the supervisor. Call Start exactly once; reconfiguration
// happens via app restart (the agent settings section reports needsRestart).
func NewLLMSidecar(cfg SidecarConfig, llmDir string, logf func(string, ...any)) *LLMSidecar {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	if cfg.Port <= 0 {
		cfg.Port = sidecarDefaultPort
	}
	if cfg.CtxSize <= 0 {
		cfg.CtxSize = sidecarDefaultCtxSize
	}
	return &LLMSidecar{
		llmDir: llmDir,
		logf:   logf,
		cfg:    cfg,
		state:  StateOff,
		wake:   make(chan struct{}, 1),
	}
}

// SetOnRestart wires a crash-restart hook (metrics). Optional.
func (s *LLMSidecar) SetOnRestart(fn func()) { s.onRestart = fn }

// Endpoint is the OpenAI-compatible base URL of the running server.
func (s *LLMSidecar) Endpoint() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return "http://127.0.0.1:" + strconv.Itoa(s.cfg.Port) + "/v1"
}

// Status returns the current state and, for off/failed, why.
func (s *LLMSidecar) Status() (SidecarState, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state, s.lastErr
}

// Restarts reports how many crash-restarts the supervisor has performed.
func (s *LLMSidecar) Restarts() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.restarts
}

// StderrTail returns the child's most recent stderr lines (newest last) — the
// first place to look when the state is failed.
func (s *LLMSidecar) StderrTail() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.tail))
	copy(out, s.tail)
	return out
}

// ResolvedPaths reports the binary/model the sidecar would run, resolving the
// configured paths against the default install layout under <dataDir>/llm.
func (s *LLMSidecar) ResolvedPaths() (binary, model string) {
	s.mu.Lock()
	cfg := s.cfg
	s.mu.Unlock()
	return resolveSidecarBinary(cfg.BinaryPath, s.llmDir), resolveSidecarModel(cfg.ModelPath, s.llmDir)
}

// SetPaths updates the binary/model paths (called by the installer after a
// successful install/import so a restart is not needed just to point at them)
// and nudges the supervisor.
func (s *LLMSidecar) SetPaths(binary, model string) {
	s.mu.Lock()
	if binary != "" {
		s.cfg.BinaryPath = binary
	}
	if model != "" {
		s.cfg.ModelPath = model
	}
	s.mu.Unlock()
	s.nudge()
}

// Pause kills the child and holds it down so an installer can replace files the
// running process would keep locked on Windows. Always pair with Resume.
func (s *LLMSidecar) Pause() {
	s.mu.Lock()
	s.paused = true
	if s.cfg.Enabled {
		s.state = StatePaused
	}
	stop := s.stopCmd
	s.mu.Unlock()
	if stop != nil {
		stop()
	}
}

// Resume lifts a Pause and nudges the supervisor to relaunch.
func (s *LLMSidecar) Resume() {
	s.mu.Lock()
	s.paused = false
	s.mu.Unlock()
	s.nudge()
}

// Start launches the supervision loop. A sidecar with Enabled=false stays a
// no-op supervisor (state off) — constructing it unconditionally keeps the
// wiring simple and lets Status answer the UI in every mode.
func (s *LLMSidecar) Start(ctx context.Context) {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return
	}
	s.started = true
	enabled := s.cfg.Enabled
	s.mu.Unlock()
	if !enabled {
		return
	}
	safego.Supervise(ctx, "myseliasan.llm-sidecar", s.run)
}

func (s *LLMSidecar) run(ctx context.Context) {
	backoff := sidecarBackoffFloor
	for ctx.Err() == nil {
		if s.isPaused() {
			s.waitNudge(ctx, time.Second)
			continue
		}

		binary, model := s.ResolvedPaths()
		if reason := sidecarMissingReason(binary, model); reason != "" {
			s.setState(StateOff, reason)
			// Not a crash: wait for the installer's nudge rather than spinning.
			s.waitNudge(ctx, 10*time.Second)
			continue
		}

		s.setState(StateStarting, "")
		healthy, err := s.runChildOnce(ctx, binary, model)
		if ctx.Err() != nil {
			break
		}
		if s.isPaused() {
			continue // Pause killed it on purpose; not a failure.
		}
		msg := "llama-server exited"
		if err != nil {
			msg = err.Error()
		}
		s.setState(StateFailed, msg)
		s.bumpRestarts()
		if healthy {
			backoff = sidecarBackoffFloor // it ran fine for a while; restart promptly
		}
		s.logf("llm sidecar: %s — restarting in %s", msg, backoff)
		s.waitNudge(ctx, backoff)
		backoff *= 2
		if backoff > sidecarBackoffCeiling {
			backoff = sidecarBackoffCeiling
		}
	}
	s.setState(StateStopped, "")
	s.killChild()
}

// runChildOnce spawns llama-server and blocks until it exits (or ctx/Pause kills
// it). Returns whether it ever became healthy, and the exit error.
func (s *LLMSidecar) runChildOnce(parentCtx context.Context, binary, model string) (bool, error) {
	ctx, cancel := context.WithCancel(parentCtx)
	defer cancel()

	s.mu.Lock()
	cfg := s.cfg
	s.stopCmd = cancel
	s.mu.Unlock()

	args := []string{
		"--model", model,
		"--host", "127.0.0.1",
		"--port", strconv.Itoa(cfg.Port),
		"--ctx-size", strconv.Itoa(cfg.CtxSize),
		// The sidecar serves exactly one consumer (this app) on loopback; the
		// bundled web UI would be an unaudited extra surface, so it is disabled.
		"--no-webui",
	}
	if cfg.Threads > 0 {
		args = append(args, "--threads", strconv.Itoa(cfg.Threads))
	}

	cmd := exec.CommandContext(ctx, binary, args...)
	procutil.HideWindow(cmd)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return false, fmt.Errorf("stderr pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return false, fmt.Errorf("start llama-server: %w", err)
	}
	s.mu.Lock()
	s.cmd = cmd
	s.mu.Unlock()

	go s.drainStderr(stderr)

	// Health-watch in parallel with Wait: flip to ready when /health passes, fail
	// the spawn if it never does within the load window.
	healthCh := make(chan bool, 1)
	go func() { healthCh <- s.awaitHealthy(ctx, cfg.Port) }()

	waitErr := make(chan error, 1)
	go func() { waitErr <- cmd.Wait() }()

	becameHealthy := false
	for {
		select {
		case ok := <-healthCh:
			if ok {
				becameHealthy = true
				s.setState(StateReady, "")
				s.logf("llm sidecar: llama-server ready on 127.0.0.1:%d", cfg.Port)
			} else if ctx.Err() == nil {
				// Never got healthy inside the window: kill and report.
				cancel()
				err := <-waitErr
				return false, fmt.Errorf("llama-server did not become healthy within %s (last stderr: %s): %v",
					sidecarHealthTimeout, s.lastStderrLine(), err)
			}
		case err := <-waitErr:
			s.mu.Lock()
			s.cmd = nil
			s.stopCmd = nil
			s.mu.Unlock()
			if err != nil {
				return becameHealthy, fmt.Errorf("llama-server exited: %v (last stderr: %s)", err, s.lastStderrLine())
			}
			return becameHealthy, nil
		}
	}
}

// awaitHealthy polls GET /health until 200, timeout, or ctx cancel.
func (s *LLMSidecar) awaitHealthy(ctx context.Context, port int) bool {
	deadline := time.Now().Add(sidecarHealthTimeout)
	client := &http.Client{Timeout: 2 * time.Second}
	url := "http://127.0.0.1:" + strconv.Itoa(port) + "/health"
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return false
		case <-time.After(sidecarHealthInterval):
		}
		resp, err := client.Get(url)
		if err != nil {
			continue
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			return true
		}
	}
	return false
}

func (s *LLMSidecar) drainStderr(r interface{ Read([]byte) (int, error) }) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	for scanner.Scan() {
		line := scanner.Text()
		s.mu.Lock()
		s.tail = append(s.tail, line)
		if len(s.tail) > sidecarStderrTailLines {
			s.tail = s.tail[len(s.tail)-sidecarStderrTailLines:]
		}
		s.mu.Unlock()
	}
}

func (s *LLMSidecar) lastStderrLine() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := len(s.tail) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(s.tail[i]); l != "" {
			return l
		}
	}
	return "(none)"
}

func (s *LLMSidecar) setState(st SidecarState, reason string) {
	s.mu.Lock()
	s.state = st
	s.lastErr = reason
	s.mu.Unlock()
}

func (s *LLMSidecar) isPaused() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.paused
}

func (s *LLMSidecar) bumpRestarts() {
	s.mu.Lock()
	s.restarts++
	s.mu.Unlock()
	if s.onRestart != nil {
		s.onRestart()
	}
}

func (s *LLMSidecar) killChild() {
	s.mu.Lock()
	cmd := s.cmd
	s.cmd = nil
	s.mu.Unlock()
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}
}

func (s *LLMSidecar) nudge() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// waitNudge sleeps for d, waking early on a nudge or ctx cancel.
func (s *LLMSidecar) waitNudge(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-s.wake:
	case <-time.After(d):
	}
}

// resolveSidecarBinary resolves the configured binary path, falling back to the
// default install location under <llmDir>/bin (searched recursively, because the
// Linux release archive nests everything under a llama-<tag>/ directory).
func resolveSidecarBinary(configured, llmDir string) string {
	if p := strings.TrimSpace(configured); p != "" {
		return p
	}
	return findFileUnder(llmDirBin(llmDir), llamaServerExeName())
}

// resolveSidecarModel resolves the configured model path, falling back to the
// default model file, then to any single .gguf under <llmDir>/models.
func resolveSidecarModel(configured, llmDir string) string {
	if p := strings.TrimSpace(configured); p != "" {
		return p
	}
	dir := llmDirModels(llmDir)
	if def := dir + string(os.PathSeparator) + defaultModelFile; fileExists(def) {
		return def
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var only string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".gguf") {
			continue
		}
		if only != "" {
			return "" // multiple models and none configured: refuse to guess
		}
		only = dir + string(os.PathSeparator) + e.Name()
	}
	return only
}

func sidecarMissingReason(binary, model string) string {
	switch {
	case binary == "" && model == "":
		return "llama-server and a model are not installed"
	case binary == "":
		return "llama-server is not installed"
	case !fileExists(binary):
		return "llama-server binary not found at " + binary
	case model == "":
		return "no model installed"
	case !fileExists(model):
		return "model not found at " + model
	}
	return ""
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
