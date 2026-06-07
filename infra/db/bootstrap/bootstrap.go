package bootstrap

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

const bootstrapStateTable = "bootstrap_schema_state"

// Ensure provisions the database and schema if needed.
func Ensure(ctx context.Context, opts Options) (*Status, error) {
	bootstrapConfig := normalizeBootstrapConfig(opts.Bootstrap)
	status := &Status{AppName: opts.AppName, DatabaseName: opts.Config.DbName}
	engine := normalizeDbEngine(opts.Config.Engine)
	if engine != "postgres" && engine != "mariadb" {
		return nil, fmt.Errorf("bootstrap currently supports only postgres and mariadb, got %q", engine)
	}

	if !bootstrapConfig.Enabled {
		status.Ready = true
		status.Message = "bootstrap disabled"
		return status, nil
	}

	databaseCreated, err := ensureDatabase(ctx, opts.Config, engine, bootstrapConfig.AutoCreateDatabase)
	if err != nil {
		return nil, err
	}
	status.DatabaseCreated = databaseCreated

	db, err := openTargetDB(opts.Config, engine)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	if err := db.PingContext(ctx); err != nil {
		return nil, err
	}

	if err := ensureBootstrapStateTable(ctx, db, engine); err != nil {
		return nil, err
	}

	manifest, manifestHash, err := BuildManifest(opts.AppName, opts.Entities)
	if err != nil {
		return nil, err
	}
	status.ManifestHash = manifestHash

	previousHash, err := loadManifestHash(ctx, db, engine, opts.AppName)
	if err != nil {
		return nil, err
	}
	if previousHash != "" && previousHash != manifestHash {
		status.DriftDetected = true
	}

	tablesCreated, schemaUpdated, err := ensureSchema(ctx, db, engine, manifest, bootstrapConfig.AutoCreateSchema, bootstrapConfig.AutoMigrate)
	if err != nil {
		return nil, err
	}
	status.SchemaCreated = tablesCreated
	status.SchemaUpdated = schemaUpdated

	if bootstrapConfig.AutoSeed && len(opts.Seeders) > 0 {
		seeded, err := runSeeders(ctx, db, opts.Seeders)
		if err != nil {
			return nil, err
		}
		status.Seeded = seeded
	}

	if err := saveManifest(ctx, db, engine, opts.AppName, manifest, manifestHash); err != nil {
		return nil, err
	}

	status.Ready = true
	status.Message = "bootstrap complete"
	if status.DriftDetected {
		status.Message = "schema drift reconciled with additive updates"
	}
	return status, nil
}

func normalizeBootstrapConfig(cfg BootstrapConfig) BootstrapConfig {
	if cfg.SetupPath == "" {
		cfg.SetupPath = "/setup"
	}
	return cfg
}

func ensureDatabase(ctx context.Context, cfg dbsql.DbConfigModel, engine string, allowCreate bool) (bool, error) {
	exists, err := databaseExists(ctx, cfg, engine)
	if err != nil {
		return false, err
	}
	if exists {
		return false, nil
	}
	if !allowCreate {
		return false, fmt.Errorf("database %s does not exist", cfg.DbName)
	}
	if err := createDatabase(ctx, cfg, engine); err != nil {
		return false, err
	}
	return true, nil
}

func databaseExists(ctx context.Context, cfg dbsql.DbConfigModel, engine string) (bool, error) {
	adminDB, err := openMaintenanceDB(cfg, engine)
	if err != nil {
		return false, err
	}
	defer adminDB.Close()

	var exists bool
	switch engine {
	case "postgres":
		err = adminDB.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)`, cfg.DbName).Scan(&exists)
	case "mariadb":
		err = adminDB.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM information_schema.schemata WHERE schema_name = ?)`, cfg.DbName).Scan(&exists)
	default:
		return false, fmt.Errorf("unsupported db engine %q", engine)
	}
	return exists, err
}

func createDatabase(ctx context.Context, cfg dbsql.DbConfigModel, engine string) error {
	adminDB, err := openMaintenanceDB(cfg, engine)
	if err != nil {
		return err
	}
	defer adminDB.Close()

	switch engine {
	case "postgres":
		if _, err := adminDB.ExecContext(ctx, fmt.Sprintf("CREATE DATABASE %s", quoteIdent(cfg.DbName, engine))); err != nil {
			return err
		}
	case "mariadb":
		if _, err := adminDB.ExecContext(ctx, fmt.Sprintf("CREATE DATABASE %s CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci", quoteIdent(cfg.DbName, engine))); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported db engine %q", engine)
	}

	return nil
}

func openMaintenanceDB(cfg dbsql.DbConfigModel, engine string) (*sql.DB, error) {
	dbName := "postgres"
	if engine == "mariadb" {
		dbName = ""
	}
	driver, dsn := buildDSN(cfg, dbName, engine)
	return sql.Open(driver, dsn)
}

func openTargetDB(cfg dbsql.DbConfigModel, engine string) (*sql.DB, error) {
	driver, dsn := buildDSN(cfg, cfg.DbName, engine)
	return sql.Open(driver, dsn)
}

func buildDSN(cfg dbsql.DbConfigModel, databaseName string, engine string) (string, string) {
	switch engine {
	case "postgres":
		return "postgres", fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s", cfg.Host, cfg.Port, cfg.User, cfg.Password, databaseName, cfg.SslMode)
	case "mariadb":
		if databaseName == "" {
			return "mysql", fmt.Sprintf("%s:%s@tcp(%s:%d)/?parseTime=true&multiStatements=true", cfg.User, cfg.Password, cfg.Host, cfg.Port)
		}
		return "mysql", fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?parseTime=true&multiStatements=true", cfg.User, cfg.Password, cfg.Host, cfg.Port, databaseName)
	default:
		return "", ""
	}
}

func normalizeDbEngine(engine string) string {
	value := strings.TrimSpace(strings.ToLower(engine))
	if value == "" {
		return "postgres"
	}
	return value
}

func ensureBootstrapStateTable(ctx context.Context, db *sql.DB, engine string) error {
	var stmt string
	switch engine {
	case "postgres":
		stmt = fmt.Sprintf(`
CREATE TABLE IF NOT EXISTS %s (
	app_name TEXT PRIMARY KEY,
	manifest_hash TEXT NOT NULL,
	manifest_json JSONB NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
)
`, quoteIdent(bootstrapStateTable, engine))
	case "mariadb":
		stmt = fmt.Sprintf(`
CREATE TABLE IF NOT EXISTS %s (
	app_name VARCHAR(255) PRIMARY KEY,
	manifest_hash VARCHAR(255) NOT NULL,
	manifest_json JSON NOT NULL,
	updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
)
`, quoteIdent(bootstrapStateTable, engine))
	default:
		return fmt.Errorf("unsupported db engine %q", engine)
	}

	_, err := db.ExecContext(ctx, stmt)
	return err
}

func loadManifestHash(ctx context.Context, db *sql.DB, engine string, appName string) (string, error) {
	var manifestHash string
	query := fmt.Sprintf(`SELECT manifest_hash FROM %s WHERE app_name = ?`, quoteIdent(bootstrapStateTable, engine))
	if engine == "postgres" {
		query = fmt.Sprintf(`SELECT manifest_hash FROM %s WHERE app_name = $1`, quoteIdent(bootstrapStateTable, engine))
	}

	err := db.QueryRowContext(ctx, query, appName).Scan(&manifestHash)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return manifestHash, err
}

func saveManifest(ctx context.Context, db *sql.DB, engine string, appName string, manifest Manifest, manifestHash string) error {
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		return err
	}

	switch engine {
	case "postgres":
		_, err = db.ExecContext(ctx, fmt.Sprintf(`
INSERT INTO %s (app_name, manifest_hash, manifest_json, updated_at)
VALUES ($1, $2, $3::jsonb, $4)
ON CONFLICT (app_name)
DO UPDATE SET manifest_hash = EXCLUDED.manifest_hash, manifest_json = EXCLUDED.manifest_json, updated_at = EXCLUDED.updated_at
`, quoteIdent(bootstrapStateTable, engine)), appName, manifestHash, string(manifestBytes), time.Now().UTC())
	case "mariadb":
		_, err = db.ExecContext(ctx, fmt.Sprintf(`
INSERT INTO %s (app_name, manifest_hash, manifest_json, updated_at)
VALUES (?, ?, ?, ?)
ON DUPLICATE KEY UPDATE manifest_hash = VALUES(manifest_hash), manifest_json = VALUES(manifest_json), updated_at = VALUES(updated_at)
`, quoteIdent(bootstrapStateTable, engine)), appName, manifestHash, string(manifestBytes), time.Now().UTC())
	default:
		return fmt.Errorf("unsupported db engine %q", engine)
	}

	return err
}

func ensureSchema(ctx context.Context, db *sql.DB, engine string, manifest Manifest, allowCreate bool, allowMigrate bool) (bool, bool, error) {
	tablesCreated := false
	tablesUpdated := false

	for _, table := range manifest.Tables {
		exists, err := tableExists(ctx, db, engine, table.Name)
		if err != nil {
			return false, false, err
		}
		if !exists {
			if !allowCreate {
				return false, false, fmt.Errorf("table %s does not exist", table.Name)
			}
			if err := createTable(ctx, db, engine, table); err != nil {
				return false, false, err
			}
			tablesCreated = true
			tablesUpdated = true
			continue
		}
		if allowMigrate {
			updated, err := migrateTable(ctx, db, engine, table)
			if err != nil {
				return false, false, err
			}
			if updated {
				tablesUpdated = true
			}
		}
		if err := ensureUniqueIndexes(ctx, db, engine, table); err != nil {
			return false, false, err
		}
	}

	return tablesCreated, tablesUpdated, nil
}

func tableExists(ctx context.Context, db *sql.DB, engine string, tableName string) (bool, error) {
	var exists bool
	var err error
	switch engine {
	case "postgres":
		err = db.QueryRowContext(ctx, `SELECT to_regclass($1) IS NOT NULL`, tableName).Scan(&exists)
	case "mariadb":
		err = db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = ?)`, tableName).Scan(&exists)
	default:
		return false, fmt.Errorf("unsupported db engine %q", engine)
	}
	return exists, err
}

func createTable(ctx context.Context, db *sql.DB, engine string, table TableSpec) error {
	_, err := db.ExecContext(ctx, fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (%s)", quoteIdent(table.Name, engine), tableDefinition(table, engine)))
	if err != nil {
		return err
	}
	return ensureUniqueIndexes(ctx, db, engine, table)
}

func migrateTable(ctx context.Context, db *sql.DB, engine string, table TableSpec) (bool, error) {
	existing, err := existingColumns(ctx, db, engine, table.Name)
	if err != nil {
		return false, err
	}
	updated := false
	for _, column := range table.Columns {
		if _, ok := existing[column.Name]; ok {
			continue
		}
		_, err := db.ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s", quoteIdent(table.Name, engine), columnDefinition(column, false, engine)))
		if err != nil {
			return false, err
		}
		updated = true
	}
	if err := ensureUniqueIndexes(ctx, db, engine, table); err != nil {
		return false, err
	}
	return updated, nil
}

func ensureUniqueIndexes(ctx context.Context, db *sql.DB, engine string, table TableSpec) error {
	existing := map[string]struct{}{}
	if engine == "mariadb" {
		indexes, err := existingIndexes(ctx, db, table.Name)
		if err != nil {
			return err
		}
		existing = indexes
	}

	for _, uniqueIndex := range table.Unique {
		columns := make([]string, 0, len(uniqueIndex.Columns))
		for _, column := range uniqueIndex.Columns {
			columns = append(columns, quoteIdent(column, engine))
		}
		indexName := uniqueIndex.Name
		if indexName == "" {
			indexName = fmt.Sprintf("ux_%s_%s", table.Name, strings.Join(uniqueIndex.Columns, "_"))
		}

		if engine == "mariadb" {
			if _, ok := existing[indexName]; ok {
				continue
			}
		}

		query := fmt.Sprintf("CREATE UNIQUE INDEX IF NOT EXISTS %s ON %s (%s)", quoteIdent(indexName, engine), quoteIdent(table.Name, engine), strings.Join(columns, ", "))
		if engine == "mariadb" {
			query = fmt.Sprintf("CREATE UNIQUE INDEX %s ON %s (%s)", quoteIdent(indexName, engine), quoteIdent(table.Name, engine), strings.Join(columns, ", "))
		}

		_, err := db.ExecContext(ctx, query)
		if err != nil {
			return err
		}
	}
	return nil
}

func existingIndexes(ctx context.Context, db *sql.DB, tableName string) (map[string]struct{}, error) {
	rows, err := db.QueryContext(ctx, `
SELECT index_name
FROM information_schema.statistics
WHERE table_schema = DATABASE() AND table_name = ?
`, tableName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	indexes := make(map[string]struct{})
	for rows.Next() {
		var indexName string
		if err := rows.Scan(&indexName); err != nil {
			return nil, err
		}
		indexes[indexName] = struct{}{}
	}

	return indexes, rows.Err()
}

func runSeeders(ctx context.Context, db *sql.DB, seeders []Seeder) (bool, error) {
	if len(seeders) == 0 {
		return false, nil
	}
	sort.SliceStable(seeders, func(i, j int) bool { return seeders[i].Name() < seeders[j].Name() })
	for _, seeder := range seeders {
		if err := seeder.Seed(ctx, db); err != nil {
			return false, fmt.Errorf("seed %s failed: %w", seeder.Name(), err)
		}
	}
	return true, nil
}
