package postgres

import (
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"strings"

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

func (m *dbCrud) genSqlStr(props reflect.Value, dataset string, limit uint64, offset uint64) string {
	selCols := []string{}

	for i := 0; i < props.NumField(); i++ {
		selCols = append(selCols, strcase.ToSnake(props.Type().Field(i).Name))
		// varName := props.Type().Field(i).Name
		// varType := props.Type().Field(i).Type
		// varValue := props.Field(i).Interface()
		// fmt.Printf("%v %v %v\n", varName, varType, varValue)
	}

	selSqlStr := strings.Join(selCols, `, `)
	res := fmt.Sprintf(`SELECT %s
	FROM %s`, selSqlStr, dataset)

	if limit > 0 {
		res = fmt.Sprintf("%s LIMIT %d", res, limit)
	}

	if offset > 0 {
		res = fmt.Sprintf("%s OFFSET %d", res, offset)
	}

	return res
}

func (m *dbCrud) Get(model interface{}, dataset string, limit uint64, offset uint64) ([]map[string]interface{}, uint64, error) {
	props := reflect.ValueOf(model)
	sqlStr := m.genSqlStr(props, dataset, limit, offset)

	rows, err := m.db.Query(sqlStr)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, 0, err
	}

	if len(cols) != props.NumField() {
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
			// vals[i] = reflect.New(props.Type().Field(i).Type.Elem())
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

		result = append(result, data)
	}

	return result, rowCnt, nil
}
