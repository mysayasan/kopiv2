package postgres

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strings"

	"github.com/gofiber/fiber/v2/log"
	strcase "github.com/iancoleman/strcase"
	_ "github.com/lib/pq"
	"github.com/mysayasan/kopiv2/domain/enums/data"
)

func (m *dbCrud) genDelSqlStr(props reflect.Value, datasrc string, filters []data.Filter) string {
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

	selFilters := m.genWhereSqlStr(props, filters)

	var res = ""
	if len(selFilters) > 0 {
		res = fmt.Sprintf(`DELETE FROM %s WHERE %s;`, datasrc, strings.Join(selFilters, " AND "))
	}

	if os.Getenv("ENVIRONMENT") == "dev" {
		log.Info(res)
	}

	return res
}

func (m *dbCrud) Delete(ctx context.Context, model interface{}, datasrc string, delByUKey bool) (uint64, error) {
	props := reflect.ValueOf(model)

	filters := make([]data.Filter, 0)
	for i := 0; i < props.NumField(); i++ {
		field := props.Type().Field(i)
		if field.Type.Kind() == reflect.Slice {
			if field.Type.Elem().Kind() == reflect.Struct {
				continue
			}
		}

		if delByUKey {
			if field.Tag.Get("ukey") == "true" {
				filter := data.Filter{
					FieldName: field.Name,
					Compare:   1,
					Value:     props.Field(i).Interface(),
				}
				filters = append(filters, filter)
			}
		} else {
			if field.Tag.Get("pkey") == "true" {
				filter := data.Filter{
					FieldName: field.Name,
					Compare:   1,
					Value:     props.Field(i).Interface(),
				}
				filters = append(filters, filter)
			}
		}
	}

	if len(filters) < 1 {
		return 0, fmt.Errorf("delete failed : cant find pkey or ukey in data fields")
	}

	sqlStr := m.genDelSqlStr(props, datasrc, filters)

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
