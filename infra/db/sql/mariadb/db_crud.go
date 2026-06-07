package mariadb

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"strings"

	_ "github.com/go-sql-driver/mysql"
	strcase "github.com/iancoleman/strcase"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// dbCrud struct
type dbCrud struct {
	db *sql.DB
	tx *sql.Tx
}

// Create new DbCrud
func NewDbCrud(config dbsql.DbConfigModel) (dbsql.IDbCrud, error) {
	conn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?parseTime=true&multiStatements=true", config.User, config.Password, config.Host, config.Port, config.DbName)
	db, err := sql.Open("mysql", conn)
	if err != nil {
		return nil, err
	}
	if err = db.Ping(); err != nil {
		return nil, err
	}

	return &dbCrud{
		db: db,
		tx: nil,
	}, nil
}

func (m *dbCrud) BeginTx(ctx context.Context) error {
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	m.tx = tx
	return nil
}

func (m *dbCrud) Ping(ctx context.Context) error {
	return m.db.PingContext(ctx)
}

func (m *dbCrud) RollbackTx() error {
	err := m.tx.Rollback()
	if err != nil {
		return err
	}
	m.tx = nil
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
	return nil
}

func (m *dbCrud) EndTx() error {
	m.tx = nil
	return nil
}

func (m *dbCrud) genJoinSqlStr(props reflect.Value, srcname string, srcalias string) string {
	res := ""

	pkeyFieldNm := ""
	for i := 0; i < props.NumField(); i++ {
		field := props.Type().Field(i)
		if field.Type.Kind() == reflect.Slice {
			if field.Type.Elem().Kind() == reflect.Struct {
				continue
			}
		}

		if field.Tag.Get("pkey") == "true" {
			pkeyFieldNm = field.Name
			break
		}
	}

	for i := 0; i < props.NumField(); i++ {
		field := props.Type().Field(i)
		if field.Type.Kind() == reflect.Slice {
			if field.Type.Elem().Kind() == reflect.Struct {
				continue
			}
		}

		if strings.EqualFold(field.Tag.Get("tablejoin"), srcalias) {
			res = fmt.Sprintf("%s\nINNER JOIN %s %s ON table0.%s = %s.%s", res, srcname, srcalias, strcase.ToSnake(field.Name), srcalias, strcase.ToSnake(pkeyFieldNm))
		}
	}

	return res
}

func (m *dbCrud) genWhereSqlStr(props reflect.Value, filters []sqldataenums.Filter) []string {
	res := []string{}
	for _, filter := range filters {
		filter := filter
		fieldNm := strcase.ToSnake(filter.FieldName)
		field, ok := props.Type().FieldByName(filter.FieldName)
		if ok {
			tblalias := field.Tag.Get("tblalias")
			if tblalias != "" {
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
		switch fieldValue.Interface().(type) {
		case int, int16, int32, int64, uint, uint16, uint32, uint64:
			fs := fmt.Sprintf("%s %s %d", fieldNm, op, filter.Value)
			res = append(res, fs)
		case bool:
			fs := fmt.Sprintf("%s %s '%t'", fieldNm, op, filter.Value)
			res = append(res, fs)
		default:
			fs := fmt.Sprintf("%s %s '%s'", fieldNm, op, filter.Value)
			res = append(res, fs)
		}
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
		sorter := sorter
		fieldNm := strcase.ToSnake(sorter.FieldName)
		field, ok := props.Type().FieldByName(sorter.FieldName)
		if ok {
			tblalias := field.Tag.Get("tblalias")
			if tblalias != "" {
				fieldNm = fmt.Sprintf("%s.%s", tblalias, fieldNm)
			}
		}
		if sorter.Sort == 2 {
			fs := fmt.Sprintf("%s DESC", fieldNm)
			res = append(res, fs)
		} else {
			fs := fmt.Sprintf("%s ASC", fieldNm)
			res = append(res, fs)
		}
	}

	return res
}

func (m *dbCrud) getCols(props reflect.Value) []string {
	res := []string{}
	for i := 0; i < props.NumField(); i++ {
		field := props.Type().Field(i)
		if field.Type.Kind() == reflect.Slice {
			if field.Type.Elem().Kind() == reflect.Struct {
				continue
			}
		}

		tblalias := field.Tag.Get("tblalias")
		if tblalias != "" {
			res = append(res, fmt.Sprintf("%s.%s", tblalias, strcase.ToSnake(field.Name)))
			continue
		}

		res = append(res, strcase.ToSnake(field.Name))
		// varName := props.Type().Field(i).Name
		// varType := props.Type().Field(i).Type
		// varValue := props.Field(i).Interface()
		// fmt.Printf("%v %v %v\n", varName, varType, varValue)
	}

	return res
}

func (m *dbCrud) getFiltersByKeyType(props reflect.Value, keyType sqldataenums.DBKeyType, keyGroup string, keys ...any) []sqldataenums.Filter {
	filters := make([]sqldataenums.Filter, 0)
	keyLoopCnt := 0
	for i := 0; i < props.NumField(); i++ {
		field := props.Type().Field(i)
		if field.Type.Kind() == reflect.Slice {
			if field.Type.Elem().Kind() == reflect.Struct {
				continue
			}
		}

		if len(keys) > 0 && keyLoopCnt >= len(keys) {
			break
		}

		switch keyType {
		case sqldataenums.DBKeyType(sqldataenums.Primary):
			{
				if keyGroup == "" {
					keyGroup = "true"
				}
				if field.Tag.Get("pkey") == keyGroup {
					val := props.Field(i).Interface()
					if keyLoopCnt < len(keys) {
						val = keys[keyLoopCnt]
						keyLoopCnt++
					}

					filter := sqldataenums.Filter{
						FieldName: field.Name,
						Compare:   1,
						Value:     val,
					}
					filters = append(filters, filter)
				}
				break
			}
		case sqldataenums.DBKeyType(sqldataenums.Unique):
			{
				if field.Tag.Get("ukey") == keyGroup {
					val := props.Field(i).Interface()
					if keyLoopCnt < len(keys) {
						val = keys[keyLoopCnt]
						keyLoopCnt++
					}

					filter := sqldataenums.Filter{
						FieldName: field.Name,
						Compare:   1,
						Value:     val,
					}
					filters = append(filters, filter)
				}
				break
			}
		case sqldataenums.DBKeyType(sqldataenums.Foreign):
			{
				if field.Tag.Get("fkey") == keyGroup {
					val := props.Field(i).Interface()
					if keyLoopCnt < len(keys) {
						val = keys[keyLoopCnt]
						keyLoopCnt++
					}

					filter := sqldataenums.Filter{
						FieldName: field.Name,
						Compare:   1,
						Value:     val,
					}
					filters = append(filters, filter)
				}
				break
			}
		}
	}

	return filters
}
