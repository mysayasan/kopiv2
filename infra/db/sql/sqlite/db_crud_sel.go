package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync"

	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

func (m *dbCrud) genSelSqlStr(props reflect.Value, limit uint64, offset uint64, filters []sqldataenums.Filter, sorters []sqldataenums.Sorter, datasrc string, joinsrc ...string) (int, string) {
	return m.genSelSqlStrWithJoinSpecs(props, limit, offset, filters, sorters, datasrc, joinSpecsFromSources(joinsrc)...)
}

func (m *dbCrud) genSelSqlStrWithJoinSpecs(props reflect.Value, limit uint64, offset uint64, filters []sqldataenums.Filter, sorters []sqldataenums.Sorter, datasrc string, joins ...dbsql.JoinSpec) (int, string) {
	props = indirectStructValue(props)
	if datasrc == "" {
		datasrc = tableNameForValue(props)
	}
	if len(joins) > 0 {
		datasrc = fmt.Sprintf("%s table0", datasrc)
		for _, join := range joins {
			if strings.TrimSpace(join.Source) == "" || strings.TrimSpace(join.Alias) == "" {
				continue
			}
			datasrc = fmt.Sprintf("%s\n %s", datasrc, m.genJoinSqlStr(props, join.Source, join.Alias))
		}
	}

	selCols := m.getCols(props)
	res := fmt.Sprintf(`SELECT %s
FROM %s`, strings.Join(selCols, `, `), datasrc)

	if selFilters := m.genWhereSqlStr(props, filters); len(selFilters) > 0 {
		res = fmt.Sprintf("%s\nWHERE %s", res, strings.Join(selFilters, " AND "))
	}
	if selSorters := m.genSortSqlStr(props, sorters); len(selSorters) > 0 {
		res = fmt.Sprintf("%s\nORDER BY %s", res, strings.Join(selSorters, ","))
	}

	pageClause := ""
	if limit > 0 {
		pageClause = fmt.Sprintf("LIMIT %d", limit)
	}
	if offset > 0 {
		if pageClause == "" {
			pageClause = "LIMIT -1"
		}
		pageClause = fmt.Sprintf("%s OFFSET %d", pageClause, offset)
	}

	res = fmt.Sprintf(`WITH cte AS (
%s
)
SELECT page.*, (SELECT COUNT(*) FROM cte) AS x_rows_cnt
FROM (
	SELECT *
	FROM cte
	%s
) page;`, res, pageClause)

	if os.Getenv("ENVIRONMENT") == "dev" {
		fmt.Println(res)
	}
	return len(selCols), res
}

func (m *dbCrud) SelectJoin(ctx context.Context, model interface{}, limit uint64, offset uint64, filters []sqldataenums.Filter, sorters []sqldataenums.Sorter, datasrc string, joins ...dbsql.JoinSpec) ([]map[string]interface{}, uint64, error) {
	props := reflect.ValueOf(model)
	colCnt, sqlStr := m.genSelSqlStrWithJoinSpecs(props, limit, offset, filters, sorters, datasrc, joins...)
	return m.selectWithSQL(ctx, model, colCnt, sqlStr)
}

func (m *dbCrud) Select(ctx context.Context, model interface{}, limit uint64, offset uint64, filters []sqldataenums.Filter, sorters []sqldataenums.Sorter, datasrc string, joinsrc ...string) ([]map[string]interface{}, uint64, error) {
	props := reflect.ValueOf(model)
	colCnt, sqlStr := m.genSelSqlStr(props, limit, offset, filters, sorters, datasrc, joinsrc...)
	return m.selectWithSQL(ctx, model, colCnt, sqlStr)
}

func (m *dbCrud) selectWithSQL(ctx context.Context, model interface{}, colCnt int, sqlStr string) ([]map[string]interface{}, uint64, error) {
	props := indirectStructValue(reflect.ValueOf(model))
	var rows *sql.Rows
	var err error
	if m.tx != nil {
		rows, err = m.tx.QueryContext(ctx, sqlStr)
		if err != nil {
			if rbErr := m.tx.Rollback(); rbErr != nil {
				return nil, 0, fmt.Errorf("tx err: %v, rb err: %v", err, rbErr)
			}
			return nil, 0, err
		}
	} else {
		rows, err = m.db.QueryContext(ctx, sqlStr)
		if err != nil {
			return nil, 0, err
		}
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, 0, err
	}
	hasRowsCount := len(cols) > 0 && cols[len(cols)-1] == "x_rows_cnt"
	expectedColCnt := colCnt
	if hasRowsCount {
		expectedColCnt++
	}
	if len(cols) != expectedColCnt {
		return nil, 0, errors.New("different length between db columns prop field")
	}

	maxRowCnt := uint64(100)
	rowCnt := uint64(0)
	totalCnt := uint64(0)
	result := make([]map[string]interface{}, 0)
	for rows.Next() {
		if maxRowCnt != 0 && rowCnt >= maxRowCnt {
			break
		}
		rowCnt++
		vals := make([]interface{}, len(cols))
		raw := make([]interface{}, len(cols))
		for i := range vals {
			vals[i] = &raw[i]
		}
		if err = rows.Scan(vals...); err != nil {
			return nil, 0, err
		}

		data := make(map[string]interface{})
		for i := 0; i < props.NumField(); i++ {
			field := props.Type().Field(i)
			data[field.Name] = normalizeSQLiteScannedValue(raw[i], field.Type)
		}
		if hasRowsCount {
			totalCnt = rawCountToUint64(raw[len(raw)-1])
		}
		result = append(result, data)
	}
	if len(result) < 1 {
		return nil, 0, fmt.Errorf("no result found")
	}

	if len(result) > 0 {
		var wg sync.WaitGroup
		for _, res := range result {
			for i := 0; i < props.NumField(); i++ {
				field := props.Type().Field(i)
				if field.Type.Kind() == reflect.Slice && field.Type.Elem().Kind() == reflect.Struct {
					cdatsrc := field.Tag.Get("datasrc")
					pkeys := strings.Split(field.Tag.Get("parents"), ",")
					var filters []sqldataenums.Filter
					for _, pkey := range pkeys {
						fkeys := strings.Split(pkey, "=")
						if len(fkeys) != 2 {
							continue
						}
						filters = append(filters, sqldataenums.Filter{
							FieldName: fkeys[1],
							Compare:   sqldataenums.Equal,
							Value:     derefValue(res[fkeys[0]]),
						})
					}
					wg.Add(1)
					go func(res map[string]interface{}, props reflect.Value, filters []sqldataenums.Filter, cdatsrc string) {
						defer wg.Done()
						rows, _, err := m.Select(ctx, props.Interface(), 0, 0, filters, nil, cdatsrc)
						if err == nil {
							res[field.Name] = rows
						}
					}(res, reflect.Indirect(reflect.New(field.Type.Elem())), filters, cdatsrc)
				}
			}
		}
		wg.Wait()
	}

	if os.Getenv("ENVIRONMENT") == "dev" {
		fmt.Println(result)
	}
	if !hasRowsCount {
		totalCnt = rowCnt
	}
	return result, totalCnt, rows.Err()
}

func normalizeSQLiteScannedValue(raw interface{}, fieldType reflect.Type) interface{} {
	if fieldType == reflect.TypeOf(sql.NullString{}) {
		switch v := raw.(type) {
		case nil:
			return &sql.NullString{}
		case string:
			return &sql.NullString{String: v, Valid: true}
		case []byte:
			return &sql.NullString{String: string(v), Valid: true}
		default:
			return &sql.NullString{String: fmt.Sprintf("%v", v), Valid: true}
		}
	}
	if fieldType.Kind() == reflect.Slice && fieldType.Elem().Kind() == reflect.Uint8 {
		switch v := raw.(type) {
		case []byte:
			return &v
		case string:
			b := []byte(v)
			return &b
		default:
			b := []byte{}
			return &b
		}
	}
	value := normalizeScalar(raw, fieldType)
	ptr := reflect.New(fieldType)
	if value.IsValid() && value.Type().ConvertibleTo(fieldType) {
		ptr.Elem().Set(value.Convert(fieldType))
	}
	return ptr.Interface()
}

func normalizeScalar(raw interface{}, fieldType reflect.Type) reflect.Value {
	for fieldType.Kind() == reflect.Pointer {
		fieldType = fieldType.Elem()
	}
	if raw == nil {
		return reflect.Zero(fieldType)
	}
	switch fieldType.Kind() {
	case reflect.Bool:
		return reflect.ValueOf(boolValue(raw))
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return reflect.ValueOf(int64Value(raw))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return reflect.ValueOf(uint64(int64Value(raw)))
	case reflect.Float32, reflect.Float64:
		return reflect.ValueOf(float64Value(raw))
	case reflect.String:
		return reflect.ValueOf(stringValue(raw))
	default:
		return reflect.ValueOf(raw)
	}
}

func derefValue(value interface{}) interface{} {
	v := reflect.ValueOf(value)
	for v.IsValid() && v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil
		}
		v = v.Elem()
	}
	if !v.IsValid() {
		return nil
	}
	return v.Interface()
}

func rawCountToUint64(raw interface{}) uint64 {
	n := int64Value(raw)
	if n < 0 {
		return 0
	}
	return uint64(n)
}

func int64Value(raw interface{}) int64 {
	switch v := raw.(type) {
	case int:
		return int64(v)
	case int64:
		return v
	case float64:
		return int64(v)
	case []byte:
		n, _ := strconv.ParseInt(string(v), 10, 64)
		return n
	case string:
		n, _ := strconv.ParseInt(v, 10, 64)
		return n
	default:
		n, _ := strconv.ParseInt(fmt.Sprintf("%v", v), 10, 64)
		return n
	}
}

func float64Value(raw interface{}) float64 {
	switch v := raw.(type) {
	case float32:
		return float64(v)
	case float64:
		return v
	case int64:
		return float64(v)
	case []byte:
		n, _ := strconv.ParseFloat(string(v), 64)
		return n
	case string:
		n, _ := strconv.ParseFloat(v, 64)
		return n
	default:
		n, _ := strconv.ParseFloat(fmt.Sprintf("%v", v), 64)
		return n
	}
}

func stringValue(raw interface{}) string {
	switch v := raw.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	default:
		return fmt.Sprintf("%v", v)
	}
}

func (m *dbCrud) SelectSingle(ctx context.Context, model interface{}, filters []sqldataenums.Filter, datasrc string) (map[string]interface{}, error) {
	props := reflect.ValueOf(model)
	rows, _, err := m.Select(ctx, props.Interface(), 1, 0, filters, nil, datasrc)
	if err != nil {
		return nil, err
	}
	if len(rows) > 0 {
		return rows[0], nil
	}
	return nil, nil
}

func (m *dbCrud) SelectById(ctx context.Context, model interface{}, datasrc string, id uint64) (map[string]interface{}, error) {
	props := indirectStructValue(reflect.ValueOf(model))
	filters := m.getFiltersByKeyType(props, sqldataenums.DBKeyType(sqldataenums.Primary), "true", id)
	rows, _, err := m.Select(ctx, props.Interface(), 1, 0, filters, nil, datasrc)
	if err != nil {
		return nil, err
	}
	if len(rows) > 0 {
		return rows[0], nil
	}
	return nil, nil
}

func (m *dbCrud) SelectByUnique(ctx context.Context, model interface{}, datasrc string, keyGroup string, uids ...any) (map[string]interface{}, error) {
	props := indirectStructValue(reflect.ValueOf(model))
	filters := m.getFiltersByKeyType(props, sqldataenums.DBKeyType(sqldataenums.Unique), keyGroup, uids...)
	rows, _, err := m.Select(ctx, props.Interface(), 1, 0, filters, nil, datasrc)
	if err != nil {
		return nil, err
	}
	if len(rows) > 0 {
		return rows[0], nil
	}
	return nil, nil
}

func (m *dbCrud) SelectByForeign(ctx context.Context, model interface{}, datasrc string, keyGroup string, fids ...any) ([]map[string]interface{}, error) {
	props := indirectStructValue(reflect.ValueOf(model))
	filters := m.getFiltersByKeyType(props, sqldataenums.DBKeyType(sqldataenums.Foreign), keyGroup, fids...)
	rows, _, err := m.Select(ctx, props.Interface(), 1, 0, filters, nil, datasrc)
	if err != nil {
		return nil, err
	}
	if len(rows) > 0 {
		return rows, nil
	}
	return nil, nil
}
