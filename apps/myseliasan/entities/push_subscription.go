package entities

// PushSubscription is one browser, on one device, that has agreed to be woken by this control
// plane (W3-9). Table `push_subscription`, created by the auto-migrator.
//
// It is per BROWSER, not per user: the same operator with a phone and a laptop has two rows,
// and revoking one must not silence the other. The endpoint is what identifies the device —
// it is issued by the browser vendor's push service and is unique — so it carries the unique
// key. Re-subscribing from the same browser therefore updates the row it already had rather
// than accumulating a duplicate that would double every buzz.
type PushSubscription struct {
	Id int64 `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	// UserId is who this device belongs to. A subscription is personal: it wakes a phone in
	// somebody's pocket, so only its owner (or an administrator cleaning up) may remove it.
	UserId int64 `json:"userId" form:"userId" query:"userId" idx:"user" validate:"required"`
	// Endpoint is the vendor URL this device is reached at. It is also, unavoidably, a
	// third-party identifier for the device — see the note on the service about what leaves
	// the building.
	Endpoint string `json:"endpoint" form:"endpoint" query:"endpoint" ukey:"push_endpoint" validate:"required"`
	// P256dh / Auth are the browser's key material for the payload encryption (RFC 8291).
	// Auth is a secret: with it and the endpoint, anyone could decrypt what we send. Neither
	// is ever serialized back to a client.
	P256dh string `json:"-" form:"p256dh" query:"p256dh"`
	Auth   string `json:"-" form:"auth" query:"auth"`
	// Label is what the operator calls this device ("Ahmad's phone"), so a list of
	// subscriptions is something a person can act on rather than a column of opaque URLs.
	Label string `json:"label" form:"label" query:"label"`
	// MinSeverity is the floor for this device. It is PER DEVICE on purpose: the phone in
	// somebody's pocket at 3am and the laptop on their desk want different thresholds, and a
	// single install-wide setting forces the stricter one on everybody or the looser one on
	// the person who then mutes the app — which is the same as having no push at all.
	MinSeverity string `json:"minSeverity" form:"minSeverity" query:"minSeverity"`
	// Enabled parks a device without forgetting it (a phone left at home for a week).
	Enabled bool `json:"enabled" form:"enabled" query:"enabled"`

	// What the last real delivery attempt did. These are not diagnostics — they ARE the
	// feature's honesty: an install that cannot reach the push service has to say so, and the
	// only way to know is to have tried. See services/push.go.
	LastOutcome   string `json:"lastOutcome" form:"lastOutcome" query:"lastOutcome"`
	LastDetail    string `json:"lastDetail" form:"lastDetail" query:"lastDetail"`
	LastAttemptAt int64  `json:"lastAttemptAt" form:"lastAttemptAt" query:"lastAttemptAt"`
	// LastDeliveredAt is the last time the push service ACCEPTED a message for this device.
	// Kept separate from LastAttemptAt because "we tried a second ago" and "it worked a second
	// ago" are different facts and only one of them is reassuring.
	LastDeliveredAt int64 `json:"lastDeliveredAt" form:"lastDeliveredAt" query:"lastDeliveredAt"`

	CreatedAt int64 `json:"createdAt" form:"createdAt" query:"createdAt"`
	UpdatedAt int64 `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
}
