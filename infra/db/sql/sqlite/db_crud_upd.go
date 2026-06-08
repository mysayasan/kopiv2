package sqlite

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strings"

	strcase "github.com/iancoleman/strcase"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
)

func (m *dbCrud) genUpdSqlStr(props reflect.Value, datasrc string, filters []sqldataenums.Filter) string {
	props = indirectStructValue(props)
	if datasrc == "" {
		datasrc = tableNameForValue(props)
	}
	values := []string{}
	for i := 0; i < props.NumField(); i++ {
		field := props.Type().Field(i)
		if persistentFieldSkipped(field) || field.Tag.Get("ignoreOnUpdate") == "true" || field.Tag.Get("pkey") == "true" {
			continue
		}
		values = append(values, fmt.Sprintf("%s = %s", strcase.ToSnake(field.Name), fieldLiteral(field, props.Field(i))))
	}
	res := fmt.Sprintf(`UPDATE %s SET %s`, datasrc, strings.Join(values, `, `))
	if selFilters := m.genWhereSqlStr(props, filters); len(selFilters) > 0 {
		res = fmt.Sprintf("%s WHERE %s", res, strings.Join(selFilters, " AND "))
	}
	res = fmt.Sprintf(`%s;`, res)
	if os.Getenv("ENVIRONMENT") == "dev" {
		fmt.Println(res)
	}
	return res
}

func (m *dbCrud) UpdateById(ctx context.Context, model interface{}, datasrc string) (uint64, error) {
	props := indirectStructValue(reflect.ValueOf(model))
	return m.updateByFilters(ctx, props, datasrc, m.getFiltersByKeyType(props, sqldataenums.DBKeyType(sqldataenums.Primary), "true"))
}

func (m *dbCrud) UpdateByUnique(ctx context.Context, model interface{}, datasrc string, keyGroup string) (uint64, error) {
	props := indirectStructValue(reflect.ValueOf(model))
	return m.updateByFilters(ctx, props, datasrc, m.getFiltersByKeyType(props, sqldataenums.DBKeyType(sqldataenums.Unique), keyGroup))
}

func (m *dbCrud) UpdateByForeign(ctx context.Context, model interface{}, datasrc string, keyGroup string) (uint64, error) {
	props := indirectStructValue(reflect.ValueOf(model))
	return m.updateByFilters(ctx, props, datasrc, m.getFiltersByKeyType(props, sqldataenums.DBKeyType(sqldataenums.Foreign), keyGroup))
}

func (m *dbCrud) updateByFilters(ctx context.Context, props reflect.Value, datasrc string, filters []sqldataenums.Filter) (uint64, error) {
	if len(filters) < 1 {
		return 0, fmt.Errorf("update failed : cant find pkey or ukey in data fields")
	}
	sqlStr := m.genUpdSqlStr(props, datasrc, filters)
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
	affect, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	if affect < 1 {
		return 0, fmt.Errorf("weird  behaviour. total affected: %d", affect)
	}
	return uint64(affect), nil
}
