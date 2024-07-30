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

	"github.com/gofiber/fiber/v2/log"
	strcase "github.com/iancoleman/strcase"
	_ "github.com/lib/pq"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
)

func (m *dbCrud) genSelSqlStr(props reflect.Value, limit uint64, offset uint64, filters []sqldataenums.Filter, sorters []sqldataenums.Sorter, datasrc string) (int, string) {

	if datasrc == "" {
		propName := strcase.ToSnake(props.Type().Name())
		temp := strings.Replace(propName, "_entity", "", 1)
		if temp == propName {
			temp = strings.Replace(propName, "_model", "", 1)
		}
		datasrc = temp
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

	selSorters := m.genSortSqlStr(sorters)
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
			INNER JOIN (SELECT count(*) FROM cte) c(x_rows_cnt) ON true;
	
		`, res, rowLimit, rowOffset)

	if os.Getenv("ENVIRONMENT") == "dev" {
		log.Info(res)
	}

	return len(selCols), res
}

func (m *dbCrud) Select(ctx context.Context, model interface{}, limit uint64, offset uint64, filters []sqldataenums.Filter, sorters []sqldataenums.Sorter, datasrc string) ([]map[string]interface{}, uint64, error) {
	props := reflect.ValueOf(model)
	colCnt, sqlStr := m.genSelSqlStr(props, limit, offset, filters, sorters, datasrc)

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
	result := make([]map[string]interface{}, 0)

	for rows.Next() {
		if maxRowCnt != 0 && rowCnt >= maxRowCnt {
			break
		}

		rowCnt++
		vals := make([]interface{}, len(cols))
		for i := 0; i < len(cols); i++ {
			if cols[i] == "x_rows_cnt" {
				vals[i] = new(uint64)
				continue
			}
			switch props.Field(i).Interface().(type) {
			case []uint8:
				{
					vals[i] = new([]uint8)
					break
				}
			case int16:
				{
					vals[i] = new(int16)
					break
				}
			case uint16:
				{
					vals[i] = new(uint16)
					break
				}
			case int, int32:
				{
					vals[i] = new(int)
					break
				}
			case uint, uint32:
				{
					vals[i] = new(uint)
					break
				}
			case int64:
				{
					vals[i] = new(int64)
					break
				}
			case uint64:
				{
					vals[i] = new(uint64)
					break
				}
			case float32:
				{
					vals[i] = new(float32)
					break
				}
			case float64:
				{
					vals[i] = new(float64)
					break
				}
			case string:
				{
					vals[i] = new(string)
					break
				}
			case sql.NullString:
				{
					vals[i] = new(sql.NullString)
					break
				}
			case bool:
				{
					vals[i] = new(bool)
				}
			}
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
			data[field.Name] = vals[i]
		}

		if offset >= limit {
			rowCnt = *vals[len(vals)-1].(*uint64)
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
				if field.Type.Kind() == reflect.Slice {
					if field.Type.Elem().Kind() == reflect.Struct {
						cdatsrc := field.Tag.Get("datasrc")
						pkeys := strings.Split(field.Tag.Get("parents"), ",")

						var filters []sqldataenums.Filter
						for _, pkey := range pkeys {
							fkeys := strings.Split(pkey, ":")
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
		log.Info(result)
	}

	return result, rowCnt, nil
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

	rows, _, err := m.Select(ctx, props.Interface(), 0, 0, filters, nil, datasrc)
	if err != nil {
		return nil, err
	}

	if len(rows) > 0 {
		return rows, nil
	}

	return nil, nil
}
