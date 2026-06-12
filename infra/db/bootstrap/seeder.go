package bootstrap

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// SQLSeeder executes a list of SQL statements as initial data.
type SQLSeeder struct {
	name       string
	statements []string
}

// NewSQLSeeder creates a config-driven SQL seeder.
func NewSQLSeeder(name string, statements []string) Seeder {
	return &SQLSeeder{name: name, statements: statements}
}

func (m *SQLSeeder) Name() string {
	if m.name != "" {
		return m.name
	}
	return "sql"
}

func (m *SQLSeeder) Seed(ctx context.Context, db *sql.DB) error {
	for _, statement := range m.statements {
		statement = strings.TrimSpace(statement)
		if statement == "" {
			continue
		}
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("executing seed statement failed: %w", err)
		}
	}
	return nil
}
