package postgres

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"

	"github.com/gofiber/fiber/v2/log"
	strcase "github.com/iancoleman/strcase"
	_ "github.com/lib/pq"
	sqldb "github.com/mysayasan/kopiv2/infra/db/sql"
)

// dbCrud struct
type dbCrud struct {
	db *sql.DB
}

// Create new DbCrud
func NewDbCrud(config sqldb.DbConfigModel) (IDbCrud, error) {
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

func (m *dbCrud) genSqlStr(props reflect.Value, dataset []string, limit uint64, offset uint64, filters []sqldb.Filter, sorters []sqldb.Sorter) (int, string) {
	selCols := []string{}
	cSqlStr := []string{}

	fmt.Printf(">>>>%s ", props.Kind())

	for i := 0; i < props.NumField(); i++ {
		field := props.Type().Field(i)
		if field.Type.Kind() == reflect.Slice {
			// log.Info(r.Elem().Kind())
			if len(dataset) > 1 {
				// fmt.Printf(" >>>> %s", field.Tag.Get("parent"))
				// fmt.Printf(" >>>> %s", field.Type.Elem())
				// fmt.Printf(" >>>> %s", field.Type.Kind())
				_, sqlStr := m.genSqlStr(reflect.Indirect(reflect.New(field.Type.Elem())), dataset[1:], limit, offset, nil, nil)
				cSqlStr = append(cSqlStr, sqlStr)
				// fmt.Printf(" >>>> %d <<< %s\n", cSelCols, cSqlStr)
			}
			continue
		}

		selCols = append(selCols, strcase.ToSnake(props.Type().Field(i).Name))
		// varName := props.Type().Field(i).Name
		// varType := props.Type().Field(i).Type
		// varValue := props.Field(i).Interface()
		// fmt.Printf("%v %v %v\n", varName, varType, varValue)
	}

	selSqlStr := strings.Join(selCols, `, `)
	res := fmt.Sprintf(`SELECT %s
	FROM %s`, selSqlStr, dataset[0])

	selFilters := []string{}
	for _, filter := range filters {
		if filter.Compare == 1 {
			// rv := reflect.ValueOf(filter.Value)
			switch props.Field(filter.FieldIdx).Interface().(type) {
			case int, int16, int32, int64, uint16, uint32, uint64:
				{
					fs := fmt.Sprintf("%s = %d", strcase.ToSnake(props.Type().Field(filter.FieldIdx).Name), filter.Value)
					selFilters = append(selFilters, fs)
					break
				}
			default:
				{
					fs := fmt.Sprintf("%s = '%s'", strcase.ToSnake(props.Type().Field(filter.FieldIdx).Name), filter.Value)
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
		if sorter.Sort == 2 {
			fs := fmt.Sprintf("%s DESC", strcase.ToSnake(props.Type().Field(sorter.FieldIdx).Name))
			selSorters = append(selSorters, fs)
		} else {
			fs := fmt.Sprintf("%s ASC", strcase.ToSnake(props.Type().Field(sorter.FieldIdx).Name))
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

	for _, sqlStr := range cSqlStr {
		res = fmt.Sprintf("%s\n%s", res, sqlStr)
	}

	log.Info(res)

	return len(selCols), res
}

func (m *dbCrud) Get(model interface{}, dataset []string, limit uint64, offset uint64, filters []sqldb.Filter, sorters []sqldb.Sorter) ([]map[string]interface{}, uint64, error) {
	props := reflect.ValueOf(model)
	colCnt, sqlStr := m.genSqlStr(props, dataset, limit, offset, filters, sorters)

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
			data[props.Type().Field(i).Name] = vals[i]
		}

		if offset >= limit {
			rowCnt = *vals[len(vals)-1].(*uint64)
		}

		result = append(result, data)
	}

	return result, rowCnt, nil
}
