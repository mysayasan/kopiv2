package postgres

import (
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
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// dbCrud struct
type dbCrud struct {
	db *sql.DB
}

// Create new DbCrud
func NewDbCrud(config dbsql.DbConfigModel) (IDbCrud, error) {
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
	}, nil
}

func (m *dbCrud) genSqlStr(props reflect.Value, limit uint64, offset uint64, filters []dbsql.Filter, sorters []dbsql.Sorter, datasrc string) (int, string) {

	if datasrc == "" {
		propName := strcase.ToSnake(props.Type().Name())
		temp := strings.Replace(propName, "_entity", "", 1)
		if temp == propName {
			temp = strings.Replace(propName, "_model", "", 1)
		}
		datasrc = temp
	}

	selCols := []string{}

	for i := 0; i < props.NumField(); i++ {
		field := props.Type().Field(i)
		if field.Type.Kind() == reflect.Slice {
			if field.Type.Elem().Kind() == reflect.Struct {
				continue
			}
		}

		selCols = append(selCols, strcase.ToSnake(field.Name))
		// varName := props.Type().Field(i).Name
		// varType := props.Type().Field(i).Type
		// varValue := props.Field(i).Interface()
		// fmt.Printf("%v %v %v\n", varName, varType, varValue)
	}

	selSqlStr := strings.Join(selCols, `, `)
	res := fmt.Sprintf(`SELECT %s
	FROM %s`, selSqlStr, datasrc)

	selFilters := []string{}
	for _, filter := range filters {
		if filter.Compare == 1 {
			field := props.FieldByName(filter.FieldName)
			switch field.Interface().(type) {
			case int, int16, int32, int64, uint16, uint32, uint64:
				{
					fs := fmt.Sprintf("%s = %d", strcase.ToSnake(filter.FieldName), filter.Value)
					selFilters = append(selFilters, fs)
					break
				}
			default:
				{
					fs := fmt.Sprintf("%s = '%s'", strcase.ToSnake(filter.FieldName), filter.Value)
					selFilters = append(selFilters, fs)
					break
				}
			}
		}
	}

	if len(selFilters) > 0 {
		res = fmt.Sprintf(`
		%s
		WHERE %s
		`, res, strings.Join(selFilters, " AND "))
	}

	selSorters := []string{}
	for _, sorter := range sorters {
		// field := props.FieldByName(sorter.FieldName)
		if sorter.Sort == 2 {
			fs := fmt.Sprintf("%s DESC", strcase.ToSnake(sorter.FieldName))
			selSorters = append(selSorters, fs)
		} else {
			fs := fmt.Sprintf("%s ASC", strcase.ToSnake(sorter.FieldName))
			selSorters = append(selSorters, fs)
		}
	}

	if len(selSorters) > 0 {
		res = fmt.Sprintf(`
		%s
		ORDER BY %s
		`, res, strings.Join(selSorters, ","))
	}

	if os.Getenv("ENVIRONMENT") == "dev" {
		log.Info(res)
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

	return len(selCols), res
}

func (m *dbCrud) Get(props reflect.Value, limit uint64, offset uint64, filters []dbsql.Filter, sorters []dbsql.Sorter, datasrc string) ([]map[string]interface{}, uint64, error) {
	// props := reflect.ValueOf(model)
	colCnt, sqlStr := m.genSqlStr(props, limit, offset, filters, sorters, datasrc)

	rows, err := m.db.Query(sqlStr)
	if err != nil {
		return nil, 0, err
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
			case int:
				{
					vals[i] = new(int)
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

	if len(result) > 0 {
		var wg sync.WaitGroup
		for _, res := range result {
			for i := 0; i < props.NumField(); i++ {
				field := props.Type().Field(i)
				if field.Type.Kind() == reflect.Slice {
					if field.Type.Elem().Kind() == reflect.Struct {
						cdatsrc := field.Tag.Get("datasrc")
						pkeys := strings.Split(field.Tag.Get("parents"), ",")

						var filters []dbsql.Filter
						for _, pkey := range pkeys {
							fkeys := strings.Split(pkey, ":")
							var val interface{}
							switch props.FieldByName(fkeys[0]).Interface().(type) {
							case []uint8:
								{
									val = *res[fkeys[0]].(*[]uint8)
									break
								}
							case int:
								{
									val = *res[fkeys[0]].(*int)
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
							filters = append(filters, dbsql.Filter{
								FieldName: fkeys[1],
								Compare:   1,
								Value:     val,
							})
						}

						wg.Add(1)
						go func(props reflect.Value, filters []dbsql.Filter, cdatsrc string) {
							defer wg.Done()
							rows, _, err := m.Get(props, 0, 0, filters, nil, cdatsrc)
							if err == nil {
								res[field.Name] = rows
							}
						}(reflect.Indirect(reflect.New(field.Type.Elem())), filters, cdatsrc)
					}
				}
			}
		}
		wg.Wait()
	}

	return result, rowCnt, nil
}
