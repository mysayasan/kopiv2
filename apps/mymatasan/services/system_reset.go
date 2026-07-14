package services

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mysayasan/kopiv2/infra/db/bootstrap"
	"github.com/mysayasan/kopiv2/infra/recording"
)

// ProcessRestarter gracefully restarts the host process. apphost.Restarter
// satisfies it; kept as a local interface so this package doesn't depend on the
// host wiring.
type ProcessRestarter interface {
	Restart(reason string)
}

// CryptoEraser destroys the at-rest encryption key, instantly rendering all data
// encrypted with it unrecoverable (crypto-erase). atrest.KeyStore satisfies it; kept
// as a local interface so this package doesn't depend on the crypto module.
type CryptoEraser interface {
	Destroy() error
}

// shredTimeBudget caps how long the factory reset spends securely overwriting media.
// Past it, remaining files are unlinked without the multi-pass overwrite so the reset
// always completes and restarts rather than crawling through a large library on a
// near-full disk.
const shredTimeBudget = 2 * time.Minute

// ResetStage is the current phase of a factory reset.
type ResetStage string

const (
	ResetIdle       ResetStage = "idle"
	ResetErasing    ResetStage = "erasing"   // fast unlink of all media (content gone first)
	ResetWipingDB   ResetStage = "wiping_db" // drop DB + restore factory defaults
	ResetScrubbing  ResetStage = "scrubbing" // best-effort secure overwrite of freed space
	ResetRestarting ResetStage = "restarting"
	ResetFailed     ResetStage = "failed"
)

// ResetProgress is the in-memory status of a factory reset. It is deliberately not
// persisted — the database is dropped mid-flight, so the UI polls this from process
// memory until the server restarts.
type ResetProgress struct {
	Running bool       `json:"running"`
	Stage   ResetStage `json:"stage"`
	Percent int        `json:"percent"`
	Message string     `json:"message"`
	// Warning accumulates non-fatal problems (e.g. a file that couldn't be shredded,
	// or a database-wipe error). The reset pushes on regardless — it's a security
	// wipe that must always reach "restarted" — so these are advisory, not failures.
	Warning   string `json:"warning,omitempty"`
	Error     string `json:"error,omitempty"`
	StartedAt int64  `json:"startedAt,omitempty"`
	UpdatedAt int64  `json:"updatedAt,omitempty"`
}

// SystemResetConfig wires the factory-reset orchestrator.
type SystemResetConfig struct {
	// CollectMediaPaths returns the on-disk directories whose contents are wiped
	// (recordings, snapshots, training data, uploads). Resolved at start time so
	// per-camera recording paths are current. MUST NOT include the working dir,
	// database file, or log dir.
	CollectMediaPaths func(ctx context.Context) []string
	ShredPasses       int
	BootstrapOpts     bootstrap.Options
	Restarter         ProcessRestarter
	// KeyStore (optional) crypto-erases the at-rest encryption key as the first wipe
	// step, instantly making all encrypted data unrecoverable regardless of size.
	KeyStore CryptoEraser
	// StopServices stops everything holding camera streams open before the wipe — the
	// recording manager (whose ffmpeg processes lock the current .ts segments and keep
	// writing new ones), the detector worker, and the background monitors. Without it
	// the open segment can't be shredded/removed (left behind), new footage is written
	// mid-wipe, and the recorder competes for a near-full disk and stalls the shred.
	// It must be safe to call once; the app's normal shutdown may close the same things
	// again. Optional but strongly recommended.
	StopServices func()
	// CloseDatabase closes the app's own database connection pool right before the wipe
	// drops the database. This is REQUIRED for a sqlite database on Windows: the file
	// can't be deleted while this process still holds it open ("The process cannot access
	// the file because it is being used by another process"), so without it the DB wipe
	// silently fails and old data survives the reset. For postgres/mariadb it lets the
	// DROP proceed without the app's sessions blocking it. The pool is invalid afterwards,
	// but the reset restarts the process immediately. Optional but strongly recommended.
	CloseDatabase func() error
}

// SystemResetService performs a destructive factory reset: securely shred all media,
// drop and rebuild + reseed the database (honouring the configured engine), then
// restart the process into a clean first-run state. Runtime settings reset to
// defaults naturally because they live in the (now-dropped) database.
type SystemResetService struct {
	cfg      SystemResetConfig
	mu       sync.Mutex
	progress ResetProgress
}

func NewSystemResetService(cfg SystemResetConfig) *SystemResetService {
	return &SystemResetService{cfg: cfg, progress: ResetProgress{Stage: ResetIdle}}
}

// Allowed reports whether factory reset is enabled (bootstrap.allowReset).
func (s *SystemResetService) Allowed() bool {
	return s.cfg.BootstrapOpts.Bootstrap.AllowReset
}

// Progress returns a snapshot of the current reset status.
func (s *SystemResetService) Progress() ResetProgress {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.progress
}

// InProgress reports whether a factory reset is currently running. While it is, the app
// has closed its database pool and is finishing the (slow) free-space scrub before
// restarting, so DB-backed requests would fail — callers use this to shed load with a
// clean 503 instead of raw 500s.
func (s *SystemResetService) InProgress() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Stay "in progress" through the restarting stage too: run() flips Running to false
	// ~1.5s before the process actually relaunches, but the DB pool is already closed, so
	// the gate must keep shedding load until this process dies. A fresh process starts idle.
	return s.progress.Running || s.progress.Stage == ResetRestarting
}

// Start kicks off the reset in the background. It errors if reset is disabled or one
// is already running.
func (s *SystemResetService) Start() error {
	if !s.Allowed() {
		return errors.New("factory reset is disabled (set bootstrap.allowReset = true to enable)")
	}
	s.mu.Lock()
	if s.progress.Running {
		s.mu.Unlock()
		return errors.New("a reset is already in progress")
	}
	now := time.Now().Unix()
	s.progress = ResetProgress{Running: true, Stage: ResetErasing, Percent: 0, Message: "Starting…", StartedAt: now, UpdatedAt: now}
	s.mu.Unlock()
	go s.run()
	return nil
}

func (s *SystemResetService) set(stage ResetStage, percent int, msg string) {
	s.mu.Lock()
	s.progress.Stage = stage
	s.progress.Percent = percent
	s.progress.Message = msg
	s.progress.UpdatedAt = time.Now().Unix()
	s.mu.Unlock()
}

// warn records a non-fatal problem without halting the reset.
func (s *SystemResetService) warn(msg string) {
	s.mu.Lock()
	if s.progress.Warning == "" {
		s.progress.Warning = msg
	} else {
		s.progress.Warning += " " + msg
	}
	s.progress.UpdatedAt = time.Now().Unix()
	s.mu.Unlock()
}

func (s *SystemResetService) run() {
	ctx := context.Background()

	// This is a deliberate security wipe: every stage runs best-effort and the
	// sequence ALWAYS drives to a restart. Stage problems are recorded as warnings,
	// never aborts — and a restart re-runs bootstrap, so it can finish a rebuild that
	// a transient error interrupted rather than leaving the box half-wiped.
	//
	// Order is chosen so the irreversible, must-finish work survives a force-stop:
	//   1) stop services, 2) FAST-erase all media (unlink — content gone from the
	//   filesystem almost instantly), 3) drop+restore the database, 4) only THEN the
	//   slow best-effort secure overwrite (free-space scrub), 5) restart. Kill it
	//   anywhere after step 2 and the footage is already gone and the reset is done;
	//   only the residual-block scrub may be incomplete.

	// Stop recorders/monitors/detector FIRST so the recording ffmpeg releases its open
	// .ts segment (otherwise it's locked, left behind, and keeps writing during the
	// wipe) and stops competing for the disk.
	if s.cfg.StopServices != nil {
		s.set(ResetErasing, 1, "Stopping camera services…")
		s.cfg.StopServices()
	}

	// Crypto-erase FIRST: destroying the at-rest key makes every encrypted recording,
	// snapshot, and file instantly unrecoverable on any device/OS, regardless of size —
	// the real secure-wipe guarantee. The delete + TRIM + scrub that follow reclaim
	// space and scrub any plaintext residue (legacy files + the in-progress segment).
	if s.cfg.KeyStore != nil {
		s.set(ResetErasing, 2, "Destroying encryption key…")
		if err := s.cfg.KeyStore.Destroy(); err != nil {
			s.warn(fmt.Sprintf("could not destroy the encryption key: %v", err))
		}
	}

	roots := s.collectRoots(ctx)

	// 1. Fast erase: unlink everything immediately so no footage remains in the
	// filesystem even if the wipe is interrupted before the secure overwrite.
	s.eraseMedia(roots)

	// 2. Reset the database + factory defaults — guaranteed before the slow scrub.
	s.set(ResetWipingDB, 50, "Wiping database & restoring factory defaults…")
	// Close our own connection pool first so the drop can proceed. For sqlite on Windows
	// the file is otherwise locked by this process and can't be removed, so the wipe would
	// silently fail and old data would survive the reset.
	if s.cfg.CloseDatabase != nil {
		if err := s.cfg.CloseDatabase(); err != nil {
			s.warn(fmt.Sprintf("could not close the database before wiping it: %v", err))
		}
	}
	if _, err := bootstrap.Reset(ctx, s.cfg.BootstrapOpts); err != nil {
		s.warn(fmt.Sprintf("database wipe reported an error (restarting to retry the rebuild): %v", err))
	}

	// 3. Best-effort secure overwrite of the freed disk space (time-budgeted). The
	// files are already gone; this scrubs residual blocks and is safe to interrupt.
	s.scrubFreeSpace(roots)

	s.set(ResetRestarting, 100, "Restarting…")
	s.mu.Lock()
	s.progress.Running = false
	s.mu.Unlock()

	// Give the client a moment to read the final 100%/Restarting state before the
	// server goes down with the relaunch.
	if s.cfg.Restarter != nil {
		time.AfterFunc(1500*time.Millisecond, func() { s.cfg.Restarter.Restart("factory reset") })
	} else {
		s.warn("no restarter configured; restart the process manually to complete the reset.")
	}
}

// collectRoots resolves the absolute media directories to wipe, skipping the working
// directory so a reset can never delete the app itself.
func (s *SystemResetService) collectRoots(ctx context.Context) []string {
	seen := map[string]struct{}{}
	var roots []string
	if s.cfg.CollectMediaPaths != nil {
		for _, p := range s.cfg.CollectMediaPaths(ctx) {
			p = strings.TrimSpace(p)
			if p == "" || p == "." {
				continue
			}
			abs, err := filepath.Abs(p)
			if err != nil {
				abs = p
			}
			if _, ok := seen[abs]; ok {
				continue
			}
			seen[abs] = struct{}{}
			roots = append(roots, abs)
		}
	}
	return roots
}

// eraseMedia unlinks all media directories outright (no overwrite) — a fast metadata
// operation that gets the footage out of the filesystem near-instantly, so a
// force-stop after this point leaves nothing recoverable through the filesystem.
func (s *SystemResetService) eraseMedia(roots []string) {
	s.set(ResetErasing, 5, "Erasing recordings & media…")
	for i, root := range roots {
		_ = os.RemoveAll(root)
		pct := 5
		if len(roots) > 0 {
			pct = 5 + int(float64(i+1)/float64(len(roots))*40) // 5..45
		}
		s.set(ResetErasing, pct, "Erasing recordings & media…")
	}
	s.set(ResetErasing, 45, "Media erased.")
}

// scrubFreeSpace performs the device-appropriate secure overwrite of the just-freed
// space on each media volume: TRIM/discard (so SSD/NVMe controllers physically erase
// the freed cells) followed by a random-data free-space overwrite (so HDD residual
// blocks are scrubbed). Both are best-effort and time-budgeted — the files are already
// unlinked, so this stage is safe to interrupt. Each distinct volume is handled once.
func (s *SystemResetService) scrubFreeSpace(roots []string) {
	passes := s.cfg.ShredPasses
	if passes < 1 {
		passes = recording.DefaultShredPasses
	}
	deadline := time.Now().Add(shredTimeBudget)
	s.set(ResetScrubbing, 60, "Securely erasing free space (TRIM + overwrite)…")

	seen := map[string]struct{}{}
	for _, root := range roots {
		key := filepath.VolumeName(root)
		if key == "" {
			key = root // non-Windows: dedup per root path
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}

		// TRIM first — the correct, fast erase for flash. Bounded so a slow/unprivileged
		// trim can't stall the reset.
		trimCtx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		if err := recording.TrimVolume(trimCtx, root); err != nil {
			s.warn(fmt.Sprintf("TRIM on %s skipped: %v", root, err))
		}
		cancel()

		if time.Now().After(deadline) {
			s.warn("secure overwrite stopped at its time budget; freed space was not fully overwritten.")
			break
		}
		// Free-space overwrite for HDDs. The root was removed by eraseMedia; recreate a
		// directory on the same volume to write the fill file into, then remove it.
		if err := os.MkdirAll(root, 0o755); err != nil {
			continue
		}
		if _, err := recording.ScrubFreeSpace(root, passes, deadline, 0); err != nil {
			s.warn(fmt.Sprintf("free-space overwrite on %s: %v", root, err))
		}
		_ = os.RemoveAll(root)
	}
	s.set(ResetScrubbing, 95, "Secure erase complete.")
}
