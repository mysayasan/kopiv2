package sqlite

import (
	"context"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"

	strcase "github.com/iancoleman/strcase"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	_ "modernc.org/sqlite"
)

type dbCrud struct {
	db   *sql.DB
	tx   *sql.Tx
	txMu *sync.Mutex
}

func NewDbCrud(config dbsql.DbConfigModel) (dbsql.IDbCrud, error) {
	dbPath := strings.TrimSpace(config.DbName)
	if dbPath == "" {
		return nil, fmt.Errorf("sqlite db_name is required")
	}
	if dbPath != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
			return nil, err
		}
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)

	crud := &dbCrud{
		db:   db,
		tx:   nil,
		txMu: &sync.Mutex{},
	}
	if err := crud.configure(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	if err = db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return crud, nil
}

func (m *dbCrud) configure(ctx context.Context) error {
	for _, stmt := range []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA journal_mode = WAL",
		"PRAGMA busy_timeout = 5000",
	} {
		if _, err := m.db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func (m *dbCrud) BeginTx(ctx context.Context) error {
	if m.txMu != nil {
		m.txMu.Lock()
	}
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		if m.txMu != nil {
			m.txMu.Unlock()
		}
		return err
	}
	m.tx = tx
	return nil
}

func (m *dbCrud) BeginScopedTx(ctx context.Context) (dbsql.IDbCrud, error) {
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	return &dbCrud{
		db: m.db,
		tx: tx,
	}, nil
}

func (m *dbCrud) Ping(ctx context.Context) error {
	return m.db.PingContext(ctx)
}

func (m *dbCrud) RollbackTx() error {
	if m.tx == nil {
		return nil
	}
	err := m.tx.Rollback()
	if err != nil {
		return err
	}
	m.tx = nil
	if m.txMu != nil {
		m.txMu.Unlock()
	}
	return nil
}

func (m *dbCrud) CommitTx() error {
	if m.tx == nil {
		return nil
	}
	err := m.tx.Commit()
	if err != nil {
		return err
	}
	m.tx = nil
	if m.txMu != nil {
		m.txMu.Unlock()
	}
	return nil
}

func tableNameForValue(props reflect.Value) string {
	typ := props.Type()
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	propName := strcase.ToSnake(typ.Name())
	for _, suffix := range []string{"_entity", "_vw_model", "_join_model", "_model"} {
		temp := strings.Replace(propName, suffix, "", 1)
		if temp != propName {
			return temp
		}
	}
	return propName
}

func indirectStructValue(value reflect.Value) reflect.Value {
	for value.Kind() == reflect.Pointer {
		value = value.Elem()
	}
	return value
}

func joinSpecsFromSources(joinsrc []string) []dbsql.JoinSpec {
	joins := make([]dbsql.JoinSpec, 0, len(joinsrc))
	for idx, src := range joinsrc {
		joins = append(joins, dbsql.JoinSpec{
			Source: src,
			Alias:  fmt.Sprintf("table%d", idx+1),
		})
	}
	return joins
}

func joinFieldColumnName(field reflect.StructField) string {
	if dbcol := strings.TrimSpace(field.Tag.Get("dbcol")); dbcol != "" {
		return dbcol
	}
	return strcase.ToSnake(field.Name)
}

func (m *dbCrud) genJoinSqlStr(props reflect.Value, srcname string, srcalias string) string {
	res := ""
	pkeyFieldNm := ""
	for i := 0; i < props.NumField(); i++ {
		field := props.Type().Field(i)
		if persistentFieldSkipped(field) {
			continue
		}
		if field.Tag.Get("pkey") == "true" {
			pkeyFieldNm = field.Name
			break
		}
	}
	for i := 0; i < props.NumField(); i++ {
		field := props.Type().Field(i)
		if persistentFieldSkipped(field) {
			continue
		}
		if strings.EqualFold(field.Tag.Get("tablejoin"), srcalias) {
			res = fmt.Sprintf("%s\nINNER JOIN %s %s ON table0.%s = %s.%s", res, srcname, srcalias, strcase.ToSnake(field.Name), srcalias, strcase.ToSnake(pkeyFieldNm))
		}
	}
	return res
}

func persistentFieldSkipped(field reflect.StructField) bool {
	if field.Type.Kind() == reflect.Slice && field.Type.Elem().Kind() == reflect.Struct {
		return true
	}
	if field.Type.Kind() == reflect.Struct && !(field.Type.PkgPath() == "database/sql" && strings.HasPrefix(field.Type.Name(), "Null")) {
		return true
	}
	return false
}

func (m *dbCrud) getCols(props reflect.Value) []string {
	res := []string{}
	for i := 0; i < props.NumField(); i++ {
		field := props.Type().Field(i)
		if persistentFieldSkipped(field) {
			continue
		}
		tblalias := field.Tag.Get("tblalias")
		if tblalias != "" {
			res = append(res, fmt.Sprintf("%s.%s", tblalias, joinFieldColumnName(field)))
			continue
		}
		res = append(res, joinFieldColumnName(field))
	}
	return res
}

func (m *dbCrud) genWhereSqlStr(props reflect.Value, filters []sqldataenums.Filter) []string {
	res := []string{}
	for _, filter := range filters {
		fieldNm := strcase.ToSnake(filter.FieldName)
		field, ok := props.Type().FieldByName(filter.FieldName)
		if ok {
			fieldNm = joinFieldColumnName(field)
			if tblalias := field.Tag.Get("tblalias"); tblalias != "" {
				fieldNm = fmt.Sprintf("%s.%s", tblalias, fieldNm)
			}
		}
		op := filterCompareOperator(filter.Compare)
		if op == "" {
			continue
		}
		fieldValue := props.FieldByName(filter.FieldName)
		if !fieldValue.IsValid() {
			continue
		}
		res = append(res, fmt.Sprintf("%s %s %s", fieldNm, op, sqlValueForField(fieldValue, filter.Value)))
	}
	return res
}

func filterCompareOperator(compare sqldataenums.Compare) string {
	switch compare {
	case sqldataenums.Equal:
		return "="
	case sqldataenums.NotEqual:
		return "<>"
	case sqldataenums.GreaterThan:
		return ">"
	case sqldataenums.LessThan:
		return "<"
	case sqldataenums.GreaterThanOrEqualTo:
		return ">="
	case sqldataenums.LessThanOrEqualTo:
		return "<="
	default:
		return ""
	}
}

func (m *dbCrud) genSortSqlStr(props reflect.Value, sorters []sqldataenums.Sorter) []string {
	res := []string{}
	for _, sorter := range sorters {
		fieldNm := strcase.ToSnake(sorter.FieldName)
		field, ok := props.Type().FieldByName(sorter.FieldName)
		if ok {
			fieldNm = joinFieldColumnName(field)
			if tblalias := field.Tag.Get("tblalias"); tblalias != "" {
				fieldNm = fmt.Sprintf("%s.%s", tblalias, fieldNm)
			}
		}
		if sorter.Sort == sqldataenums.DESC {
			res = append(res, fmt.Sprintf("%s DESC", fieldNm))
		} else {
			res = append(res, fmt.Sprintf("%s ASC", fieldNm))
		}
	}
	return res
}

func (m *dbCrud) getFiltersByKeyType(props reflect.Value, keyType sqldataenums.DBKeyType, keyGroup string, keys ...any) []sqldataenums.Filter {
	filters := make([]sqldataenums.Filter, 0)
	keyLoopCnt := 0
	for i := 0; i < props.NumField(); i++ {
		field := props.Type().Field(i)
		if persistentFieldSkipped(field) {
			continue
		}
		if len(keys) > 0 && keyLoopCnt >= len(keys) {
			break
		}
		switch keyType {
		case sqldataenums.DBKeyType(sqldataenums.Primary):
			if keyGroup == "" {
				keyGroup = "true"
			}
			if field.Tag.Get("pkey") != keyGroup {
				continue
			}
		case sqldataenums.DBKeyType(sqldataenums.Unique):
			if field.Tag.Get("ukey") != keyGroup {
				continue
			}
		case sqldataenums.DBKeyType(sqldataenums.Foreign):
			if field.Tag.Get("fkey") != keyGroup {
				continue
			}
		default:
			continue
		}
		val := props.Field(i).Interface()
		if keyLoopCnt < len(keys) {
			val = keys[keyLoopCnt]
			keyLoopCnt++
		}
		filters = append(filters, sqldataenums.Filter{
			FieldName: field.Name,
			Compare:   sqldataenums.Equal,
			Value:     val,
		})
	}
	return filters
}

func sqlValueForField(fieldValue reflect.Value, value any) string {
	fieldType := fieldValue.Type()
	for fieldType.Kind() == reflect.Pointer {
		fieldType = fieldType.Elem()
	}
	return sqlLiteral(value, fieldType)
}

func sqlLiteral(value any, typ reflect.Type) string {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if value == nil {
		return "NULL"
	}
	if typ == reflect.TypeOf(sql.NullString{}) {
		if v, ok := value.(sql.NullString); ok {
			if !v.Valid {
				return "NULL"
			}
			return fmt.Sprintf("'%s'", escapeSQLString(v.String))
		}
	}
	switch typ.Kind() {
	case reflect.Bool:
		if boolValue(value) {
			return "1"
		}
		return "0"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return fmt.Sprintf("%v", value)
	case reflect.Slice:
		if bytes, ok := value.([]uint8); ok {
			return fmt.Sprintf("X'%s'", hex.EncodeToString(bytes))
		}
	}
	return fmt.Sprintf("'%s'", escapeSQLString(fmt.Sprintf("%v", value)))
}

func fieldLiteral(field reflect.StructField, value reflect.Value) string {
	if value.Kind() == reflect.Interface && !value.IsNil() {
		value = value.Elem()
	}
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return "NULL"
		}
		value = value.Elem()
	}
	if value.Type() == reflect.TypeOf(sql.NullString{}) {
		ns := value.Interface().(sql.NullString)
		if !ns.Valid {
			return "NULL"
		}
		return fmt.Sprintf("'%s'", escapeSQLString(ns.String))
	}
	return sqlLiteral(value.Interface(), field.Type)
}

func boolValue(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case int64:
		return v != 0
	case int:
		return v != 0
	case string:
		return strings.EqualFold(v, "true") || v == "1"
	default:
		return strings.EqualFold(fmt.Sprintf("%v", value), "true")
	}
}

func escapeSQLString(value string) string {
	return strings.ReplaceAll(value, "'", "''")
}
