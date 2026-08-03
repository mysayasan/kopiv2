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

	"github.com/mysayasan/kopiv2/infra/config"
	"github.com/mysayasan/kopiv2/infra/db/bootstrap"
)

// ResetBootstrapOptions builds the bootstrap.Options a factory reset needs from the
// running app config, so each app wires the reset in a few lines instead of repeating
// the same field-by-field copy.
//
// Passing migrations is NOT optional even though the rebuilt database is brand new. A
// fresh database BASELINES them — it was just created at the current shape, so there is
// nothing for them to do. Omit them and the rebuild still writes a manifest, so the next
// boot reads as "not fresh" while the migration table is empty, and every migration is
// then replayed against an already-current schema and fails.
func ResetBootstrapOptions(
	appName string,
	cfg *config.AppConfigModel,
	entities []any,
	migrations []bootstrap.Migration,
	seeders []bootstrap.Seeder,
) bootstrap.Options {
	return bootstrap.Options{
		AppName: appName,
		Config:  cfg.Db,
		Bootstrap: bootstrap.BootstrapConfig{
			Enabled:            cfg.Bootstrap.Enabled,
			AutoCreateDatabase: cfg.Bootstrap.AutoCreateDatabase,
			AutoCreateSchema:   cfg.Bootstrap.AutoCreateSchema,
			AutoMigrate:        cfg.Bootstrap.AutoMigrate,
			AutoSeed:           cfg.Bootstrap.AutoSeed,
			AllowReset:         cfg.Bootstrap.AllowReset,
			SetupPath:          cfg.Bootstrap.SetupPath,
			SeedStatements:     cfg.Bootstrap.SeedStatements,
		},
		Entities:   entities,
		Migrations: migrations,
		Seeders:    seeders,
	}
}

// Factory reset for the control-plane style apps (myseliasan, myidsan, myiotsan).
//
// mymatasan has its own orchestrator because an NVR's wipe is dominated by footage: it
// shreds, TRIMs and scrubs free space through a multi-minute time budget. These three
// hold almost nothing on disk by comparison — their state is the database, plus a
// handful of uploaded files and the at-rest key — so the honest implementation is the
// short one: stop background work, destroy the key, unlink the data directories, drop
// and rebuild the database, restart. No scrub stage is claimed, because there is no
// large media library whose residual blocks would justify one.
//
// The JSON progress contract deliberately matches mymatasan's so the SPA overlay
// behaves identically across the suite.

// ProcessRestarter gracefully relaunches the running process. apphost.Restarter
// satisfies it; kept local so this package does not depend on the host wiring.
type ProcessRestarter interface {
	Restart(reason string)
}

// CryptoEraser destroys the at-rest encryption key, instantly making everything sealed
// with it unrecoverable. atrest.KeyStore satisfies it.
type CryptoEraser interface {
	Destroy() error
}

// ResetStage is the current phase of a factory reset.
type ResetStage string

const (
	ResetIdle       ResetStage = "idle"
	ResetErasing    ResetStage = "erasing"   // destroy the key, unlink app data
	ResetWipingDB   ResetStage = "wiping_db" // drop the database, rebuild and reseed
	ResetRestarting ResetStage = "restarting"
	ResetFailed     ResetStage = "failed"
)

// ResetProgress is the in-memory status of a factory reset. It is deliberately not
// persisted: the database is dropped mid-flight, so the UI polls this out of process
// memory until the server restarts under it.
type ResetProgress struct {
	Running bool       `json:"running"`
	Stage   ResetStage `json:"stage"`
	Percent int        `json:"percent"`
	Message string     `json:"message"`
	// Warning accumulates non-fatal problems. A reset is a destructive operation that
	// must always reach "restarting" -- a half-wiped install that never restarts is the
	// worst outcome -- so stage failures are advisory and the sequence pushes on.
	Warning   string `json:"warning,omitempty"`
	Error     string `json:"error,omitempty"`
	StartedAt int64  `json:"startedAt,omitempty"`
	UpdatedAt int64  `json:"updatedAt,omitempty"`
}

// SystemResetConfig wires the orchestrator.
type SystemResetConfig struct {
	// ConfirmPhrase is the exact text an operator must type to confirm, GitHub-style.
	// It is the app's own name, so muscle memory from one console cannot carry a
	// reflexive confirmation into another app's dialog. Required.
	ConfirmPhrase string
	// CollectDataPaths returns the directories whose CONTENTS are removed: uploads,
	// floor plans, cached basemaps and the like. Resolved at start time. It must never
	// include the working directory, the config file, the log directory or the TLS
	// certificates -- a reset restores factory state, it does not uninstall the app.
	CollectDataPaths func(ctx context.Context) []string
	BootstrapOpts    bootstrap.Options
	Restarter        ProcessRestarter
	// KeyStore (optional) crypto-erases the at-rest key first, so anything sealed with
	// it is unrecoverable immediately, whatever happens to the rest of the sequence.
	KeyStore CryptoEraser
	// StopServices (optional) stops background work holding files or sockets open --
	// pollers, control channels, schedulers -- before the wipe. Must be safe to call
	// once; normal shutdown may close the same things again.
	StopServices func()
	// CloseDatabase closes this process's own connection pool immediately before the
	// drop. REQUIRED for sqlite on Windows: the file cannot be deleted while this
	// process holds it open, so without this the wipe silently fails and the old data
	// survives a reset that reported success. For postgres/mariadb it stops our own
	// sessions blocking the DROP.
	CloseDatabase func() error
	// Logf (optional) records stage transitions to the app log, which is the only
	// durable trace: the audit trail lives in the database being dropped.
	Logf func(format string, args ...any)
}

// SystemResetService performs a destructive factory reset: erase app data, drop and
// rebuild the database, then restart into a clean first-run state. Runtime settings
// return to defaults naturally because they live in the dropped database.
type SystemResetService struct {
	cfg      SystemResetConfig
	mu       sync.Mutex
	progress ResetProgress
}

func NewSystemResetService(cfg SystemResetConfig) *SystemResetService {
	return &SystemResetService{cfg: cfg, progress: ResetProgress{Stage: ResetIdle}}
}

// Allowed reports whether factory reset is enabled (bootstrap.allowReset). It ships
// false on the identity and fleet apps, so the button is hidden until an operator
// deliberately turns it on in config.json.
func (s *SystemResetService) Allowed() bool {
	return s.cfg.BootstrapOpts.Bootstrap.AllowReset
}

// ConfirmPhrase is the text the operator must type. Exposed so the UI can render the
// instruction from one source of truth rather than hardcoding the app name twice.
func (s *SystemResetService) ConfirmPhrase() string {
	return s.cfg.ConfirmPhrase
}

// Progress returns a snapshot of the current status.
func (s *SystemResetService) Progress() ResetProgress {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.progress
}

// InProgress reports whether a reset is running. It stays true through the restarting
// stage: run() clears Running about a second before the process actually relaunches,
// but the database pool is already closed by then, so the request gate must keep
// shedding load until this process dies. A fresh process starts idle.
func (s *SystemResetService) InProgress() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.progress.Running || s.progress.Stage == ResetRestarting
}

// ErrConfirmMismatch is returned when the typed confirmation does not match.
var ErrConfirmMismatch = errors.New("confirmation text does not match")

// Start kicks off the reset in the background after checking the typed confirmation.
//
// The phrase is verified HERE and not only in the browser. The client-side dialog is a
// guard against a mis-click; this is the guard against anything else that can reach an
// authenticated endpoint -- a stray curl, a replayed request, a script pointed at the
// wrong host. A destructive endpoint that trusts the client to have asked is not
// actually confirmed.
func (s *SystemResetService) Start(confirm string) error {
	if !s.Allowed() {
		return errors.New("factory reset is disabled (set bootstrap.allowReset = true to enable)")
	}
	if strings.TrimSpace(confirm) != s.cfg.ConfirmPhrase {
		return ErrConfirmMismatch
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
	s.logf("reset: %s (%d%%) %s", stage, percent, msg)
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
	s.logf("reset warning: %s", msg)
}

func (s *SystemResetService) logf(format string, args ...any) {
	if s.cfg.Logf != nil {
		s.cfg.Logf(format, args...)
	}
}

func (s *SystemResetService) run() {
	ctx := context.Background()

	// Ordered so the irreversible work lands before anything slow or fallible, and so
	// killing the process partway cannot leave a half-wiped install that still serves
	// the old data: the key dies first, the files go next, the database drop follows,
	// and the restart is unconditional.

	if s.cfg.StopServices != nil {
		s.set(ResetErasing, 5, "Stopping background services…")
		s.cfg.StopServices()
	}

	// Crypto-erase first: destroying the key makes every sealed column and file
	// unrecoverable immediately, regardless of what the later stages manage to do.
	if s.cfg.KeyStore != nil {
		s.set(ResetErasing, 15, "Destroying encryption key…")
		if err := s.cfg.KeyStore.Destroy(); err != nil {
			s.warn(fmt.Sprintf("could not destroy the encryption key: %v", err))
		}
	}

	s.set(ResetErasing, 30, "Erasing stored files…")
	s.eraseData(s.collectRoots(ctx))

	s.set(ResetWipingDB, 60, "Wiping database & restoring factory defaults…")
	if s.cfg.CloseDatabase != nil {
		if err := s.cfg.CloseDatabase(); err != nil {
			s.warn(fmt.Sprintf("could not close the database before wiping it: %v", err))
		}
	}
	if _, err := bootstrap.Reset(ctx, s.cfg.BootstrapOpts); err != nil {
		// Restarting re-runs bootstrap, which can finish a rebuild a transient error
		// interrupted -- better than stopping here with the database already dropped.
		s.warn(fmt.Sprintf("database wipe reported an error (restarting to retry the rebuild): %v", err))
	}

	s.set(ResetRestarting, 100, "Restarting…")
	s.mu.Lock()
	s.progress.Running = false
	s.mu.Unlock()

	// Let the client read the final state before the server goes down under it.
	if s.cfg.Restarter != nil {
		time.AfterFunc(1500*time.Millisecond, func() { s.cfg.Restarter.Restart("factory reset") })
	} else {
		s.warn("no restarter configured; restart the process manually to complete the reset.")
	}
}

// collectRoots resolves the absolute directories to empty, dropping anything that would
// be catastrophic to delete. A reset must not be able to remove the app itself.
func (s *SystemResetService) collectRoots(ctx context.Context) []string {
	if s.cfg.CollectDataPaths == nil {
		return nil
	}
	wd, _ := os.Getwd()
	if wd != "" {
		wd, _ = filepath.Abs(wd)
	}
	seen := map[string]struct{}{}
	var roots []string
	for _, p := range s.cfg.CollectDataPaths(ctx) {
		p = strings.TrimSpace(p)
		if p == "" || p == "." || p == ".." {
			continue
		}
		abs, err := filepath.Abs(p)
		if err != nil {
			continue
		}
		// Refuse the working directory and any filesystem root: emptying either would
		// delete the binary, the config and the logs along with the data.
		if abs == wd || abs == filepath.Dir(abs) {
			s.warn(fmt.Sprintf("refusing to erase %q: it is the working directory or a filesystem root", abs))
			continue
		}
		if _, dup := seen[abs]; dup {
			continue
		}
		seen[abs] = struct{}{}
		roots = append(roots, abs)
	}
	return roots
}

// eraseData empties each root, leaving the (now empty) directory in place so the app
// does not have to recreate it on the next boot.
func (s *SystemResetService) eraseData(roots []string) {
	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			if !os.IsNotExist(err) {
				s.warn(fmt.Sprintf("could not read %q: %v", root, err))
			}
			continue
		}
		for _, e := range entries {
			target := filepath.Join(root, e.Name())
			if err := os.RemoveAll(target); err != nil {
				s.warn(fmt.Sprintf("could not remove %q: %v", target, err))
			}
		}
	}
}
