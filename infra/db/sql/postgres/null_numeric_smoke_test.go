package postgres

import (
	"context"
	"os"
	"testing"

	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// managedNodeRow is a slice of myseliasan's managed_node: an int64 column that the
// auto-migrator added later, so every row adopted before that upgrade holds NULL in it.
type managedNodeRow struct {
	Id            int64  `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true"`
	NodeId        string `json:"nodeId" form:"nodeId" query:"nodeId"`
	Version       string `json:"version" form:"version" query:"version"`
	VersionSeenAt int64  `json:"versionSeenAt" form:"versionSeenAt" query:"versionSeenAt"`
}

// TestSmokeSelectSurvivesNullNumericColumn is the live regression test for
//
//	select list failed: sql: Scan error on column index 8, name "version_seen_at":
//	converting NULL to int64 is unsupported
//
// It has to run against postgres to mean anything: the sqlite driver scans into
// interface{} and maps nil to the zero value, so every unit test in the suite passed
// while this took the fleet's node list down on a real upgrade.
//
// Point it at a database whose managed_node still has NULLs (this asserts nothing about
// how many rows come back — a table with no NULL left is a passing but empty test).
func TestSmokeSelectSurvivesNullNumericColumn(t *testing.T) {
	if os.Getenv("KOPIV2_POSTGRES_SMOKE") != "1" {
		t.Skip("set KOPIV2_POSTGRES_SMOKE=1 to run against a local Postgres database")
	}

	crud, err := NewDbCrud(dbsql.DbConfigModel{
		Host:     getenvDefault("KOPIV2_POSTGRES_HOST", "localhost"),
		Port:     getenvIntDefault("KOPIV2_POSTGRES_PORT", 5433),
		User:     getenvDefault("KOPIV2_POSTGRES_USER", "postgres"),
		Password: getenvDefault("KOPIV2_POSTGRES_PASSWORD", "postgres"),
		DbName:   getenvDefault("KOPIV2_POSTGRES_DB", "myseliasandb"),
		SslMode:  getenvDefault("KOPIV2_POSTGRES_SSLMODE", "disable"),
	})
	if err != nil {
		t.Fatalf("NewDbCrud failed: %v", err)
	}

	rows, _, err := crud.Select(context.Background(), managedNodeRow{}, 0, 0, nil, nil, "managed_node")
	if err != nil {
		t.Fatalf("Select managed_node failed: %v", err)
	}
	for _, row := range rows {
		seen, ok := row["VersionSeenAt"].(*int64)
		if !ok {
			t.Fatalf("VersionSeenAt scanned as %T, want *int64", row["VersionSeenAt"])
		}
		version, ok := row["Version"].(*string)
		if !ok {
			t.Fatalf("Version scanned as %T, want *string", row["Version"])
		}
		t.Logf("node %s: version=%q seenAt=%d", derefString(row["NodeId"]), *version, *seen)
	}
}

func derefString(value interface{}) string {
	if str, ok := value.(*string); ok && str != nil {
		return *str
	}
	return ""
}
