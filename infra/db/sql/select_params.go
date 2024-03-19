package sql

// DbConfig
type PagingParams struct {
	Limit  uint64 `json:"imit" validate:"required"`
	Offset uint64 `json:"offset" validate:"required"`
}
