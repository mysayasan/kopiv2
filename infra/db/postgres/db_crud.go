package postgres

import (
	"database/sql"
	"fmt"
	"reflect"

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
	for i := 0; i < props.NumField(); i++ {
		varName := props.Type().Field(i).Name
		varType := props.Type().Field(i).Type
		varValue := props.Field(i).Interface()
		fmt.Printf("%v %v %v\n", varName, varType, varValue)
	}
	return nil, 0, nil
}
