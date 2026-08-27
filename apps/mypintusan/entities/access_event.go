package entities

// AccessEvent is the append-only record of every access decision, modelled on myidsan's audit log.
//
// EVERY decision is recorded, including denials and unknown cards. A denied unknown credential at
// 03:00 on a perimeter door is the single most valuable row this table will ever hold, and a system
// that logs only successes cannot produce it.
type AccessEvent struct {
	Id       int64 `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	At       int64 `json:"at" form:"at" query:"at" idx:"at" validate:"required"`
	DoorId   int64 `json:"doorId" form:"doorId" query:"doorId" idx:"door"`
	ReaderId int64 `json:"readerId" form:"readerId" query:"readerId"`

	// CredentialId and HolderId are nullable: an unknown card has neither.
	CredentialId int64 `json:"credentialId" form:"credentialId" query:"credentialId"`
	HolderId     int64 `json:"holderId" form:"holderId" query:"holderId" idx:"holder"`
	// HolderName is DENORMALISED on purpose. An access log must still read correctly years after a
	// holder record is deleted or renamed, and a report that says "holder 4471" helps nobody.
	HolderName string `json:"holderName" form:"holderName" query:"holderName"`

	// RawCredential is the value as presented. Stored even — especially — when it matches nothing,
	// so an operator can enrol it or investigate it. It is what turns "access denied" into "this
	// specific card was tried at this door".
	RawCredential string `json:"rawCredential" form:"rawCredential" query:"rawCredential"`

	// Decision is granted | denied.
	Decision string `json:"decision" form:"decision" query:"decision" idx:"decision" validate:"required"`
	// Reason is the machine-readable why, enumerated because "denied" on its own is useless at 3am.
	Reason string `json:"reason" form:"reason" query:"reason" idx:"reason"`
	// Detail is free text for the operator, never parsed.
	Detail string `json:"detail" form:"detail" query:"detail"`

	// Duress records that access was granted under a duress PIN. The event is stored normally and
	// the alarm is raised out of band; nothing about the reader's behaviour may differ, or the
	// coercer learns the alarm was sent.
	Duress bool `json:"duress" form:"duress" query:"duress"`
	// Offline records that the decision was served from cache rather than live data.
	Offline bool `json:"offline" form:"offline" query:"offline"`

	// SnapshotRef and ContactRef are the correlation hooks: the image mymatasan captured at this
	// door at this instant, and the myiotsan contact event confirming the door actually opened.
	// The second is what separates "we energised the strike" from "somebody went through".
	SnapshotRef string `json:"snapshotRef" form:"snapshotRef" query:"snapshotRef"`
	ContactRef  string `json:"contactRef" form:"contactRef" query:"contactRef"`
}

// Decisions.
const (
	DecisionGranted = "granted"
	DecisionDenied  = "denied"
)

// Reasons. These are the enumerated answers to "why did that door not open?", and they are a
// closed set precisely so that an operator, a report and an alert rule can all agree on them.
const (
	ReasonOK                 = "ok"
	ReasonUnknownCredential  = "unknown-credential"
	ReasonCredentialInactive = "credential-inactive"
	ReasonCredentialExpired  = "credential-expired"
	ReasonCredentialRevoked  = "credential-revoked"
	ReasonHolderSuspended    = "holder-suspended"
	ReasonHolderExpired      = "holder-expired"
	ReasonNoGrant            = "no-grant"
	ReasonOutOfSchedule      = "out-of-schedule"
	ReasonHoliday            = "holiday"
	ReasonLockdown           = "lockdown"
	ReasonAntipassback       = "antipassback"
	ReasonOfflineCacheStale  = "offline-cache-expired"
	ReasonOfflineNotAllowed  = "offline-not-allowed"
	ReasonOfflineDenied      = "offline-denied"
	ReasonSecureChannel      = "secure-channel-failed"
	ReasonReaderOffline      = "reader-offline"
	// ReasonReaderNotEnrolled is a badge on a reader that ANSWERS PERFECTLY WELL and that nobody
	// added. It used to be filed as reader-offline, which is the one thing it is not: the first
	// live bench of this app watched a healthy simulated reader deliver cards while the operator
	// was told the reader was offline. That sends an installer commissioning a new door to check
	// cabling and power for a device whose only problem is that it is not in the database.
	ReasonReaderNotEnrolled = "reader-not-enrolled"
	ReasonDoorDisabled       = "door-disabled"
	ReasonBadPin             = "bad-pin"
	ReasonDuress             = "duress"
	// ReasonDoorForced and ReasonDoorHeldOpen are not access DECISIONS — nobody presented a
	// credential — but they belong in the same append-only log, because "what happened at this
	// door" is one question and answering it from two tables that have to be interleaved by
	// timestamp is how the interesting sequence gets missed.
	ReasonDoorForced   = "door-forced"
	ReasonDoorHeldOpen = "door-held-open"
	ReasonDoorClosed   = "door-closed"
)
