package postgres

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strings"

	strcase "github.com/iancoleman/strcase"
	_ "github.com/lib/pq"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
)

func (m *dbCrud) genDelSqlStr(props reflect.Value, datasrc string, filters []sqldataenums.Filter) string {
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

	selFilters := m.genWhereSqlStr(props, filters)

	var res = ""
	if len(selFilters) > 0 {
		res = fmt.Sprintf(`DELETE FROM %s WHERE %s;`, datasrc, strings.Join(selFilters, " AND "))
	}

	if os.Getenv("ENVIRONMENT") == "dev" {
		fmt.Println(res)
	}

	return res
}

func (m *dbCrud) DeleteById(ctx context.Context, model interface{}, datasrc string, id uint64) (uint64, error) {
	props := reflect.ValueOf(model)

	filters := m.getFiltersByKeyType(props, 1, "true", id)

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

func (m *dbCrud) DeleteByUnique(ctx context.Context, model interface{}, datasrc string, keyGroup string, uids ...any) (uint64, error) {
	props := reflect.ValueOf(model)

	filters := m.getFiltersByKeyType(props, 2, keyGroup, uids...)

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

func (m *dbCrud) DeleteByForeign(ctx context.Context, model interface{}, datasrc string, keyGroup string, fids ...any) (uint64, error) {
	props := reflect.ValueOf(model)

	filters := m.getFiltersByKeyType(props, 3, keyGroup, fids...)

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
