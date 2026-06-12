package sqlite

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strings"

	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
)

func (m *dbCrud) genDelSqlStr(props reflect.Value, datasrc string, filters []sqldataenums.Filter) string {
	props = indirectStructValue(props)
	if datasrc == "" {
		datasrc = tableNameForValue(props)
	}
	selFilters := m.genWhereSqlStr(props, filters)
	res := ""
	if len(selFilters) > 0 {
		res = fmt.Sprintf(`DELETE FROM %s WHERE %s;`, datasrc, strings.Join(selFilters, " AND "))
	}
	if os.Getenv("ENVIRONMENT") == "dev" {
		fmt.Println(res)
	}
	return res
}

func (m *dbCrud) Delete(ctx context.Context, model interface{}, datasrc string, filters []sqldataenums.Filter) (uint64, error) {
	props := indirectStructValue(reflect.ValueOf(model))
	if len(filters) < 1 {
		return 0, fmt.Errorf("delete failed : filters are required")
	}
	return m.deleteByFilters(ctx, props, datasrc, filters)
}

func (m *dbCrud) DeleteById(ctx context.Context, model interface{}, datasrc string, id uint64) (uint64, error) {
	props := indirectStructValue(reflect.ValueOf(model))
	return m.deleteByFilters(ctx, props, datasrc, m.getFiltersByKeyType(props, sqldataenums.DBKeyType(sqldataenums.Primary), "true", id))
}

func (m *dbCrud) DeleteByUnique(ctx context.Context, model interface{}, datasrc string, keyGroup string, uids ...any) (uint64, error) {
	props := indirectStructValue(reflect.ValueOf(model))
	return m.deleteByFilters(ctx, props, datasrc, m.getFiltersByKeyType(props, sqldataenums.DBKeyType(sqldataenums.Unique), keyGroup, uids...))
}

func (m *dbCrud) DeleteByForeign(ctx context.Context, model interface{}, datasrc string, keyGroup string, fids ...any) (uint64, error) {
	props := indirectStructValue(reflect.ValueOf(model))
	return m.deleteByFilters(ctx, props, datasrc, m.getFiltersByKeyType(props, sqldataenums.DBKeyType(sqldataenums.Foreign), keyGroup, fids...))
}

func (m *dbCrud) deleteByFilters(ctx context.Context, props reflect.Value, datasrc string, filters []sqldataenums.Filter) (uint64, error) {
	if len(filters) < 1 {
		return 0, fmt.Errorf("delete failed : cant find pkey or ukey in data fields")
	}
	sqlStr := m.genDelSqlStr(props, datasrc, filters)
	if strings.TrimSpace(sqlStr) == "" {
		return 0, fmt.Errorf("delete failed : filters are required")
	}
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
