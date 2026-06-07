package bootstrap

import (
	"testing"

	appmodels "github.com/mysayasan/kopiv2/apps/mymatasan/models"
	sharedentities "github.com/mysayasan/kopiv2/domain/entities"
)

func TestBuildManifest(t *testing.T) {
	manifest, hash, err := BuildManifest("mymatasan", []any{
		sharedentities.ApiEndpoint{},
		appmodels.ResidentProp{},
	})
	if err != nil {
		t.Fatalf("BuildManifest returned error: %v", err)
	}
	if hash == "" {
		t.Fatalf("expected manifest hash to be populated")
	}
	if manifest.AppName != "mymatasan" {
		t.Fatalf("expected app name mymatasan, got %s", manifest.AppName)
	}
	if len(manifest.Tables) != 2 {
		t.Fatalf("expected 2 tables, got %d", len(manifest.Tables))
	}

	var apiEndpoint TableSpec
	for _, table := range manifest.Tables {
		if table.Name == "api_endpoint" {
			apiEndpoint = table
		}
	}
	if apiEndpoint.Name != "api_endpoint" {
		t.Fatalf("expected api_endpoint table in manifest")
	}
	if len(apiEndpoint.Unique) != 1 {
		t.Fatalf("expected one unique index group for api_endpoint, got %d", len(apiEndpoint.Unique))
	}
	if len(apiEndpoint.Unique[0].Columns) != 3 {
		t.Fatalf("expected unique group to contain three columns, got %d", len(apiEndpoint.Unique[0].Columns))
	}
}

func TestBuildManifestKeepsEntityFieldOrder(t *testing.T) {
	manifest, _, err := BuildManifest("mymatasan", []any{
		sharedentities.ApiEndpoint{},
	})
	if err != nil {
		t.Fatalf("BuildManifest returned error: %v", err)
	}
	if len(manifest.Tables) != 1 {
		t.Fatalf("expected 1 table, got %d", len(manifest.Tables))
	}

	expected := []string{
		"id",
		"title",
		"description",
		"app_code",
		"host",
		"path",
		"access_tier",
		"is_active",
		"created_by",
		"created_at",
		"updated_by",
		"updated_at",
	}
	if len(manifest.Tables[0].Columns) != len(expected) {
		t.Fatalf("expected %d columns, got %d", len(expected), len(manifest.Tables[0].Columns))
	}
	for idx, column := range manifest.Tables[0].Columns {
		if column.Name != expected[idx] {
			t.Fatalf("expected column %d to be %s, got %s", idx, expected[idx], column.Name)
		}
	}
}

func TestBuildManifestCreatesApiLogAutoIncrementPrimaryKey(t *testing.T) {
	manifest, _, err := BuildManifest("mymatasan", []any{
		sharedentities.ApiLog{},
	})
	if err != nil {
		t.Fatalf("BuildManifest returned error: %v", err)
	}
	if len(manifest.Tables) != 1 {
		t.Fatalf("expected 1 table, got %d", len(manifest.Tables))
	}
	if len(manifest.Tables[0].Columns) == 0 {
		t.Fatalf("expected api_log columns")
	}

	id := manifest.Tables[0].Columns[0]
	if id.Name != "id" {
		t.Fatalf("expected first column to be id, got %s", id.Name)
	}
	if !id.PrimaryKey {
		t.Fatalf("expected api_log.id to be primary key")
	}
	if !id.AutoInc {
		t.Fatalf("expected api_log.id to auto increment")
	}
}

func TestBuildManifestIgnoresSliceFields(t *testing.T) {
	manifest, _, err := BuildManifest("mymatasan", []any{appmodels.ResidentProp{}})
	if err != nil {
		t.Fatalf("BuildManifest returned error: %v", err)
	}
	if len(manifest.Tables) != 1 {
		t.Fatalf("expected 1 table, got %d", len(manifest.Tables))
	}

	table := manifest.Tables[0]
	for _, column := range table.Columns {
		if column.Name == "pics" {
			t.Fatalf("expected slice field pics to be ignored")
		}
	}
}
