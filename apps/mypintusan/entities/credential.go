package entities

// Credential is one physical token belonging to a holder. A holder may have several — card, PIN,
// plate — and any of them opens a door the holder is granted.
type Credential struct {
	Id       int64 `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	HolderId int64 `json:"holderId" form:"holderId" query:"holderId" idx:"holder" validate:"required"`

	// Kind is card | pin | plate | face | mobile.
	//
	// `plate` and `face` are satisfied by mymatasan's LPR and face recognition rather than by a
	// reader on the RS-485 bus — the same decision path, a different sensor. That is the cheapest
	// large win available to this app: gate-by-plate works the day LPR is wired in, with no new
	// decision logic, and it makes the site's existing camera investment pay for itself twice.
	Kind string `json:"kind" form:"kind" query:"kind" validate:"required"`

	// Format is the credential encoding: wiegand26 | wiegand34 | raw-uid | desfire-ev2 | seos |
	// plate | face-vector. It is part of the MATCH KEY, not decoration — the same number in two
	// encodings is two different credentials.
	Format string `json:"format" form:"format" query:"format"`

	// FacilityCode and CardNumber together identify a Wiegand credential.
	//
	// Matching on CardNumber ALONE is a real-world collision, not a theoretical one: card 1234
	// exists in every facility code ever issued, so a site that has bought two batches of cards
	// will, sooner or later, open a door for the wrong person. Both fields are the key.
	FacilityCode int    `json:"facilityCode" form:"facilityCode" query:"facilityCode" idx:"card"`
	CardNumber   string `json:"cardNumber" form:"cardNumber" query:"cardNumber" idx:"card"`

	// PinHash is a bcrypt hash. A PIN is a secret and is stored the way myidsan stores passwords —
	// never a plaintext or reversible column, however convenient a "show PIN" button would be.
	PinHash string `json:"-" form:"-" query:"-"`

	// DuressPinHash, when set, grants access AND raises a SILENT alarm. Conventionally it is the
	// normal PIN with the last digit incremented. The coercer must see nothing: the alarm must not
	// touch the reader's LED or buzzer, and the door must open exactly as it always does.
	DuressPinHash string `json:"-" form:"-" query:"-"`

	// Status is active | lost | stolen | suspended | expired | revoked. `lost` and `stolen` are
	// distinct from `revoked` on purpose — a card presented after being reported stolen is an
	// incident worth surfacing, not merely a denial.
	Status     string `json:"status" form:"status" query:"status" validate:"required"`
	ValidFrom  int64  `json:"validFrom" form:"validFrom" query:"validFrom"`
	ValidUntil int64  `json:"validUntil" form:"validUntil" query:"validUntil"`

	IssuedBy     int64  `json:"issuedBy" form:"issuedBy" query:"issuedBy"`
	IssuedAt     int64  `json:"issuedAt" form:"issuedAt" query:"issuedAt"`
	RevokedBy    int64  `json:"revokedBy" form:"revokedBy" query:"revokedBy"`
	RevokedAt    int64  `json:"revokedAt" form:"revokedAt" query:"revokedAt"`
	RevokeReason string `json:"revokeReason" form:"revokeReason" query:"revokeReason"`
}

// Credential kinds.
const (
	CredCard   = "card"
	CredPin    = "pin"
	CredPlate  = "plate"
	CredFace   = "face"
	CredMobile = "mobile"
)

// Credential statuses.
const (
	CredActive    = "active"
	CredLost      = "lost"
	CredStolen    = "stolen"
	CredSuspended = "suspended"
	CredExpired   = "expired"
	CredRevoked   = "revoked"
)

// Credential formats. A profile emitting raw-uid only is a SECURITY note, not just a format note:
// UID-only credentials clone with a cheap phone app, so the hardware plan caps such a reader at
// `interior` doors regardless of how well the reader itself is verified.
const (
	FormatWiegand26 = "wiegand26"
	FormatWiegand34 = "wiegand34"
	FormatRawUID    = "raw-uid"
	FormatDesfire   = "desfire-ev2"
	FormatSeos      = "seos"
	FormatPlate     = "plate"
	FormatFace      = "face-vector"
)
