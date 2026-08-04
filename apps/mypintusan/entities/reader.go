package entities

// Reader is a reader INSTANCE on a bus, as distinct from ReaderProfile, which is the model
// template. One profile, many readers — the abstraction that stops a hundred identical doors being
// configured by hand.
type Reader struct {
	Id        int64  `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	Name      string `json:"name" form:"name" query:"name" validate:"required"`
	ProfileId int64  `json:"profileId" form:"profileId" query:"profileId" idx:"profile"`
	DoorId    int64  `json:"doorId" form:"doorId" query:"doorId" idx:"door"`
	// Direction is in | out. An `out` reader is what makes hard anti-passback possible at all.
	Direction string `json:"direction" form:"direction" query:"direction"`

	// BusPort is the OSDP bus this PD sits on ("COM3", "/dev/ttyUSB0", or "tcp://host:port" for
	// the simulator). Note OSDP and Modbus RTU CANNOT share one RS-485 pair: two polled masters
	// transmitting into each other produce intermittent CRC failures that look exactly like a
	// cabling fault. A door needs two adapters, not one.
	BusPort string `json:"busPort" form:"busPort" query:"busPort" idx:"bus"`
	// OsdpAddress is 0-126, unique per bus. Readers ship at 0, so two new readers on one bus
	// collide until one is moved — address assignment is a real onboarding step, not an
	// afterthought.
	OsdpAddress int `json:"osdpAddress" form:"osdpAddress" query:"osdpAddress"`

	// ScbkState is default | rekeyed | failed. A reader still on the well-known default base key
	// is NOT secure whatever else it claims, because that key is printed in every vendor's manual.
	// Such a reader is capped at `interior` doors until a KEYSET lands, and the UI nags until then.
	ScbkState string `json:"scbkState" form:"scbkState" query:"scbkState"`

	LastSeenAt int64 `json:"lastSeenAt" form:"lastSeenAt" query:"lastSeenAt"`
	// TamperState is ok | tamper | offline.
	TamperState string `json:"tamperState" form:"tamperState" query:"tamperState"`

	Enabled   bool  `json:"enabled" form:"enabled" query:"enabled"`
	CreatedBy int64 `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedAt int64 `json:"createdAt" form:"createdAt" query:"createdAt"`
	UpdatedBy int64 `json:"updatedBy" form:"updatedBy" query:"updatedBy"`
	UpdatedAt int64 `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
}

// Reader directions.
const (
	DirectionIn  = "in"
	DirectionOut = "out"
)

// SCBK states.
const (
	ScbkDefault = "default"
	ScbkRekeyed = "rekeyed"
	ScbkFailed  = "failed"
)

// Tamper states.
const (
	TamperOK      = "ok"
	TamperTripped = "tamper"
	TamperOffline = "offline"
)

// ReaderProfile is a template for a reader TYPE: how to talk to it, what it can do, and how far we
// trust it. It is to mypintusan what DeviceProfile is to myiotsan, with one addition myiotsan does
// not need — a trust level, because a wrong reader binding does not produce a bad reading, it
// produces a door that opens for the wrong person.
type ReaderProfile struct {
	Id   int64  `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	Slug string `json:"slug" form:"slug" query:"slug" ukey:"slug" validate:"required"`
	Name string `json:"name" form:"name" query:"name" validate:"required"`

	Vendor      string `json:"vendor" form:"vendor" query:"vendor"`
	Model       string `json:"model" form:"model" query:"model"`
	Description string `json:"description" form:"description" query:"description"`

	// Transport selects the driver: "osdp" (the standard, and the only one at launch), later
	// "wiegand", "zkteco", "hikvision-isapi".
	Transport string `json:"transport" form:"transport" query:"transport" validate:"required"`

	OsdpBaud int `json:"osdpBaud" form:"osdpBaud" query:"osdpBaud"`
	// OsdpDefaultAddress is the address the reader SHIPS with (almost always 0). It is a profile
	// hint for onboarding, not the live address — a multi-drop bus cannot have two PDs at 0.
	OsdpDefaultAddress int `json:"osdpDefaultAddress" form:"osdpDefaultAddress" query:"osdpDefaultAddress"`
	// SupportsSecureChannel is a CAPABILITY CLAIM by the profile. Whether Secure Channel is
	// REQUIRED is a per-door policy, never a property of the reader.
	SupportsSecureChannel bool `json:"supportsSecureChannel" form:"supportsSecureChannel" query:"supportsSecureChannel"`
	ShipsWithDefaultSCBK  bool `json:"shipsWithDefaultScbk" form:"shipsWithDefaultScbk" query:"shipsWithDefaultScbk"`

	HasKeypad    bool `json:"hasKeypad" form:"hasKeypad" query:"hasKeypad"`
	HasBiometric bool `json:"hasBiometric" form:"hasBiometric" query:"hasBiometric"`
	HasTamper    bool `json:"hasTamper" form:"hasTamper" query:"hasTamper"`
	// HasLedControl and HasBuzzerControl gate the feedback commands. A reader that ignores them
	// silently is worse than one that declares it cannot: the operator sees no red flash on deny
	// and concludes the system is broken.
	HasLedControl    bool `json:"hasLedControl" form:"hasLedControl" query:"hasLedControl"`
	HasBuzzerControl bool `json:"hasBuzzerControl" form:"hasBuzzerControl" query:"hasBuzzerControl"`

	// CardFormats is a JSON array of the encodings this reader emits, most-preferred first.
	CardFormats string `json:"cardFormats" form:"cardFormats" query:"cardFormats"`

	// Verification is unverified | community | verified | reference. It does the real work in the
	// trust matrix and is NOT the same as Builtin — conflating them is the mistake to avoid. A
	// builtin profile may still be unverified if it was seeded from a datasheet.
	Verification string `json:"verification" form:"verification" query:"verification" validate:"required"`
	// VerifiedFirmware and VerifiedAt record WHAT was benched. A verification with no firmware
	// string is worthless: vendors change reader behaviour in point releases.
	VerifiedFirmware string `json:"verifiedFirmware" form:"verifiedFirmware" query:"verifiedFirmware"`
	VerifiedAt       int64  `json:"verifiedAt" form:"verifiedAt" query:"verifiedAt"`
	SourceUrl        string `json:"sourceUrl" form:"sourceUrl" query:"sourceUrl"`

	// Builtin means "shipped in the binary, cannot be deleted", so a site cannot break its own
	// onboarding by tidying up. Orthogonal to Verification.
	Builtin   bool  `json:"builtin" form:"builtin" query:"builtin"`
	CreatedBy int64 `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedAt int64 `json:"createdAt" form:"createdAt" query:"createdAt"`
	UpdatedBy int64 `json:"updatedBy" form:"updatedBy" query:"updatedBy"`
	UpdatedAt int64 `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
}

// Verification levels, ordered.
const (
	VerifyUnverified = "unverified"
	VerifyCommunity  = "community"
	VerifyVerified   = "verified"
	VerifyReference  = "reference"
)
