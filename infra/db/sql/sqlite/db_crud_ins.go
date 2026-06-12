package sqlite

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strings"

	strcase "github.com/iancoleman/strcase"
)

func (m *dbCrud) genInsSqlStr(props reflect.Value, datasrc string) string {
	props = indirectStructValue(props)
	if datasrc == "" {
		datasrc = tableNameForValue(props)
	}

	selCols := []string{}
	values := []string{}
	for i := 0; i < props.NumField(); i++ {
		field := props.Type().Field(i)
		if persistentFieldSkipped(field) || field.Tag.Get("skipWhenInsert") == "true" {
			continue
		}
		selCols = append(selCols, strcase.ToSnake(field.Name))
		values = append(values, fieldLiteral(field, props.Field(i)))
	}

	res := fmt.Sprintf(`INSERT INTO %s (%s) VALUES (%s)`, datasrc, strings.Join(selCols, `, `), strings.Join(values, `, `))
	if os.Getenv("ENVIRONMENT") == "dev" {
		fmt.Println(res)
	}
	return res
}

func (m *dbCrud) Insert(ctx context.Context, model interface{}, datasrc string) (uint64, error) {
	props := reflect.ValueOf(model)
	for props.Kind() == reflect.Pointer {
		props = props.Elem()
	}
	if props.Kind() == reflect.Slice || props.Kind() == reflect.Array {
		var lastId int64
		for i := 0; i < props.Len(); i++ {
			id, err := m.insertOne(ctx, props.Index(i), datasrc)
			if err != nil {
				return 0, err
			}
			lastId = int64(id)
		}
		return uint64(lastId), nil
	}
	return m.insertOne(ctx, props, datasrc)
}

func (m *dbCrud) insertOne(ctx context.Context, props reflect.Value, datasrc string) (uint64, error) {
	sqlStr := m.genInsSqlStr(props, datasrc)
	var res sqlResult
	var err error
	if m.tx != nil {
		res, err = m.tx.ExecContext(ctx, sqlStr)
	} else {
		res, err = m.db.ExecContext(ctx, sqlStr)
	}
	if err != nil {
		return 0, err
	}
	lastid, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	return uint64(lastid), nil
}

type sqlResult interface {
	LastInsertId() (int64, error)
	RowsAffected() (int64, error)
}
