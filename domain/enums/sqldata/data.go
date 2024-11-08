package sqldataenums

// Paging
type Paging struct {
	Limit  uint64 `json:"imit" validate:"required"`
	Offset uint64 `json:"offset" validate:"required"`
}

// Filter
type Filter struct {
	FieldName string      `json:"fieldName" validate:"required"`
	Compare   Compare     `json:"compare" validate:"required"`
	Value     interface{} `json:"value" validate:"required"`
}

type Compare int

const (
	Equal                Compare = iota + 1 // EnumIndex = 1
	NotEqual                                // EnumIndex = 2
	GreaterThan                             // EnumIndex = 3
	LessThan                                // EnumIndex = 4
	GreaterThanOrEqualTo                    // EnumIndex = 5
	LessThanOrEqualTo                       // EnumIndex = 6
)

// Sorter
type Sorter struct {
	FieldName string `json:"fieldName" validate:"required"`
	Sort      Sort   `json:"sort" validate:"required"`
}

type Sort int

const (
	ASC  Sort = iota + 1 // EnumIndex = 1
	DESC                 // EnumIndex = 2
)

type DBKeyType int

const (
	Primary Sort = iota + 1 // EnumIndex = 1
	Unique                  // EnumIndex = 2
	Foreign                 // EnumIndex = 2
)
