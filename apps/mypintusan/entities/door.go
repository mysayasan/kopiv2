package entities

// Door is one controlled opening.
type Door struct {
	Id   int64  `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	Name string `json:"name" form:"name" query:"name" validate:"required"`

	// Placement reuses myseliasan's site/building/floor tree rather than inventing a second
	// geography — a door belongs on a floor plan next to the cameras already watching it.
	SiteId     int64 `json:"siteId" form:"siteId" query:"siteId" idx:"site"`
	BuildingId int64 `json:"buildingId" form:"buildingId" query:"buildingId" idx:"building"`
	FloorId    int64 `json:"floorId" form:"floorId" query:"floorId"`

	// Class is interior | perimeter | critical. It drives the offline policy, the reader trust
	// matrix, and whether Secure Channel is required.
	Class string `json:"class" form:"class" query:"class" validate:"required"`

	// LockKind is fail-secure | fail-safe. It is MODELLED rather than merely documented because it
	// changes both the wiring and the alarm semantics: a fail-safe maglock has no mechanical
	// egress, so every safety device must be able to cut its power without software, wired in
	// series on the lock feed. See MYPINTUSAN_HARDWARE_PLAN.md §6 — and note that egress
	// requirements are set by local building code (UBBL/BOMBA here, NFPA 101/IBC elsewhere), which
	// this app deliberately does not attempt to encode.
	LockKind string `json:"lockKind" form:"lockKind" query:"lockKind" validate:"required"`

	UnlockSeconds int `json:"unlockSeconds" form:"unlockSeconds" query:"unlockSeconds"`
	// ExtendedUnlockSeconds is the longer strike time for holders flagged ExtendedUnlock.
	ExtendedUnlockSeconds int `json:"extendedUnlockSeconds" form:"extendedUnlockSeconds" query:"extendedUnlockSeconds"`
	// HeldOpenSeconds is the door-held-open alarm threshold.
	HeldOpenSeconds int `json:"heldOpenSeconds" form:"heldOpenSeconds" query:"heldOpenSeconds"`

	ReaderInId int64 `json:"readerInId" form:"readerInId" query:"readerInId"`
	// ReaderOutId is the exit-side reader; 0 for a REX-only door. Required for hard anti-passback,
	// which is why APB cannot simply be switched on everywhere.
	ReaderOutId int64 `json:"readerOutId" form:"readerOutId" query:"readerOutId"`

	// Bindings into myiotsan: the relay channel that throws the strike, and the contact that
	// reports real door position. Actuation goes through myiotsan's guarded CommandService.Issue
	// chokepoint — mypintusan does not drive a relay directly.
	RelayDeviceKey   string `json:"relayDeviceKey" form:"relayDeviceKey" query:"relayDeviceKey"`
	RelayChannel     int    `json:"relayChannel" form:"relayChannel" query:"relayChannel"`
	ContactDeviceKey string `json:"contactDeviceKey" form:"contactDeviceKey" query:"contactDeviceKey"`

	// RequireSecureChannel defaults on for perimeter and critical. If the session fails to
	// establish or drops mid-session, the reader is taken OUT OF SERVICE and alarmed; it does not
	// silently fall back to cleartext. A downgrade-to-plaintext fallback is exactly what an RS-485
	// tap wants, and "the door kept working" is how it goes unnoticed for a year.
	RequireSecureChannel bool `json:"requireSecureChannel" form:"requireSecureChannel" query:"requireSecureChannel"`

	// OfflinePolicy is cached | deny. There is deliberately NO allow-all: "fail open on network
	// loss" is a documented attack (cut the uplink, walk in), and offering it as a checkbox means
	// somebody will tick it. Free egress is unaffected either way because egress is hardware.
	OfflinePolicy string `json:"offlinePolicy" form:"offlinePolicy" query:"offlinePolicy"`
	// OfflineTTLSeconds overrides the class default (interior 72h, perimeter 24h, critical 8h).
	OfflineTTLSeconds int `json:"offlineTtlSeconds" form:"offlineTtlSeconds" query:"offlineTtlSeconds"`

	// AntiPassback is off | soft | hard. Soft (log the violation, grant anyway) is the right
	// default: hard APB on a door with a flaky contact locks out the entire staff on day one.
	AntiPassback string `json:"antiPassback" form:"antiPassback" query:"antiPassback"`

	Enabled   bool  `json:"enabled" form:"enabled" query:"enabled"`
	CreatedBy int64 `json:"createdBy" form:"createdBy" query:"createdBy"`
	CreatedAt int64 `json:"createdAt" form:"createdAt" query:"createdAt"`
	UpdatedBy int64 `json:"updatedBy" form:"updatedBy" query:"updatedBy"`
	UpdatedAt int64 `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
}

// Door classes.
const (
	ClassInterior  = "interior"
	ClassPerimeter = "perimeter"
	ClassCritical  = "critical"
)

// Lock kinds.
const (
	LockFailSecure = "fail-secure"
	LockFailSafe   = "fail-safe"
)

// Offline policies. There is no allow-all, by design.
const (
	OfflineCached = "cached"
	OfflineDeny   = "deny"
)

// Anti-passback modes.
const (
	APBOff  = "off"
	APBSoft = "soft"
	APBHard = "hard"
)

// DefaultOfflineTTLSeconds returns the cache lifetime for a door class when the door does not
// override it. Past the TTL the door denies — the cache keeps a network blip from stranding staff
// outside a building, but "works offline" must never mean "stops checking".
func (d Door) DefaultOfflineTTLSeconds() int {
	if d.OfflineTTLSeconds > 0 {
		return d.OfflineTTLSeconds
	}
	switch d.Class {
	case ClassCritical:
		return 8 * 3600
	case ClassPerimeter:
		return 24 * 3600
	default:
		return 72 * 3600
	}
}

// StrikeSeconds returns the unlock duration for a holder, honouring the accessibility extension.
func (d Door) StrikeSeconds(extended bool) int {
	if extended && d.ExtendedUnlockSeconds > 0 {
		return d.ExtendedUnlockSeconds
	}
	if d.UnlockSeconds > 0 {
		return d.UnlockSeconds
	}
	return 5
}
