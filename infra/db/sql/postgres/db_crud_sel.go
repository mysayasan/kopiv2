package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"

	strcase "github.com/iancoleman/strcase"
	_ "github.com/lib/pq"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

func (m *dbCrud) genSelSqlStr(props reflect.Value, limit uint64, offset uint64, filters []sqldataenums.Filter, sorters []sqldataenums.Sorter, datasrc string, joinsrc ...string) (int, string) {
	return m.genSelSqlStrWithJoinSpecs(props, limit, offset, filters, sorters, datasrc, joinSpecsFromSources(joinsrc)...)
}

func (m *dbCrud) genSelSqlStrWithJoinSpecs(props reflect.Value, limit uint64, offset uint64, filters []sqldataenums.Filter, sorters []sqldataenums.Sorter, datasrc string, joins ...dbsql.JoinSpec) (int, string) {
	if datasrc == "" {
		propName := strcase.ToSnake(props.Type().Name())
		temp := strings.Replace(propName, "_entity", "", 1)
		if temp == propName {
			temp = strings.Replace(propName, "_vw_model", "", 1)
		}
		if temp == propName {
			temp = strings.Replace(propName, "_join_model", "", 1)
		}
		if temp == propName {
			temp = strings.Replace(propName, "_model", "", 1)
		}
		datasrc = temp
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
	selSqlStr := strings.Join(selCols, `, `)
	res := fmt.Sprintf(`SELECT %s
	FROM %s`, selSqlStr, datasrc)

	selFilters := m.genWhereSqlStr(props, filters)
	if len(selFilters) > 0 {
		res = fmt.Sprintf(`
		%s
		WHERE %s
		`, res, strings.Join(selFilters, " AND "))
	}

	selSorters := m.genSortSqlStr(props, sorters)
	if len(selSorters) > 0 {
		res = fmt.Sprintf(`
		%s
		ORDER BY %s
		`, res, strings.Join(selSorters, ","))
	}

	rowLimit := ""
	rowOffset := ""

	if limit > 0 {
		rowLimit = fmt.Sprintf("LIMIT %d", limit)
	}

	if offset > 0 {
		rowOffset = fmt.Sprintf("OFFSET %d", offset)
	}

	if rowLimit != "" || rowOffset != "" {
		res = fmt.Sprintf(`
		WITH cte AS (
			%s
			)
			SELECT *
			FROM  (
			TABLE  cte
			%s
			%s
			) sub
			INNER JOIN (SELECT count(*) FROM cte) c(x_rows_cnt) ON true	
		`, res, rowLimit, rowOffset)
	}

	res = fmt.Sprintf("%s;", res)

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
	props := reflect.ValueOf(model)
	rows := &sql.Rows{}
	var err error

	if m.tx != nil {
		rows, err = m.tx.QueryContext(ctx, sqlStr)
		if err != nil {
			if rbErr := m.tx.Rollback(); rbErr != nil {
				err = fmt.Errorf("tx err: %v, rb err: %v", err, rbErr)
				return nil, 0, err
			}
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

	propsCnt := colCnt
	if cols[len(cols)-1] == "x_rows_cnt" {
		propsCnt += 1
	}

	if len(cols) != propsCnt {
		return nil, 0, errors.New("different length between db columns prop field")
	}

	maxRowCnt := uint64(100)
	rowCnt := uint64(0)
	totalCnt := uint64(0)
	hasRowsCount := len(cols) > 0 && cols[len(cols)-1] == "x_rows_cnt"
	result := make([]map[string]interface{}, 0)

	for rows.Next() {
		if maxRowCnt != 0 && rowCnt >= maxRowCnt {
			break
		}

		rowCnt++
		vals := make([]interface{}, len(cols))
		for i := 0; i < len(cols); i++ {
			if cols[i] == "x_rows_cnt" {
				vals[i] = new(int64)
				continue
			}
			vals[i] = scanDestinationForField(props.Type().Field(i).Type)
		}

		err = rows.Scan(
			vals...,
		)
		if err != nil {
			return nil, 0, err
		}

		data := make(map[string]interface{})

		for i := 0; i < props.NumField(); i++ {
			field := props.Type().Field(i)
			data[field.Name] = normalizeScannedValue(vals[i], field.Type)
		}

		if hasRowsCount {
			totalCnt = signedCountToUint64(*vals[len(vals)-1].(*int64))
		}

		result = append(result, data)
	}

	if len(result) < 1 {
		return nil, 0, fmt.Errorf("no result found")
	}

	if len(result) > 0 {
		var wg sync.WaitGroup
		for _, res := range result {
			res := res
			for i := 0; i < props.NumField(); i++ {
				field := props.Type().Field(i)
				if field.Type.Kind() == reflect.Slice {
					if field.Type.Elem().Kind() == reflect.Struct {
						cdatsrc := field.Tag.Get("datasrc")
						pkeys := strings.Split(field.Tag.Get("parents"), ",")

						var filters []sqldataenums.Filter
						for _, pkey := range pkeys {
							pkey := pkey
							fkeys := strings.Split(pkey, "=")
							var val interface{}
							switch props.FieldByName(fkeys[0]).Interface().(type) {
							case []uint8:
								{
									val = *res[fkeys[0]].(*[]uint8)
									break
								}
							case int16:
								{
									val = *res[fkeys[0]].(*int16)
									break
								}
							case uint16:
								{
									val = *res[fkeys[0]].(*uint16)
									break
								}
							case int, int32:
								{
									val = *res[fkeys[0]].(*int)
									break
								}
							case uint, uint32:
								{
									val = *res[fkeys[0]].(*uint)
									break
								}
							case int64:
								{
									val = *res[fkeys[0]].(*int64)
									break
								}
							case uint64:
								{
									val = *res[fkeys[0]].(*uint64)
									break
								}
							case float32:
								{
									val = *res[fkeys[0]].(*float32)
									break
								}
							case float64:
								{
									val = *res[fkeys[0]].(*float64)
									break
								}
							case string:
								{
									val = *res[fkeys[0]].(*string)
									break
								}
							case sql.NullString:
								{
									val = *res[fkeys[0]].(*sql.NullString)
									break
								}
							case bool:
								{
									val = *res[fkeys[0]].(*bool)
								}
							}
							filters = append(filters, sqldataenums.Filter{
								FieldName: fkeys[1],
								Compare:   1,
								Value:     val,
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
		}
		wg.Wait()
	}

	if os.Getenv("ENVIRONMENT") == "dev" {
		fmt.Println(result)
	}

	if !hasRowsCount {
		totalCnt = rowCnt
	}

	return result, totalCnt, nil
}

func normalizeScannedValue(raw interface{}, fieldType reflect.Type) interface{} {
	if value, ok := raw.(*sql.NullString); ok && fieldType.Kind() == reflect.String {
		normalized := ""
		if value.Valid {
			normalized = value.String
		}
		return &normalized
	}

	return raw
}

func scanDestinationForField(fieldType reflect.Type) interface{} {
	if fieldType == reflect.TypeOf(sql.NullString{}) {
		return new(sql.NullString)
	}
	if fieldType.Kind() == reflect.Slice && fieldType.Elem().Kind() == reflect.Uint8 {
		return new([]uint8)
	}

	switch fieldType.Kind() {
	case reflect.Int:
		return new(int)
	case reflect.Int8:
		return new(int8)
	case reflect.Int16:
		return new(int16)
	case reflect.Int32:
		return new(int32)
	case reflect.Int64:
		return new(int64)
	case reflect.Uint:
		return new(uint)
	case reflect.Uint8:
		return new(uint8)
	case reflect.Uint16:
		return new(uint16)
	case reflect.Uint32:
		return new(uint32)
	case reflect.Uint64:
		return new(uint64)
	case reflect.Float32:
		return new(float32)
	case reflect.Float64:
		return new(float64)
	case reflect.String:
		return new(sql.NullString)
	case reflect.Bool:
		return new(bool)
	default:
		return new(interface{})
	}
}

func signedCountToUint64(count int64) uint64 {
	if count < 0 {
		return 0
	}
	return uint64(count)
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
	props := reflect.ValueOf(model)

	filters := m.getFiltersByKeyType(props, 1, "true", id)

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
	props := reflect.ValueOf(model)

	filters := m.getFiltersByKeyType(props, 2, keyGroup, uids...)

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
	props := reflect.ValueOf(model)

	filters := m.getFiltersByKeyType(props, 3, keyGroup, fids...)

	rows, _, err := m.Select(ctx, props.Interface(), 1, 0, filters, nil, datasrc)
	if err != nil {
		return nil, err
	}

	if len(rows) > 0 {
		return rows, nil
	}

	return nil, nil
}
