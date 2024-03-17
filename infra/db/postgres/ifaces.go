package postgres

import "reflect"

// IDbCrud interface
type IDbCrud interface {
	Get(props reflect.Value, dataset string) ([]map[string]interface{}, uint64, error)
}
