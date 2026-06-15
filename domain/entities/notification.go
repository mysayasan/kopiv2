package entities

// Notification is the unified, persisted record every event source funnels into.
// It is shared so any app can reuse one notification feed (vision alerts, health
// checks, system events, ...). Metadata holds arbitrary JSON context such as a
// detection bounding box, zone polygon, or confidence.
type Notification struct {
	Id        int64  `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	Category  string `json:"category" form:"category" query:"category"`
	Severity  string `json:"severity" form:"severity" query:"severity"`
	Title     string `json:"title" form:"title" query:"title"`
	Body      string `json:"body" form:"body" query:"body"`
	Source    string `json:"source" form:"source" query:"source"`
	CameraId  int64  `json:"cameraId" form:"cameraId" query:"cameraId"`
	RefType   string `json:"refType" form:"refType" query:"refType"`
	RefId     int64  `json:"refId" form:"refId" query:"refId"`
	Link      string `json:"link" form:"link" query:"link"`
	Metadata  string `json:"metadata" form:"metadata" query:"metadata"`
	IsRead    bool   `json:"isRead" form:"isRead" query:"isRead"`
	ReadBy    int64  `json:"readBy" form:"readBy" query:"readBy"`
	ReadAt    int64  `json:"readAt" form:"readAt" query:"readAt"`
	CreatedBy int64  `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedAt int64  `json:"createdAt" form:"createdAt" query:"createdAt"`
	UpdatedBy int64  `json:"updatedBy" form:"updatedBy" query:"updatedBy"`
	UpdatedAt int64  `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
}
