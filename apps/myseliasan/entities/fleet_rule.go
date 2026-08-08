package entities

// FleetRule correlates events across DIFFERENT NODES — and that is the reason the fourth app
// exists.
//
//	Motion on Camera 3   (a mymatasan node)
//	AND a door contact opening   (a myiotsan node)
//	AND no badge swipe   (a myiotsan node)
//	-> intrusion
//
// No node can see that. mymatasan cannot see your door sensors; myiotsan cannot see your
// cameras; a cloud IoT platform cannot see either. Only the control plane — which already
// receives every node's events in one feed — is in a position to notice the conjunction.
//
// A single camera's motion alert at 03:00 is noise: it fires for a moth, a spider, headlights
// through a window. A door contact opening at 03:00 is noise: cleaners, deliveries, wind. The
// two together, with no badge swipe, is not noise. Correlation is how a fleet of noisy sensors
// becomes one trustworthy signal, and it is worth more than any of the sensors.
type FleetRule struct {
	Id      int64  `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	Name    string `json:"name" form:"name" query:"name" validate:"required"`
	Enabled bool   `json:"enabled" form:"enabled" query:"enabled"`
	// WindowSeconds is how close in time the required events must be. It is the difference
	// between "a door opened and, separately, a camera saw motion last Tuesday" and one event.
	WindowSeconds int `json:"windowSeconds" form:"windowSeconds" query:"windowSeconds"`
	// GraceSeconds is how long to WAIT, after the required events have all arrived, before
	// concluding that an "absent" clause really is absent.
	//
	// THIS FIELD IS THE DIFFERENCE BETWEEN A USEFUL PRODUCT AND A NUISANCE. The canonical rule is
	// "door opened AND no badge swipe". But a badge reader reports over a network, through a
	// controller, into a hub — it is routinely a second or two behind the door contact it
	// authorises. Fire the instant the door opens and the rule cries intrusion on EVERY
	// legitimate badge entry, all day, until somebody turns it off. Waiting a few seconds to see
	// whether the swipe shows up costs nothing and is the whole difference.
	GraceSeconds int `json:"graceSeconds" form:"graceSeconds" query:"graceSeconds"`
	// CooldownSeconds suppresses re-firing, and survives restart via LastTriggeredAt — the same
	// bug mymatasan shipped and myiotsan carries a regression test for.
	CooldownSeconds int    `json:"cooldownSeconds" form:"cooldownSeconds" query:"cooldownSeconds"`
	Severity        string `json:"severity" form:"severity" query:"severity"`
	LastTriggeredAt int64  `json:"lastTriggeredAt" form:"lastTriggeredAt" query:"lastTriggeredAt"`
	CreatedBy       int64  `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedAt       int64  `json:"createdAt" form:"createdAt" query:"createdAt"`
	UpdatedBy       int64  `json:"updatedBy" form:"updatedBy" query:"updatedBy"`
	UpdatedAt       int64  `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
}

// FleetRuleClause is one condition in a correlation rule: an event that must be present, or one
// that must be absent.
type FleetRuleClause struct {
	Id     int64 `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	RuleId int64 `json:"ruleId" form:"ruleId" query:"ruleId" idx:"rule" validate:"required"`
	// Mode is "required" (this must happen) or "absent" (this must NOT have happened).
	//
	// The absent clause is what makes a rule express INNOCENCE as well as guilt. Without it, "the
	// door opened at 03:00" is an alert every night a cleaner works late. With it, the rule says
	// what everyone actually means: the door opened and nobody was authorised to open it.
	Mode string `json:"mode" form:"mode" query:"mode"`
	// NodeId scopes the clause to one node. Empty means any node — which is what you want for
	// "a badge swipe anywhere on this site".
	NodeId string `json:"nodeId" form:"nodeId" query:"nodeId"`
	// Kind scopes to a node TYPE ("camera", "iot", "door"). Empty means any.
	Kind string `json:"kind" form:"kind" query:"kind"`
	// Category matches the event's notification category ("vision.alert", "device.alert").
	Category string `json:"category" form:"category" query:"category"`
	// Match is a case-insensitive substring matched against the event's title and body — the rule
	// name that fired on the node, in practice ("Person detected", "Front door opened").
	//
	// It is a substring rather than a structured field because a node's alert IS its rule's name,
	// and the operator who wrote that rule is the one writing this one. Making them declare a
	// taxonomy first would mean the feature is never used.
	Match string `json:"match" form:"match" query:"match"`
}
