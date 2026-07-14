package entities

// DeviceAttribute is the device twin: what we asked for (desired), and what the device says is
// actually true (reported).
//
// The gap between those two is the only honest way to answer "is the door locked?". "We sent a
// lock command" is not an answer. "The device reports it is locked" is.
//
// THE DESIRED STATE IS NOT A STANDING ORDER, and this is the hazard the twin pattern invites.
// The obvious implementation re-applies desired state whenever a device reconnects — that is
// what most IoT platforms do, and for a light bulb it is fine. Here it is dangerous: a door
// controller that has been offline for a month would come back and immediately apply a
// month-old "unlock" that somebody issued for thirty seconds during a delivery. The person who
// issued it has long gone home; nobody is watching; the door opens.
//
// So desired state EXPIRES (see DesiredExpiresAt), and an expired desire is not re-applied. A
// physical action needs a human who is present and intends it NOW. If the state still needs
// changing when the device comes back, an operator issues the command again — which takes five
// seconds and is exactly the moment a human should be in the loop anyway.
type DeviceAttribute struct {
	Id       int64  `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	DeviceId int64  `json:"deviceId" form:"deviceId" query:"deviceId" ukey:"dev_key" idx:"device" validate:"required"`
	Key      string `json:"key" form:"key" query:"key" ukey:"dev_key" validate:"required"`
	// Desired is what an operator asked for.
	Desired    float64 `json:"desired" form:"desired" query:"desired"`
	HasDesired bool    `json:"hasDesired" form:"hasDesired" query:"hasDesired"`
	DesiredAt  int64   `json:"desiredAt" form:"desiredAt" query:"desiredAt"`
	// DesiredExpiresAt is when the desire stops being actionable. Past it, the twin still SHOWS
	// the disagreement — an operator should see that what was asked never took effect — but
	// nothing acts on it. Silence about a failed intent would be worse than a stale one.
	DesiredExpiresAt int64 `json:"desiredExpiresAt" form:"desiredExpiresAt" query:"desiredExpiresAt"`
	// Reported is what the device last said is true. This is the ONLY field that describes
	// physical reality.
	Reported    float64 `json:"reported" form:"reported" query:"reported"`
	HasReported bool    `json:"hasReported" form:"hasReported" query:"hasReported"`
	ReportedAt  int64   `json:"reportedAt" form:"reportedAt" query:"reportedAt"`
}
