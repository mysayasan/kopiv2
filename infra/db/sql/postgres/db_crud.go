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
	"github.com/mysayasan/kopiv2/domain/enums/data"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// dbCrud struct
type dbCrud struct {
	db *sql.DB
	tx *sql.Tx
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
		tx: nil,
	}, nil
}

func (m *dbCrud) genInsSqlStr(props reflect.Value, datasrc string) string {
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

	selCols := []string{}
	values := []string{}

	for i := 0; i < props.NumField(); i++ {
		field := props.Type().Field(i)
		if field.Type.Kind() == reflect.Slice {
			if field.Type.Elem().Kind() == reflect.Struct {
				continue
			}
		}

		if field.Tag.Get("autoinc") == "true" {
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

	res := fmt.Sprintf(`INSERT INTO %s (%s) VALUES (%s)`, datasrc, strings.Join(selCols, `, `), strings.Join(values, `, `))

	return res
}

func (m *dbCrud) Add(ctx context.Context, model interface{}, datasrc string) (uint64, error) {
	props := reflect.ValueOf(model)
	sqlStr := m.genInsSqlStr(props, datasrc)

	log.Info(sqlStr)

	var res sql.Result
	var err error

	if m.tx != nil {
		res, err = m.tx.ExecContext(ctx, sqlStr)
		if err != nil {
			return 0, err
		}
	} else {
		res, err = m.db.ExecContext(ctx, sqlStr)
		if err != nil {
			return 0, err
		}
	}

	lastid, err := res.LastInsertId()
	if err != nil {
		lastid = 0
	}

	return uint64(lastid), nil
}

func (m *dbCrud) genSelSqlStr(props reflect.Value, limit uint64, offset uint64, filters []data.Filter, sorters []data.Sorter, datasrc string) (int, string) {

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
			case int, int16, int32, int64, uint, uint16, uint32, uint64:
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

func (m *dbCrud) Get(ctx context.Context, model interface{}, limit uint64, offset uint64, filters []data.Filter, sorters []data.Sorter, datasrc string) ([]map[string]interface{}, uint64, error) {
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

	if len(result) > 0 {
		var wg sync.WaitGroup
		for _, res := range result {
			for i := 0; i < props.NumField(); i++ {
				field := props.Type().Field(i)
				if field.Type.Kind() == reflect.Slice {
					if field.Type.Elem().Kind() == reflect.Struct {
						cdatsrc := field.Tag.Get("datasrc")
						pkeys := strings.Split(field.Tag.Get("parents"), ",")

						var filters []data.Filter
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
							filters = append(filters, data.Filter{
								FieldName: fkeys[1],
								Compare:   1,
								Value:     val,
							})
						}

						wg.Add(1)
						go func(res map[string]interface{}, props reflect.Value, filters []data.Filter, cdatsrc string) {
							defer wg.Done()
							rows, _, err := m.Get(ctx, props.Interface(), 0, 0, filters, nil, cdatsrc)
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

	return result, rowCnt, nil
}

func (m *dbCrud) GetSingle(ctx context.Context, model interface{}, filters []data.Filter, datasrc string) (map[string]interface{}, error) {
	props := reflect.ValueOf(model)
	rows, _, err := m.Get(ctx, props.Interface(), 1, 0, filters, nil, datasrc)
	if err != nil {
		return nil, err
	}

	if len(rows) > 0 {
		return rows[0], nil
	}

	return nil, nil
}

func (m *dbCrud) BeginTx(ctx context.Context) error {
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	m.tx = tx
	return nil
}

func (m *dbCrud) RollbackTx() error {
	err := m.tx.Rollback()
	if err != nil {
		return err
	}
	return nil
}

func (m *dbCrud) CommitTx() error {
	err := m.tx.Commit()
	if err != nil {
		return err
	}
	return nil
}
