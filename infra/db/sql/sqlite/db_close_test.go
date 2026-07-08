package sqlite

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

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

	// While open, on Windows the file cannot be removed (the real reset bug).
	removeErr := os.Remove(path)
	t.Logf("remove while OPEN -> err=%v", removeErr)

	// Close via the new io.Closer method.
	closer, ok := crud.(io.Closer)
	if !ok {
		t.Fatalf("dbCrud does not implement io.Closer")
	}
	if err := closer.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// After close, removal must succeed on every OS.
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove after CLOSE failed: %v", err)
	}
	t.Logf("remove after CLOSE -> success")
}
