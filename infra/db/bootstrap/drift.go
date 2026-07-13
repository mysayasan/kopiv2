package bootstrap

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
)

// Schema drift: the difference between what the entities SAY the schema is and what the
// database actually has.
//
// Only column NAMES were ever compared. So:
//
//   - a TYPE CHANGE (int64 -> string) was not detected at all. The column kept its old SQL
//     type and the row scanner failed at runtime, on a customer's box, with no clue why.
//   - a DROPPED field left its column in the database forever, invisible.
//
// Neither is auto-fixed here, and that is deliberate. Rewriting a column's type in place is
// destructive and engine-specific — it is exactly what a migration is for. What this does
// is make the drift LOUD, so the developer writes that migration instead of discovering it
// from a support ticket.

// DriftKind classifies a schema difference.
type DriftKind string

const (
	// DriftTypeChanged: the column exists but its type no longer matches the entity.
	DriftTypeChanged DriftKind = "type_changed"
	// DriftExtraColumn: the database has a column the entity no longer declares — a field
	// that was renamed or removed without a migration. Its data is still there.
	DriftExtraColumn DriftKind = "extra_column"
)

// SchemaDrift is one detected difference.
type SchemaDrift struct {
	Kind     DriftKind `json:"kind"`
	Table    string    `json:"table"`
	Column   string    `json:"column"`
	Expected string    `json:"expected,omitempty"`
	Actual   string    `json:"actual,omitempty"`
}

func (d SchemaDrift) String() string {
	switch d.Kind {
	case DriftTypeChanged:
		return fmt.Sprintf("%s.%s type is %s but the entity declares %s — the row scanner will fail; write a migration to change it",
			d.Table, d.Column, d.Actual, d.Expected)
	case DriftExtraColumn:
		return fmt.Sprintf("%s.%s exists in the database but not in the entity — a renamed or removed field; its data is still there. Write a migration to drop or rename it",
			d.Table, d.Column)
	default:
		return fmt.Sprintf("%s.%s: %s", d.Table, d.Column, d.Kind)
	}
}

// columnInfo is one column as the database actually has it.
type columnInfo struct {
	Name     string
	DataType string
}

// typeFamily reduces an SQL type to the broad class that actually matters for whether a
// row scan will work.
//
// Comparing exact type strings across three engines is hopeless — "TEXT" declared on
// MariaDB comes back as "text", on Postgres a BOOLEAN is "boolean" but on SQLite it is
// whatever you wrote, and BIGINT is "int8" on Postgres. Comparing families catches the
// drift that breaks things (a string column where the entity now expects an int) without
// crying wolf over VARCHAR vs TEXT.
func typeFamily(sqlType string) string {
	t := strings.ToLower(strings.TrimSpace(sqlType))
	// Strip any length/precision: varchar(255) -> varchar.
	if i := strings.IndexByte(t, '('); i >= 0 {
		t = t[:i]
	}
	t = strings.TrimSpace(t)

	switch {
	case t == "":
		return ""
	case strings.Contains(t, "bool"):
		return "bool"
	case strings.Contains(t, "int"): // int, integer, bigint, smallint, int8, tinyint
		return "int"
	case strings.Contains(t, "char"), strings.Contains(t, "text"), strings.Contains(t, "clob"):
		return "text"
	case strings.Contains(t, "real"), strings.Contains(t, "float"), strings.Contains(t, "double"),
		strings.Contains(t, "numeric"), strings.Contains(t, "decimal"):
		return "float"
	case strings.Contains(t, "timestamp"), strings.Contains(t, "datetime"), strings.Contains(t, "date"):
		return "time"
	case strings.Contains(t, "blob"), strings.Contains(t, "bytea"):
		return "blob"
	case strings.Contains(t, "json"):
		return "json"
	default:
		return t
	}
}

// detectDrift compares a table's declared columns against the database's actual ones.
//
// It never modifies anything. A caller that wants the schema fixed writes a Migration.
func detectDrift(ctx context.Context, db *sql.DB, engine string, table TableSpec) ([]SchemaDrift, error) {
	actual, err := describeColumns(ctx, db, engine, table.Name)
	if err != nil {
		return nil, err
	}

	drift := []SchemaDrift{}
	declared := map[string]struct{}{}

	for _, column := range table.Columns {
		declared[column.Name] = struct{}{}
		got, ok := actual[column.Name]
		if !ok {
			// Missing entirely — the additive auto-migrator adds it. Not drift.
			continue
		}

		// Compare against the type this engine ACTUALLY GETS, not the engine-neutral one in
		// the manifest. The two are frequently different, and comparing the wrong one is a
		// false-positive machine: SQLite stores a BOOLEAN as INTEGER and MariaDB as
		// TINYINT(1), so a bool column would be reported as drift on every boot on two of
		// the three engines. A warning that fires constantly is a warning nobody reads.
		want := typeFamily(normalizeSQLType(column.SQLType, engine))
		have := typeFamily(got.DataType)

		// An auto-increment primary key is rewritten wholesale by columnDefinition
		// (BIGSERIAL / INTEGER PRIMARY KEY AUTOINCREMENT / BIGINT AUTO_INCREMENT), so its
		// declared type says nothing useful about what is on disk.
		if column.AutoInc && column.PrimaryKey {
			continue
		}
		// SQLite is dynamically typed and permits a column with no declared type at all, so
		// an empty family says nothing. Don't manufacture drift out of it.
		if want == "" || have == "" || want == have {
			continue
		}
		drift = append(drift, SchemaDrift{
			Kind:     DriftTypeChanged,
			Table:    table.Name,
			Column:   column.Name,
			Expected: normalizeSQLType(column.SQLType, engine),
			Actual:   got.DataType,
		})
	}

	for name := range actual {
		if _, ok := declared[name]; !ok {
			drift = append(drift, SchemaDrift{Kind: DriftExtraColumn, Table: table.Name, Column: name})
		}
	}

	sort.Slice(drift, func(i, j int) bool {
		if drift[i].Column != drift[j].Column {
			return drift[i].Column < drift[j].Column
		}
		return drift[i].Kind < drift[j].Kind
	})
	return drift, nil
}

// describeColumns returns the table's columns as the database actually has them.
func describeColumns(ctx context.Context, db *sql.DB, engine string, tableName string) (map[string]columnInfo, error) {
	if engine == "sqlite" {
		return describeSQLiteColumns(ctx, db, tableName)
	}

	query := `
SELECT column_name, data_type
FROM information_schema.columns
WHERE table_schema = 'public' AND table_name = $1
`
	args := []any{tableName}
	if engine == "mariadb" {
		query = `
SELECT column_name, data_type
FROM information_schema.columns
WHERE table_schema = DATABASE() AND table_name = ?
`
	}

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	columns := map[string]columnInfo{}
	for rows.Next() {
		var name, dataType string
		if err := rows.Scan(&name, &dataType); err != nil {
			return nil, err
		}
		columns[name] = columnInfo{Name: name, DataType: dataType}
	}
	return columns, rows.Err()
}

func describeSQLiteColumns(ctx context.Context, db *sql.DB, tableName string) (map[string]columnInfo, error) {
	rows, err := db.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%s)", quoteIdent(tableName, "sqlite")))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	columns := map[string]columnInfo{}
	for rows.Next() {
		var cid int
		var name, dataType string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &pk); err != nil {
			return nil, err
		}
		columns[name] = columnInfo{Name: name, DataType: dataType}
	}
	return columns, rows.Err()
}
