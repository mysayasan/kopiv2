package entities

// The access rules are the classic triple — group, schedule, grant — kept deliberately boring.
// Every clever access-control data model eventually has to answer "why did this door open?", and
// the boring one answers it in a single join.

// AccessGroup is a named set of holders.
type AccessGroup struct {
	Id          int64  `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	Name        string `json:"name" form:"name" query:"name" ukey:"name" validate:"required"`
	Description string `json:"description" form:"description" query:"description"`
	// SsoGroup, when set, mirrors membership from a myidsan group. READ-ONLY and ONE DIRECTION:
	// myidsan may drive access, access never drives myidsan.
	SsoGroup  string `json:"ssoGroup" form:"ssoGroup" query:"ssoGroup"`
	CreatedBy int64  `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedAt int64  `json:"createdAt" form:"createdAt" query:"createdAt"`
}

// AccessGroupMember joins a holder to a group.
type AccessGroupMember struct {
	Id       int64 `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	GroupId  int64 `json:"groupId" form:"groupId" query:"groupId" idx:"group" validate:"required"`
	HolderId int64 `json:"holderId" form:"holderId" query:"holderId" idx:"holder" validate:"required"`
	AddedBy  int64 `json:"addedBy" form:"addedBy" query:"addedBy"`
	AddedAt  int64 `json:"addedAt" form:"addedAt" query:"addedAt"`
}

// Schedule is a named time policy.
type Schedule struct {
	Id   int64  `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	Name string `json:"name" form:"name" query:"name" ukey:"name" validate:"required"`
	// Always marks the 24/7 schedule. It exists as a flag rather than as 7 windows of 0–1440 so
	// that "this grant has no time restriction" reads that way in the UI and in an audit export.
	Always    bool  `json:"always" form:"always" query:"always"`
	CreatedBy int64 `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedAt int64 `json:"createdAt" form:"createdAt" query:"createdAt"`
}

// ScheduleWindow is one weekly interval within a schedule.
type ScheduleWindow struct {
	Id         int64 `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	ScheduleId int64 `json:"scheduleId" form:"scheduleId" query:"scheduleId" idx:"schedule" validate:"required"`
	// Weekday is 0-6, Sunday-first, matching time.Weekday so no conversion table is needed.
	Weekday int `json:"weekday" form:"weekday" query:"weekday"`
	// StartMin and EndMin are minutes from midnight. A window that ends BEFORE it starts wraps
	// past midnight — the night-shift case, which a naive start<=now<=end comparison gets wrong
	// every time.
	StartMin int `json:"startMin" form:"startMin" query:"startMin"`
	EndMin   int `json:"endMin" form:"endMin" query:"endMin"`
}

// Holiday is its own entity on purpose: Malaysian public holidays vary BY STATE, so a site needs
// its own calendar rather than a hardcoded national one — and a site with offices in two states
// needs two.
type Holiday struct {
	Id   int64  `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	Name string `json:"name" form:"name" query:"name" validate:"required"`
	// Date is YYYY-MM-DD in the site's local timezone. A date rather than a timestamp because a
	// public holiday is a calendar day, not an instant, and it must not shift with UTC offset.
	Date string `json:"date" form:"date" query:"date" idx:"date" validate:"required"`
	// SiteId scopes the calendar; 0 applies to every site.
	SiteId int64 `json:"siteId" form:"siteId" query:"siteId" idx:"site"`
	// Behaviour is deny | follow-sunday | ignore.
	Behaviour string `json:"behaviour" form:"behaviour" query:"behaviour"`
	CreatedBy int64  `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedAt int64  `json:"createdAt" form:"createdAt" query:"createdAt"`
}

// Holiday behaviours.
const (
	HolidayDeny         = "deny"
	HolidayFollowSunday = "follow-sunday"
	HolidayIgnore       = "ignore"
)

// Grant is the ACL row: this group, through this door, during this schedule.
type Grant struct {
	Id         int64 `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	GroupId    int64 `json:"groupId" form:"groupId" query:"groupId" idx:"group" validate:"required"`
	DoorId     int64 `json:"doorId" form:"doorId" query:"doorId" idx:"door" validate:"required"`
	ScheduleId int64 `json:"scheduleId" form:"scheduleId" query:"scheduleId" idx:"schedule" validate:"required"`
	CreatedBy  int64 `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedAt  int64 `json:"createdAt" form:"createdAt" query:"createdAt"`
}
