package services

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBrowseDirectoryListsWithinAllowedRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "certs"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "cert.pem"), []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".hidden"), []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	res, err := BrowseDirectory(root, []string{root})
	if err != nil {
		t.Fatalf("BrowseDirectory: %v", err)
	}
	names := map[string]bool{}
	for _, e := range res.Entries {
		names[e.Name] = true
	}
	if !names["certs"] || !names["cert.pem"] {
		t.Fatalf("expected certs/ and cert.pem in listing, got %v", res.Entries)
	}
	if names[".hidden"] {
		t.Fatal("dotfiles must be hidden from the listing")
	}
	// Directories sort before files.
	if len(res.Entries) < 2 || !res.Entries[0].Dir {
		t.Fatalf("directories should sort first, got %+v", res.Entries)
	}
}

func TestBrowseWhitelistGuard(t *testing.T) {
	// withinAllowed is the sandbox gate. Test it directly with controlled roots so the
	// result doesn't depend on the OS-derived roots BrowseDirectory also adds.
	allowed := t.TempDir()
	roots := []string{allowed}

	if _, ok := withinAllowed(filepath.Join(allowed, "certs"), roots); !ok {
		t.Fatal("a path nested under an allowed root must be allowed")
	}
	if _, ok := withinAllowed(allowed, roots); !ok {
		t.Fatal("the allowed root itself must be allowed")
	}
	// The parent of the allowed root is NOT under the whitelist and must be rejected.
	if _, ok := withinAllowed(filepath.Dir(allowed), roots); ok {
		t.Fatal("a path above the allowed root must be rejected")
	}
	// A sibling/unrelated path must be rejected, and allowedParent must not leak it.
	if p := allowedParent(allowed, roots); p != "" {
		t.Fatalf("navigating up from an allowed root must stay in-sandbox, got %q", p)
	}
}
