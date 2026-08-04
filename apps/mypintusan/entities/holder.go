package entities

// Holder is a person who may go through a door.
//
// Holders live HERE, not in myidsan, and the deciding argument is population rather than
// architectural neatness: most people who badge never sign into any app — contractors, cleaners,
// delivery drivers, visitors, and the majority of floor staff. Forcing every one of them into the
// identity broker to hold a card would fill it with thousands of non-users that RBAC, MFA policy
// and session management would then have to reason about, and would drag a clean intranet SSO
// binary into the life-safety blast radius.
type Holder struct {
	Id int64 `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	// Ref is the site's OWN reference for this person — staff number, contractor id. It is the
	// ukey because that is what an HR export or a visitor book will join on; our Id means nothing
	// outside this database.
	Ref  string `json:"ref" form:"ref" query:"ref" ukey:"ref" validate:"required"`
	Name string `json:"name" form:"name" query:"name" validate:"required"`
	// Kind is staff | contractor | visitor | service. It drives defaults (a visitor should always
	// carry an expiry) and reporting, not permissions — permissions come from group membership.
	Kind string `json:"kind" form:"kind" query:"kind" validate:"required"`

	// SsoUserId links to a myidsan user, or 0 for the majority who have no login.
	//
	// STRICT id match only. NEVER fall back to email. This is a direct carry-over of the
	// privilege-escalation bug already fixed in the suite's federated-user dedup: email-fallback
	// matching was removed because myidsan can emit a non-unique email. Re-introducing it here
	// would let one person's card bind to another person's identity — which in this app is a door
	// opening for the wrong human.
	SsoUserId int64 `json:"ssoUserId" form:"ssoUserId" query:"ssoUserId" idx:"sso_user"`

	// Status is active | suspended | terminated. Suspension is reversible and keeps the audit
	// trail attached; deleting a holder to revoke access would erase who went where.
	Status     string `json:"status" form:"status" query:"status" validate:"required"`
	ValidFrom  int64  `json:"validFrom" form:"validFrom" query:"validFrom"`
	ValidUntil int64  `json:"validUntil" form:"validUntil" query:"validUntil"`

	// OfflineAllowed permits this holder through a `critical` door while its controller is running
	// on cached data. Default false: it is an explicit, auditable grant, not a convenience.
	OfflineAllowed bool `json:"offlineAllowed" form:"offlineAllowed" query:"offlineAllowed"`

	// ExtendedUnlock gives this holder the door's longer strike time — an accessibility provision,
	// modelled on the holder rather than configured per door so it follows the person around the
	// site instead of being re-entered at every opening.
	ExtendedUnlock bool `json:"extendedUnlock" form:"extendedUnlock" query:"extendedUnlock"`

	Notes     string `json:"notes" form:"notes" query:"notes"`
	CreatedBy int64  `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedAt int64  `json:"createdAt" form:"createdAt" query:"createdAt"`
	UpdatedBy int64  `json:"updatedBy" form:"updatedBy" query:"updatedBy"`
	UpdatedAt int64  `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
}

// Holder kinds.
const (
	HolderStaff      = "staff"
	HolderContractor = "contractor"
	HolderVisitor    = "visitor"
	HolderService    = "service"
)

// Holder statuses.
const (
	HolderActive     = "active"
	HolderSuspended  = "suspended"
	HolderTerminated = "terminated"
)
