package apis

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// A DECLARED ACTION THAT NOTHING EMITS IS DOCUMENTATION SHAPED LIKE EVIDENCE.
//
// This test is the grep that has already paid for itself twice in this suite, turned into something
// that fails a build. It found five audit constants on myidsan that no code path could ever write,
// and three alarms on THIS app that could never fire (#220) — in both cases the names were in the
// source, in the docs, and in nobody's database.
//
// The failure it prevents is specific and nasty: somebody opens the trail, filters on
// "credential.revoke" because the constant exists, gets nothing back, and concludes no badge was
// ever revoked. An absent record and a record of absence look identical in a query.
//
// It reads the SOURCE rather than reflecting, because a constant's whole problem is that it
// compiles perfectly whether or not anything uses it.
func TestEveryAuditActionHasAnEmitter(t *testing.T) {
	declFile := filepath.Join("..", "services", "audit.go")
	src, err := os.ReadFile(declFile)
	if err != nil {
		t.Fatalf("read %s: %v", declFile, err)
	}

	decl := regexp.MustCompile(`(?m)^\s*(Action[A-Za-z0-9]+)\s*=\s*"`)
	var actions []string
	for _, m := range decl.FindAllStringSubmatch(string(src), -1) {
		actions = append(actions, m[1])
	}
	if len(actions) < 10 {
		t.Fatalf("found only %d action constants in %s — the pattern has stopped matching, and a test that matches nothing passes everything", len(actions), declFile)
	}

	// Where an action can legitimately be emitted from: this package's handlers and the app's
	// composition root. Test files are excluded on purpose — a constant used only by its own test is
	// exactly the dead constant this is looking for.
	body := readGoSources(t, ".", filepath.Join("..", "app"))

	for _, name := range actions {
		if !strings.Contains(body, "services."+name) {
			t.Errorf("services.%s is declared and never emitted — either wire it to a handler or delete it; a filter that can never match is worse than a missing one, because it answers", name)
		}
	}
}

// The mirror: every action a handler emits is a declared constant, so the set stays closed and a UI
// filter can offer it. A hand-typed "grant.creaate" would be invisible to every query ever written.
func TestNoHandlerEmitsAnUndeclaredAction(t *testing.T) {
	body := readGoSources(t, ".", filepath.Join("..", "app"))
	src, err := os.ReadFile(filepath.Join("..", "services", "audit.go"))
	if err != nil {
		t.Fatalf("read the action declarations: %v", err)
	}
	declared := string(src)

	used := regexp.MustCompile(`services\.(Action[A-Za-z0-9]+)`).FindAllStringSubmatch(body, -1)
	for _, m := range used {
		if !strings.Contains(declared, m[1]+" ") && !strings.Contains(declared, m[1]+"=") {
			t.Errorf("%s is used but not declared in services/audit.go", m[1])
		}
	}
}

// readGoSources concatenates the non-test Go sources under the given directories.
func readGoSources(t *testing.T, dirs ...string) string {
	t.Helper()
	var b strings.Builder
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read dir %s: %v", dir, err)
		}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			data, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				t.Fatalf("read %s: %v", name, err)
			}
			b.Write(data)
		}
	}
	return b.String()
}
