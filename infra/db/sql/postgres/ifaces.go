package postgres

// IDbCrud interface
type IDbCrud interface {
	Get(model interface{}, dataset string, limit uint64, offset uint64) ([]map[string]interface{}, uint64, error)
}
