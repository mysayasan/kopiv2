package entities

// ReadingRollup is a downsampled bucket: count/min/max/sum/last of one key on one device
// over one span. It is what a chart of a month reads instead of a million raw rows, and it
// is what SURVIVES when retention purges the raw readings underneath it.
//
// Span is "1m" or "1h". The pattern — and the cursor that drives it — deliberately mirrors
// mymatasan's notification rollup: the shape is identical and that one is proven.
type ReadingRollup struct {
	Id       int64  `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	DeviceId int64  `json:"deviceId" form:"deviceId" query:"deviceId" idx:"dev_key_span" validate:"required"`
	Key      string `json:"key" form:"key" query:"key" idx:"dev_key_span,key"`
	// Span is the bucket width: "1m" or "1h".
	Span string `json:"span" form:"span" query:"span" idx:"dev_key_span,span"`
	// Bucket is the bucket's start time (unix seconds), floored to the span.
	Bucket int64   `json:"bucket" form:"bucket" query:"bucket" idx:"dev_key_span,bucket"`
	Count  int64   `json:"count" form:"count" query:"count"`
	Min    float64 `json:"min" form:"min" query:"min"`
	Max    float64 `json:"max" form:"max" query:"max"`
	Sum    float64 `json:"sum" form:"sum" query:"sum"`
	// Last is the final value in the bucket — the only meaningful summary for a STATE-like
	// key (a door's last position), where an average is nonsense.
	Last float64 `json:"last" form:"last" query:"last"`
}
