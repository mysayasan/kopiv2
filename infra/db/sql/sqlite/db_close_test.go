package sqlite

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// TestCloseReleasesSqliteFileLock guards the factory-reset bug where the sqlite file
// stayed locked because the underlying handle was never closed.
//
// The probe has to differ by platform, and getting that wrong is why this test failed the
// first time it ever ran on Linux CI. Windows refuses to unlink a file that still has an
// open handle, so "can I delete it?" is a faithful proxy for "is the handle released?".
// POSIX unlink has no such rule — it detaches the name and lets the open handle live on —
// so on Linux the mid-test removal SUCCEEDS, the file is gone, and the original
// "after close, removal must succeed on every OS" assertion then failed on a file it had
// already deleted itself.
func TestCloseReleasesSqliteFileLock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lockcheck.db")
	crud, err := NewDbCrud(dbsql.DbConfigModel{Engine: "sqlite", DbName: path})
	if err != nil {
		t.Fatalf("NewDbCrud: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("db file not created: %v", err)
	}

	// Only meaningful on Windows: the open handle must keep the file undeletable, which is
	// the condition the reset path used to trip over.
	if runtime.GOOS == "windows" {
		if err := os.Remove(path); err == nil {
			t.Fatal("removed the database file while it was still open — the handle is not held, so this test can no longer detect the lock it exists to guard")
		}
	}

	closer, ok := crud.(io.Closer)
	if !ok {
		t.Fatalf("dbCrud does not implement io.Closer")
	}
	if err := closer.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// The real assertion on every OS: once closed, the file is replaceable. On POSIX the
	// name may already be gone above, so re-create it rather than asserting on a file this
	// test deleted itself.
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatalf("recreate for post-close check: %v", err)
		}
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove after CLOSE failed: %v", err)
	}
}
