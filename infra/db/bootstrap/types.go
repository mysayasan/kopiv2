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
	Seeders   []Seeder
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
}

// Seeder seeds initial application data.
type Seeder interface {
	Name() string
	Seed(ctx context.Context, db *sql.DB) error
}
