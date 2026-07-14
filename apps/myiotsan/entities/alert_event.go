package entities

// AlertEvent is one firing of a rule: what tripped, on which device, at what value, when.
//
// It carries the READING CONTEXT (Key, Value, Message) rather than a snapshot path, which is
// the only real difference from mymatasan's alert_event. An operator asking "why did this
// fire?" three weeks later gets the actual number, not a rule name and a shrug.
//
// The (device, time) index is what the alert log and the device detail page both read.
type AlertEvent struct {
	Id       int64 `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	RuleId   int64 `json:"ruleId" form:"ruleId" query:"ruleId" idx:"rule"`
	DeviceId int64 `json:"deviceId" form:"deviceId" query:"deviceId" idx:"dev_time"`
	// RuleName and DeviceName are denormalized so an alert stays readable after the rule or
	// the device it names is deleted. An alert log with dangling references is not evidence.
	RuleName   string `json:"ruleName" form:"ruleName" query:"ruleName"`
	DeviceName string `json:"deviceName" form:"deviceName" query:"deviceName"`
	Key        string `json:"key" form:"key" query:"key"`
	// Value is the reading that tripped the rule.
	Value float64 `json:"value" form:"value" query:"value"`
	// Message is the human sentence ("Cold store temperature 8.4C is above 5C").
	Message  string `json:"message" form:"message" query:"message"`
	Severity string `json:"severity" form:"severity" query:"severity"`
	// AckedAt / AckedBy record the operator acknowledging the alert. Acknowledging is an
	// operator power; DELETING is not — that line is the same evidentiary one mymatasan draws.
	AckedAt   int64 `json:"ackedAt" form:"ackedAt" query:"ackedAt"`
	AckedBy   int64 `json:"ackedBy" form:"ackedBy" query:"ackedBy"`
	CreatedAt int64 `json:"createdAt" form:"createdAt" query:"createdAt" idx:"dev_time,time"`
}
