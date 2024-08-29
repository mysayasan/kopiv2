package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"reflect"
	"strings"

	strcase "github.com/iancoleman/strcase"
	_ "github.com/lib/pq"
)

func (m *dbCrud) genInsSqlStr(props reflect.Value, datasrc string) string {
	if props.Type().Kind() == reflect.Slice {
		fmt.Println("its a slice")
	}

	if datasrc == "" {
		propName := strcase.ToSnake(props.Type().Name())
		temp := strings.Replace(propName, "_entity", "", 1)
		if temp == propName {
			temp = strings.Replace(propName, "_model", "", 1)
		}
		datasrc = temp
	}

	selCols := []string{}
	values := []string{}

	for i := 0; i < props.NumField(); i++ {
		field := props.Type().Field(i)
		if field.Type.Kind() == reflect.Slice {
			if field.Type.Elem().Kind() == reflect.Struct {
				continue
			}
		}

		if field.Tag.Get("ignoreOnInsert") == "true" {
			continue
		}

		selCols = append(selCols, strcase.ToSnake(field.Name))

		val := props.Field(i).Interface()
		switch val := val.(type) {
		case []uint8:
			{
				values = append(values, fmt.Sprintf("%x", val))
				break
			}
		case int16, int, int32, int64:
			{
				values = append(values, fmt.Sprintf("%d", val))
				break
			}
		case uint16, uint, uint32, uint64:
			{
				values = append(values, fmt.Sprintf("%d", val))
				break
			}
		case float32:
			{
				values = append(values, fmt.Sprintf("%f", val))
				break
			}
		case float64:
			{
				values = append(values, fmt.Sprintf("%f", val))
				break
			}
		case string:
			{
				values = append(values, fmt.Sprintf("'%s'", val))
				break
			}
		case sql.NullString:
			{
				if val.Valid {
					values = append(values, fmt.Sprintf("'%s'", val.String))
				} else {
					values = append(values, "")
				}
				break
			}
		case bool:
			{
				values = append(values, fmt.Sprintf("%t", val))
				break
			}
		}
	}

	res := fmt.Sprintf(`INSERT INTO %s (%s) VALUES (%s) RETURNING id`, datasrc, strings.Join(selCols, `, `), strings.Join(values, `, `))

	if os.Getenv("ENVIRONMENT") == "dev" {
		fmt.Println(res)
	}

	return res
}

func (m *dbCrud) Insert(ctx context.Context, model interface{}, datasrc string) (uint64, error) {
	props := reflect.ValueOf(model)
	sqlStr := m.genInsSqlStr(props, datasrc)

	var err error
	lastid := 0

	if m.tx != nil {
		err = m.tx.QueryRowContext(ctx, sqlStr).Scan(&lastid)
		if err != nil {
			return 0, err
		}
	} else {
		err = m.db.QueryRowContext(ctx, sqlStr).Scan(&lastid)
		if err != nil {
			return 0, err
		}
	}

	return uint64(lastid), nil
}
