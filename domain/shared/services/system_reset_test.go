package services

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/mysayasan/kopiv2/infra/db/bootstrap"
)

func allowingOpts(allow bool) bootstrap.Options {
	return bootstrap.Options{Bootstrap: bootstrap.BootstrapConfig{AllowReset: allow}}
}

func TestStartRefusesWhenResetDisabled(t *testing.T) {
	svc := NewSystemResetService(SystemResetConfig{ConfirmPhrase: "myseliasan", BootstrapOpts: allowingOpts(false)})
	if err := svc.Start("myseliasan"); err == nil {
		t.Fatal("a correct phrase must still be refused while bootstrap.allowReset is false")
	}
	if svc.Progress().Running {
		t.Fatal("a refused start must not mark the reset running")
	}
}

// The typed confirmation is enforced server-side, so a request that reaches the endpoint
// without it -- a stray curl, a replay, a script pointed at the wrong host -- is refused
// even though the browser dialog was never involved.
func TestStartRequiresTheExactPhrase(t *testing.T) {
	for _, given := range []string{"", "MYSELIASAN", "myselia", "myseliasan!", "my seliasan"} {
		svc := NewSystemResetService(SystemResetConfig{ConfirmPhrase: "myseliasan", BootstrapOpts: allowingOpts(true)})
		if err := svc.Start(given); err == nil {
			t.Fatalf("phrase %q should have been refused", given)
		} else if err != ErrConfirmMismatch {
			t.Fatalf("phrase %q gave %v, want ErrConfirmMismatch", given, err)
		}
		if svc.Progress().Running {
			t.Fatalf("phrase %q must not start a reset", given)
		}
	}
}

// Surrounding whitespace is forgiven -- an operator who copy-pastes the app name should
// not be told the word they can plainly see is wrong.
func TestStartAcceptsThePhraseWithSurroundingSpace(t *testing.T) {
	svc := NewSystemResetService(SystemResetConfig{
		ConfirmPhrase: "myidsan",
		BootstrapOpts: allowingOpts(true),
		// No restarter and no bootstrap connection: run() records warnings and finishes.
	})
	if err := svc.Start("  myidsan\n"); err != nil {
		t.Fatalf("Start: %v", err)
	}
}

func TestCollectRootsRefusesTheWorkingDirectory(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	keep := t.TempDir()
	svc := NewSystemResetService(SystemResetConfig{
		ConfirmPhrase:    "app",
		BootstrapOpts:    allowingOpts(true),
		CollectDataPaths: func(context.Context) []string { return []string{wd, keep, "", ".", ".."} },
	})
	roots := svc.collectRoots(context.Background())
	for _, r := range roots {
		if r == wd {
			t.Fatal("the working directory must never be selected for erasure")
		}
	}
	if len(roots) != 1 || roots[0] != keep {
		t.Fatalf("roots = %v, want only %q", roots, keep)
	}
}

func TestEraseDataEmptiesRootsButKeepsThem(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "sub", "deep")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	for _, f := range []string{filepath.Join(root, "a.bin"), filepath.Join(nested, "b.bin")} {
		if err := os.WriteFile(f, []byte("data"), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}

	svc := NewSystemResetService(SystemResetConfig{ConfirmPhrase: "app", BootstrapOpts: allowingOpts(true)})
	svc.eraseData([]string{root})

	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("the root directory itself must survive: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("root still holds %d entries, want it emptied", len(entries))
	}
}

// A missing directory is normal on a fresh install and must not be reported as a problem.
func TestEraseDataIgnoresMissingRoots(t *testing.T) {
	svc := NewSystemResetService(SystemResetConfig{ConfirmPhrase: "app", BootstrapOpts: allowingOpts(true)})
	svc.eraseData([]string{filepath.Join(t.TempDir(), "never-created")})
	if w := svc.Progress().Warning; w != "" {
		t.Fatalf("unexpected warning for a missing root: %q", w)
	}
}
