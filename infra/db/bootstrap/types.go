package bootstrap

import (
	"context"
	"database/sql"

	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// BootstrapConfig controls automatic provisioning behavior.
type BootstrapConfig struct {
	Enabled            bool
	AutoCreateDatabase bool
	AutoCreateSchema   bool
	AutoMigrate        bool
	AutoSeed           bool
	AllowReset         bool
	SetupPath          string
	SeedStatements     []string
}

// Options configures the bootstrap engine.
type Options struct {
	AppName   string
	Config    dbsql.DbConfigModel
	Bootstrap BootstrapConfig
	Entities  []any
	// Migrations are the ordered, once-only schema changes the auto-migrator cannot
	// express: renames, drops, type changes, data transforms. Additive changes need NO
	// migration — add the field to the entity and auto-migrate picks it up.
	//
	// They run BEFORE auto-migrate (a rename must happen while the column still has its
	// old name), and a fresh database BASELINES them: the schema was just created at the
	// current shape, so there is nothing for them to do. See infra/db/bootstrap/migration.go.
	Migrations []Migration
	Seeders    []Seeder
}

// Status reports the result of a bootstrap run.
type Status struct {
	AppName         string `json:"appName"`
	DatabaseName    string `json:"databaseName"`
	DatabaseCreated bool   `json:"databaseCreated"`
	SchemaCreated   bool   `json:"schemaCreated"`
	SchemaUpdated   bool   `json:"schemaUpdated"`
	DriftDetected   bool   `json:"driftDetected"`
	Seeded          bool   `json:"seeded"`
	Ready           bool   `json:"ready"`
	ManifestHash    string `json:"manifestHash"`
	Message         string `json:"message"`

	// MigrationsApplied lists the migration IDs this run executed (empty on a fresh
	// database, which baselines them instead).
	MigrationsApplied []string `json:"migrationsApplied,omitempty"`
	// SchemaDrift lists differences between the entities and the database that the
	// additive auto-migrator CANNOT fix: changed column types, and columns the entities no
	// longer declare. They are reported, never auto-repaired — rewriting a column in place
	// is destructive and engine-specific, which is what a Migration is for. Non-empty means
	// somebody needs to write one.
	SchemaDrift []SchemaDrift `json:"schemaDrift,omitempty"`
}

// Seeder seeds initial application data.
type Seeder interface {
	Name() string
	Seed(ctx context.Context, db *sql.DB) error
}
