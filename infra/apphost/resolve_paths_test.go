package apphost

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/infra/db/bootstrap"
)

// stubApp is a minimal App used to exercise path resolution, which only reads
// Name() and BaseDir().
type stubApp struct {
	name    string
	baseDir string
}

func (s stubApp) Name() string    { return s.name }
func (s stubApp) BaseDir() string { return s.baseDir }
func (s stubApp) Entities() []any { return nil }
func (s stubApp) Seeders(_ []string) []bootstrap.Seeder { return nil }
func (s stubApp) RegisterAppRoutes(_ *mux.Router, _ Dependencies) (ShutdownFunc, error) {
	return nil, nil
}

func TestResolveWritablePathAbsolutePassesThrough(t *testing.T) {
	abs := filepath.Join(t.TempDir(), "abs.db")
	if got := ResolveWritablePath("ignored", abs); got != abs {
		t.Fatalf("absolute path should pass through unchanged: got %q want %q", got, abs)
	}
	if got := ResolveWritablePath("ignored", ""); got != "" {
		t.Fatalf("empty path should pass through unchanged: got %q", got)
	}
}

func TestResolveWritablePathPrefersDataDirWhenTargetExists(t *testing.T) {
	dataDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dataDir, "data"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "data", "app.db"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	want := filepath.Clean(filepath.Join(dataDir, "data", "app.db"))
	if got := ResolveWritablePath(dataDir, filepath.Join("data", "app.db")); got != want {
		t.Fatalf("existing data-dir target should win: got %q want %q", got, want)
	}
}

func TestResolveWritablePathFallsBackToLegacyCWD(t *testing.T) {
	// dataDir target is absent, but a legacy copy exists relative to the working
	// directory: the legacy location must be honoured so upgrades don't orphan it.
	cwd := t.TempDir()
	t.Chdir(cwd)
	if err := os.MkdirAll(filepath.Join(cwd, "secret"), 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(cwd, "secret", "atrest.key")
	if err := os.WriteFile(legacy, []byte("k"), 0o600); err != nil {
		t.Fatal(err)
	}

	dataDir := t.TempDir() // separate, empty data dir (target does not exist)
	got := ResolveWritablePath(dataDir, filepath.Join("secret", "atrest.key"))
	want := filepath.Clean(filepath.Join("secret", "atrest.key"))
	if got != want {
		t.Fatalf("should fall back to legacy CWD path: got %q want %q", got, want)
	}
}

func TestResolveWritablePathDefaultsToDataDirWhenNeitherExists(t *testing.T) {
	t.Chdir(t.TempDir()) // clean CWD with no legacy copy
	dataDir := t.TempDir()
	want := filepath.Clean(filepath.Join(dataDir, "recordings"))
	if got := ResolveWritablePath(dataDir, "recordings"); got != want {
		t.Fatalf("should default to data dir: got %q want %q", got, want)
	}
}

func TestEnvOverrideAppScopedThenGeneric(t *testing.T) {
	app := stubApp{name: "mymatasan", baseDir: filepath.Join("apps", "mymatasan")}

	t.Setenv("MYMATASAN_DATA", "/app/scoped")
	t.Setenv("KOPIV2_DATA", "/generic")
	if got := envOverride(app, "DATA"); got != "/app/scoped" {
		t.Fatalf("app-scoped override should win: got %q", got)
	}

	os.Unsetenv("MYMATASAN_DATA")
	if got := envOverride(app, "DATA"); got != "/generic" {
		t.Fatalf("generic override should apply when app-scoped unset: got %q", got)
	}
}

func TestResolveDataDirDefaultsToHomeDir(t *testing.T) {
	app := stubApp{name: "mymatasan", baseDir: filepath.Join("apps", "mymatasan")}
	os.Unsetenv("MYMATASAN_DATA")
	os.Unsetenv("KOPIV2_DATA")
	if got := resolveDataDir(app, "/opt/app/home"); got != "/opt/app/home" {
		t.Fatalf("data dir should default to home dir: got %q", got)
	}

	t.Setenv("MYMATASAN_DATA", "/var/lib/mymatasan")
	if got := resolveDataDir(app, "/opt/app/home"); got != filepath.Clean("/var/lib/mymatasan") {
		t.Fatalf("env override should win: got %q", got)
	}
}

func TestResolveHomeDirUsesBaseDirWhenPresent(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	baseRel := filepath.Join("apps", "mymatasan")
	if err := os.MkdirAll(filepath.Join(root, baseRel), 0o755); err != nil {
		t.Fatal(err)
	}
	app := stubApp{name: "mymatasan", baseDir: baseRel}
	os.Unsetenv("MYMATASAN_HOME")
	os.Unsetenv("KOPIV2_HOME")
	if got := resolveHomeDir(app); got != filepath.Clean(baseRel) {
		t.Fatalf("dev checkout should resolve home to BaseDir: got %q", got)
	}

	t.Setenv("MYMATASAN_HOME", "/usr/lib/mymatasan")
	if got := resolveHomeDir(app); got != filepath.Clean("/usr/lib/mymatasan") {
		t.Fatalf("env override should win: got %q", got)
	}
}

func TestLoadConfigSeedsDataDirFromHome(t *testing.T) {
	home := t.TempDir()
	data := t.TempDir()
	// Minimal but valid config the loader accepts.
	cfg := `{"jwt":{"secret":"seed-secret-value-1234567890"},"db":{"engine":"sqlite","db_name":":memory:"}}`
	if err := os.WriteFile(filepath.Join(home, "config.json"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := loadConfig(home, data); err != nil {
		t.Fatalf("loadConfig should seed and load: %v", err)
	}
	if _, err := os.Stat(filepath.Join(data, "config.json")); err != nil {
		t.Fatalf("config should have been seeded into data dir: %v", err)
	}
}
