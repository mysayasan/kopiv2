package bootstrap

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"

	strcase "github.com/iancoleman/strcase"
)

type Manifest struct {
	AppName string      `json:"appName"`
	Tables  []TableSpec `json:"tables"`
}

type TableSpec struct {
	Name    string       `json:"name"`
	Type    string       `json:"type"`
	Columns []ColumnSpec `json:"columns"`
	Unique  []IndexSpec  `json:"unique"`
}

type ColumnSpec struct {
	Name        string `json:"name"`
	SQLType     string `json:"sqlType"`
	Nullable    bool   `json:"nullable"`
	PrimaryKey  bool   `json:"primaryKey"`
	AutoInc     bool   `json:"autoInc"`
	UniqueGroup string `json:"uniqueGroup,omitempty"`
}

type IndexSpec struct {
	Name    string   `json:"name"`
	Columns []string `json:"columns"`
}

func BuildManifest(appName string, items []any) (Manifest, string, error) {
	tables := make([]TableSpec, 0, len(items))
	seen := map[string]struct{}{}

	for _, item := range items {
		spec, err := buildTableSpec(item)
		if err != nil {
			return Manifest{}, "", err
		}
		if _, ok := seen[spec.Name]; ok {
			continue
		}
		seen[spec.Name] = struct{}{}
		tables = append(tables, spec)
	}

	sort.SliceStable(tables, func(i, j int) bool { return tables[i].Name < tables[j].Name })
	for idx := range tables {
		sort.SliceStable(tables[idx].Unique, func(i, j int) bool { return tables[idx].Unique[i].Name < tables[idx].Unique[j].Name })
	}

	manifest := Manifest{AppName: appName, Tables: tables}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		return Manifest{}, "", err
	}
	hash := sha256.Sum256(encoded)
	return manifest, hex.EncodeToString(hash[:]), nil
}

func buildTableSpec(item any) (TableSpec, error) {
	typeOf := reflect.TypeOf(item)
	if typeOf == nil {
		return TableSpec{}, fmt.Errorf("nil entity type")
	}
	if typeOf.Kind() == reflect.Pointer {
		typeOf = typeOf.Elem()
	}
	if typeOf.Kind() != reflect.Struct {
		return TableSpec{}, fmt.Errorf("bootstrap entity must be a struct, got %s", typeOf.Kind())
	}

	columns := make([]ColumnSpec, 0, typeOf.NumField())
	uniqueGroups := map[string][]string{}

	for idx := 0; idx < typeOf.NumField(); idx++ {
		field := typeOf.Field(idx)
		if !field.IsExported() {
			continue
		}
		if skipField(field) {
			continue
		}

		columnName := strcase.ToSnake(field.Name)
		sqlType, nullable, autoInc, err := sqlTypeForField(field)
		if err != nil {
			return TableSpec{}, err
		}

		if strings.Contains(strings.ToLower(field.Tag.Get("validate")), "required") || field.Tag.Get("pkey") == "true" {
			nullable = false
		}
		if field.Tag.Get("skipWhenInsert") == "true" && field.Tag.Get("pkey") == "true" {
			autoInc = true
		}

		column := ColumnSpec{
			Name:       columnName,
			SQLType:    sqlType,
			Nullable:   nullable,
			PrimaryKey: field.Tag.Get("pkey") == "true",
			AutoInc:    autoInc,
		}
		if group := field.Tag.Get("ukey"); group != "" {
			column.UniqueGroup = group
			uniqueGroups[group] = append(uniqueGroups[group], columnName)
		}
		columns = append(columns, column)
	}

	if len(columns) == 0 {
		return TableSpec{}, fmt.Errorf("bootstrap entity %s has no persistent fields", typeOf.Name())
	}

	uniqueIndexes := make([]IndexSpec, 0, len(uniqueGroups))
	for group, cols := range uniqueGroups {
		uniqueIndexes = append(uniqueIndexes, IndexSpec{
			Name:    fmt.Sprintf("ux_%s_%s", strcase.ToSnake(typeOf.Name()), sanitizeIdentifier(group)),
			Columns: cols,
		})
	}

	return TableSpec{
		Name:    strcase.ToSnake(typeOf.Name()),
		Type:    typeOf.String(),
		Columns: columns,
		Unique:  uniqueIndexes,
	}, nil
}

func skipField(field reflect.StructField) bool {
	if field.Type.Kind() == reflect.Slice {
		return true
	}
	if field.Type.Kind() == reflect.Struct && !(field.Type.PkgPath() == "database/sql" && strings.HasPrefix(field.Type.Name(), "Null")) {
		return true
	}
	return false
}

func sqlTypeForField(field reflect.StructField) (string, bool, bool, error) {
	fieldType := field.Type
	if fieldType.Kind() == reflect.Pointer {
		fieldType = fieldType.Elem()
	}

	if fieldType.PkgPath() == "database/sql" {
		switch fieldType.Name() {
		case "NullString":
			return "TEXT", true, false, nil
		case "NullInt64":
			return "BIGINT", true, false, nil
		case "NullFloat64":
			return "DOUBLE PRECISION", true, false, nil
		case "NullBool":
			return "BOOLEAN", true, false, nil
		case "NullTime":
			return "TIMESTAMPTZ", true, false, nil
		}
	}

	switch fieldType.Kind() {
	case reflect.String:
		return "TEXT", true, false, nil
	case reflect.Bool:
		return "BOOLEAN", true, false, nil
	case reflect.Int, reflect.Int64:
		return "BIGINT", true, false, nil
	case reflect.Int32:
		return "INTEGER", true, false, nil
	case reflect.Int16, reflect.Int8:
		return "SMALLINT", true, false, nil
	case reflect.Uint, reflect.Uint64:
		return "BIGINT", true, false, nil
	case reflect.Uint32:
		return "INTEGER", true, false, nil
	case reflect.Uint16, reflect.Uint8:
		return "SMALLINT", true, false, nil
	case reflect.Float32:
		return "REAL", true, false, nil
	case reflect.Float64:
		return "DOUBLE PRECISION", true, false, nil
	case reflect.Struct:
		if fieldType.PkgPath() == "time" && fieldType.Name() == "Time" {
			return "TIMESTAMPTZ", true, false, nil
		}
	}

	return "TEXT", true, false, nil
}

func sanitizeIdentifier(input string) string {
	value := strings.ToLower(input)
	value = strings.ReplaceAll(value, "-", "_")
	value = strings.ReplaceAll(value, " ", "_")
	return value
}

func columnDefinition(column ColumnSpec, create bool, engine string) string {
	definition := fmt.Sprintf("%s %s", quoteIdent(column.Name, engine), normalizeSQLType(column.SQLType, engine))
	if column.AutoInc && column.PrimaryKey {
		if engine == "mariadb" {
			definition = fmt.Sprintf("%s BIGINT AUTO_INCREMENT", quoteIdent(column.Name, engine))
		} else if engine == "sqlite" {
			definition = fmt.Sprintf("%s INTEGER PRIMARY KEY AUTOINCREMENT", quoteIdent(column.Name, engine))
		} else {
			definition = fmt.Sprintf("%s BIGSERIAL", quoteIdent(column.Name, engine))
		}
	}
	if create && column.PrimaryKey && !(engine == "sqlite" && column.AutoInc) {
		definition += " PRIMARY KEY"
	}
	if create && !column.Nullable && !column.PrimaryKey {
		definition += " NOT NULL"
	}
	return definition
}

func quoteIdent(value string, engine string) string {
	if engine == "mariadb" {
		return fmt.Sprintf("`%s`", strings.ReplaceAll(value, "`", "``"))
	}
	return fmt.Sprintf("\"%s\"", strings.ReplaceAll(value, "\"", "\"\""))
}

func tableDefinition(spec TableSpec, engine string) string {
	parts := make([]string, 0, len(spec.Columns)+len(spec.Unique))
	for _, column := range spec.Columns {
		parts = append(parts, columnDefinition(column, true, engine))
	}
	for _, uniqueIndex := range spec.Unique {
		if engine == "sqlite" {
			continue
		}
		columnNames := make([]string, 0, len(uniqueIndex.Columns))
		for _, columnName := range uniqueIndex.Columns {
			columnNames = append(columnNames, quoteIdent(columnName, engine))
		}
		parts = append(parts, fmt.Sprintf("CONSTRAINT %s UNIQUE (%s)", quoteIdent(uniqueIndex.Name, engine), strings.Join(columnNames, ", ")))
	}
	return strings.Join(parts, ", ")
}

func existingColumns(ctx context.Context, db *sql.DB, engine string, tableName string) (map[string]struct{}, error) {
	if engine == "sqlite" {
		return existingSQLiteColumns(ctx, db, tableName)
	}

	query := `
SELECT column_name
FROM information_schema.columns
WHERE table_schema = 'public' AND table_name = $1
`
	args := []any{tableName}

	if engine == "mariadb" {
		query = `
SELECT column_name
FROM information_schema.columns
WHERE table_schema = DATABASE() AND table_name = ?
`
	}

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	columns := make(map[string]struct{})
	for rows.Next() {
		var columnName string
		if err := rows.Scan(&columnName); err != nil {
			return nil, err
		}
		columns[columnName] = struct{}{}
	}
	return columns, rows.Err()
}

func existingSQLiteColumns(ctx context.Context, db *sql.DB, tableName string) (map[string]struct{}, error) {
	rows, err := db.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%s)", quoteIdent(tableName, "sqlite")))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	columns := make(map[string]struct{})
	for rows.Next() {
		var cid int
		var name string
		var dataType string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &pk); err != nil {
			return nil, err
		}
		columns[name] = struct{}{}
	}
	return columns, rows.Err()
}

func normalizeSQLType(sqlType string, engine string) string {
	if engine == "sqlite" {
		switch strings.ToUpper(strings.TrimSpace(sqlType)) {
		case "BIGINT", "INTEGER", "SMALLINT":
			return "INTEGER"
		case "DOUBLE PRECISION", "REAL":
			return "REAL"
		case "BOOLEAN":
			return "INTEGER"
		case "TIMESTAMPTZ", "JSON":
			return "TEXT"
		default:
			return sqlType
		}
	}

	if engine != "mariadb" {
		return sqlType
	}

	switch strings.ToUpper(strings.TrimSpace(sqlType)) {
	case "TIMESTAMPTZ":
		return "TIMESTAMP"
	case "DOUBLE PRECISION":
		return "DOUBLE"
	case "BOOLEAN":
		return "TINYINT(1)"
	default:
		return sqlType
	}
}
