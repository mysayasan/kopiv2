package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"

	strcase "github.com/iancoleman/strcase"
	_ "github.com/lib/pq"
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
	conn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s", config.Host, config.Port, config.User, config.Password, config.DbName, config.SslMode)
	db, err := sql.Open("postgres", conn)
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

func (m *dbCrud) genWhereSqlStr(props reflect.Value, filters []sqldataenums.Filter) []string {
	res := []string{}
	for _, filter := range filters {
		if filter.Compare == 1 {
			field := props.FieldByName(filter.FieldName)
			switch field.Interface().(type) {
			case int, int16, int32, int64, uint, uint16, uint32, uint64:
				{
					fs := fmt.Sprintf("%s = %d", strcase.ToSnake(filter.FieldName), filter.Value)
					res = append(res, fs)
					break
				}
			default:
				{
					fs := fmt.Sprintf("%s = '%s'", strcase.ToSnake(filter.FieldName), filter.Value)
					res = append(res, fs)
					break
				}
			}
		}
	}

	return res
}

func (m *dbCrud) genSortSqlStr(sorters []sqldataenums.Sorter) []string {
	res := []string{}
	for _, sorter := range sorters {
		// field := props.FieldByName(sorter.FieldName)
		if sorter.Sort == 2 {
			fs := fmt.Sprintf("%s DESC", strcase.ToSnake(sorter.FieldName))
			res = append(res, fs)
		} else {
			fs := fmt.Sprintf("%s ASC", strcase.ToSnake(sorter.FieldName))
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

		res = append(res, strcase.ToSnake(field.Name))
		// varName := props.Type().Field(i).Name
		// varType := props.Type().Field(i).Type
		// varValue := props.Field(i).Interface()
		// fmt.Printf("%v %v %v\n", varName, varType, varValue)
	}

	return res
}

func (m *dbCrud) getFiltersByKeyType(props reflect.Value, keyType sqldataenums.DBKeyType, keys ...any) []sqldataenums.Filter {
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
				if field.Tag.Get("pkey") == "true" {
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
				if field.Tag.Get("ukey") == "true" {
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
				if field.Tag.Get("fkey") == "true" {
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
