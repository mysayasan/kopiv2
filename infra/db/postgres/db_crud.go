package postgres

import (
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"strings"

	strcase "github.com/iancoleman/strcase"
	_ "github.com/lib/pq"
	dbconf "github.com/mysayasan/kopiv2/infra/db"
)

// dbCrud struct
type dbCrud struct {
	db *sql.DB
}

// Create new DbCrud
func NewDbCrud(config dbconf.DbConfigModel) (IDbCrud, error) {
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

func (m *dbCrud) Get(props reflect.Value, dataset string) ([]map[string]interface{}, uint64, error) {

	selCols := []string{}

	for i := 0; i < props.NumField(); i++ {
		selCols = append(selCols, strcase.ToSnake(props.Type().Field(i).Name))
		// varName := props.Type().Field(i).Name
		// varType := props.Type().Field(i).Type
		// varValue := props.Field(i).Interface()
		// fmt.Printf("%v %v %v\n", varName, varType, varValue)
	}

	selColsStr := strings.Join(selCols, `, `)

	rows, err := m.db.Query(fmt.Sprintf(`SELECT %s
	FROM resident_prop ORDER BY id`, selColsStr))
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

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
