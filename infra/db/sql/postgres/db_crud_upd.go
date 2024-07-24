package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"reflect"
	"strings"

	"github.com/gofiber/fiber/v2/log"
	strcase "github.com/iancoleman/strcase"
	_ "github.com/lib/pq"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
)

func (m *dbCrud) genUpdSqlStr(props reflect.Value, datasrc string, filters []sqldataenums.Filter) string {
	if props.Type().Kind() == reflect.Slice {
		log.Info("its a slice")
	}

	if datasrc == "" {
		propName := strcase.ToSnake(props.Type().Name())
		temp := strings.Replace(propName, "_entity", "", 1)
		if temp == propName {
			temp = strings.Replace(propName, "_model", "", 1)
		}
		datasrc = temp
	}

	values := []string{}

	for i := 0; i < props.NumField(); i++ {
		field := props.Type().Field(i)
		if field.Type.Kind() == reflect.Slice {
			if field.Type.Elem().Kind() == reflect.Struct {
				continue
			}
		}

		if field.Tag.Get("ignoreOnUpdate") == "true" || field.Tag.Get("pkey") == "true" {
			continue
		}

		selCol := strcase.ToSnake(field.Name)

		val := props.Field(i).Interface()
		switch val := val.(type) {
		case []uint8:
			{
				values = append(values, fmt.Sprintf("%s = %x", selCol, val))
				break
			}
		case int16, int, int32, int64:
			{
				values = append(values, fmt.Sprintf("%s = %d", selCol, val))
				break
			}
		case uint16, uint, uint32, uint64:
			{
				values = append(values, fmt.Sprintf("%s = %d", selCol, val))
				break
			}
		case float32:
			{
				values = append(values, fmt.Sprintf("%s = %f", selCol, val))
				break
			}
		case float64:
			{
				values = append(values, fmt.Sprintf("%s = %f", selCol, val))
				break
			}
		case string:
			{
				values = append(values, fmt.Sprintf("%s = '%s'", selCol, val))
				break
			}
		case sql.NullString:
			{
				if val.Valid {
					values = append(values, fmt.Sprintf("%s = '%s'", selCol, val.String))
				} else {
					values = append(values, "")
				}
				break
			}
		case bool:
			{
				values = append(values, fmt.Sprintf("%s = %t", selCol, val))
				break
			}
		}
	}

	res := fmt.Sprintf(`UPDATE %s SET %s`, datasrc, strings.Join(values, `, `))

	selFilters := m.genWhereSqlStr(props, filters)
	if len(selFilters) > 0 {
		res = fmt.Sprintf(`
		%s
		WHERE %s
		`, res, strings.Join(selFilters, " AND "))
	}

	res = fmt.Sprintf(`%s;`, res)

	if os.Getenv("ENVIRONMENT") == "dev" {
		log.Info(res)
	}

	return res
}

func (m *dbCrud) UpdateById(ctx context.Context, model interface{}, datasrc string) (uint64, error) {
	props := reflect.ValueOf(model)

	filters := m.getFiltersByKeyType(props, 1)

	if len(filters) < 1 {
		return 0, fmt.Errorf("update failed : cant find pkey or ukey in data fields")
	}

	sqlStr := m.genUpdSqlStr(props, datasrc, filters)

	var err error
	affect := int64(0)

	if m.tx != nil {
		res, err := m.tx.ExecContext(ctx, sqlStr)
		if err != nil {
			return 0, err
		}

		affect, err = res.RowsAffected()
		if err != nil {
			return 0, err
		}
	} else {
		res, err := m.db.ExecContext(ctx, sqlStr)
		if err != nil {
			return 0, err
		}

		affect, err = res.RowsAffected()
		if err != nil {
			return 0, err
		}
	}

	if affect < 1 {
		err = fmt.Errorf("weird  behaviour. total affected: %d", affect)
		return 0, err
	}

	return uint64(affect), nil
}

func (m *dbCrud) UpdateByUnique(ctx context.Context, model interface{}, datasrc string) (uint64, error) {
	props := reflect.ValueOf(model)

	filters := m.getFiltersByKeyType(props, 2)

	if len(filters) < 1 {
		return 0, fmt.Errorf("update failed : cant find pkey or ukey in data fields")
	}

	sqlStr := m.genUpdSqlStr(props, datasrc, filters)

	var err error
	affect := int64(0)

	if m.tx != nil {
		res, err := m.tx.ExecContext(ctx, sqlStr)
		if err != nil {
			return 0, err
		}

		affect, err = res.RowsAffected()
		if err != nil {
			return 0, err
		}
	} else {
		res, err := m.db.ExecContext(ctx, sqlStr)
		if err != nil {
			return 0, err
		}

		affect, err = res.RowsAffected()
		if err != nil {
			return 0, err
		}
	}

	if affect < 1 {
		err = fmt.Errorf("weird  behaviour. total affected: %d", affect)
		return 0, err
	}

	return uint64(affect), nil
}

func (m *dbCrud) UpdateByForeign(ctx context.Context, model interface{}, datasrc string) (uint64, error) {
	props := reflect.ValueOf(model)

	filters := m.getFiltersByKeyType(props, 3)

	if len(filters) < 1 {
		return 0, fmt.Errorf("update failed : cant find pkey or ukey in data fields")
	}

	sqlStr := m.genUpdSqlStr(props, datasrc, filters)

	var err error
	affect := int64(0)

	if m.tx != nil {
		res, err := m.tx.ExecContext(ctx, sqlStr)
		if err != nil {
			return 0, err
		}

		affect, err = res.RowsAffected()
		if err != nil {
			return 0, err
		}
	} else {
		res, err := m.db.ExecContext(ctx, sqlStr)
		if err != nil {
			return 0, err
		}

		affect, err = res.RowsAffected()
		if err != nil {
			return 0, err
		}
	}

	if affect < 1 {
		err = fmt.Errorf("weird  behaviour. total affected: %d", affect)
		return 0, err
	}

	return uint64(affect), nil
}
