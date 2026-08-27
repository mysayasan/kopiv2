package entities

// DeviceCommand is one attempt to make a device DO something — and it is the audit record of
// that attempt, not merely a queue entry. A row here is the answer to "who unlocked that door,
// when, and did it actually open?", which is the question somebody will eventually ask.
//
// Actuation is the point where a bug stops being a wrong number on a chart and becomes a relay
// that physically fires. Everything about this table is shaped by that:
//
//   - It records the ACTOR. A command with no name attached to it is not an audit trail.
//   - It records what was ASKED and what came BACK, separately. "I sent it" and "it happened"
//     are different facts, and conflating them is how an operator ends up believing a door is
//     locked when it is not.
//   - A command that is never confirmed becomes FAILED, and STAYS failed. It is never silently
//     retried — see Status.
type DeviceCommand struct {
	Id       int64 `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	DeviceId int64 `json:"deviceId" form:"deviceId" query:"deviceId" idx:"dev_time" validate:"required"`
	// DeviceName is denormalized so the audit trail survives the device being deleted. An audit
	// log with dangling references is not evidence.
	DeviceName string `json:"deviceName" form:"deviceName" query:"deviceName"`
	// Name is the command the profile declares ("relay", "setpoint").
	Name string `json:"name" form:"name" query:"name" validate:"required"`
	// Value is what was asked for.
	Value float64 `json:"value" form:"value" query:"value"`
	// Status is the lifecycle:
	//
	//	pending    accepted, not yet published
	//	sent       published to the device
	//	confirmed  the device REPORTED BACK, on the telemetry key this command's profile declares
	//	           as its ConfirmKey, the state we asked for — the only status that means the
	//	           physical thing actually happened. A report on any OTHER key confirms nothing,
	//	           however well the number matches: two commands on one controller routinely carry
	//	           the same value (a lock and a fan are both switches), and letting one device's
	//	           answer speak for a command it said nothing about would invent an actuation and
	//	           lose a failure in the same stroke.
	//	failed     refused, never confirmed within the window, or INTERRUPTED — a row left pending
	//	           by a process that stopped between recording the command and sending it is ended
	//	           by the same sweep. Nothing may sit in a non-terminal state forever: "we never
	//	           retry" is only safe because a human is handed the decision, and a command nobody
	//	           is ever told about is a silent drop rather than a safe refusal.
	//
	// A failed command is NEVER retried automatically. Re-sending a relay write is a SECOND
	// PHYSICAL ACTION: a retry that "succeeds" after a timeout may have fired the relay twice,
	// and nobody can tell from here whether the first one landed. So a timeout ends the command,
	// tells the operator plainly that it was not confirmed, and makes THEM decide whether to
	// send it again. An automatic retry is a convenience that can open a door twice.
	Status string `json:"status" form:"status" query:"status" idx:"status"`
	// Error explains a refusal or a timeout in words an operator can act on.
	Error string `json:"error" form:"error" query:"error"`
	// RequestedBy is the local user id who issued it, and RequestedByName is who they WERE.
	//
	// The name is not redundant. A command can arrive over the FLEET TUNNEL from an operator on
	// the control plane, and that operator has no account on this node — their id here is 0. If
	// only the id were recorded, the node's own command log (the first place an investigator
	// looks) would say a relay was switched by "System". It was not: it was switched by a person,
	// and the tunnel already carries their name as "cp:<who>". Record it.
	RequestedBy     int64  `json:"requestedBy" form:"requestedBy" query:"requestedBy"`
	RequestedByName string `json:"requestedByName" form:"requestedByName" query:"requestedByName"`
	RequestedAt     int64  `json:"requestedAt" form:"requestedAt" query:"requestedAt" idx:"dev_time,time"`
	SentAt          int64  `json:"sentAt" form:"sentAt" query:"sentAt"`
	// ConfirmedAt is when the device reported the state back. Zero means it never did — and an
	// unconfirmed command must never be displayed as if it succeeded.
	ConfirmedAt int64 `json:"confirmedAt" form:"confirmedAt" query:"confirmedAt"`
}
