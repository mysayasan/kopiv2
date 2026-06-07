package entities

// OperationJob stores durable async operation state for worker recovery.
type OperationJob struct {
	Id             int64  `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	Type           string `json:"type" form:"type" query:"type"`
	ResourceKey    string `json:"resourceKey" form:"resourceKey" query:"resourceKey"`
	IdempotencyKey string `json:"idempotencyKey" form:"idempotencyKey" query:"idempotencyKey" ukey:"idempotency"`
	Status         string `json:"status" form:"status" query:"status"`
	Attempt        int64  `json:"attempt" form:"attempt" query:"attempt"`
	MaxAttempts    int64  `json:"maxAttempts" form:"maxAttempts" query:"maxAttempts"`
	LockedBy       string `json:"lockedBy" form:"lockedBy" query:"lockedBy"`
	Payload        string `json:"payload" form:"payload" query:"payload"`
	Result         string `json:"result" form:"result" query:"result"`
	LastError      string `json:"lastError" form:"lastError" query:"lastError"`
	StartedAt      int64  `json:"startedAt" form:"startedAt" query:"startedAt"`
	DeadlineAt     int64  `json:"deadlineAt" form:"deadlineAt" query:"deadlineAt"`
	CompletedAt    int64  `json:"completedAt" form:"completedAt" query:"completedAt"`
	CreatedAt      int64  `json:"createdAt" form:"createdAt" query:"createdAt" ignoreOnUpdate:"true"`
	UpdatedAt      int64  `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
}
