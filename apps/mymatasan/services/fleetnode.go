package services

import (
	sharedentities "github.com/mysayasan/kopiv2/domain/entities"
	"github.com/mysayasan/kopiv2/domain/shared/fleetnode"
	"github.com/mysayasan/kopiv2/infra/atrest"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// The node side of the fleet — discovery, adoption, mTLS enrollment, the control channel and
// the event sink — now lives in domain/shared/fleetnode, because it was never camera-specific.
// myiotsan needs exactly the same thing, and a second copy of a pairing/enrollment stack is a
// second copy of a SECURITY PROTOCOL: the two would drift the first time one of them was fixed.
//
// These aliases keep mymatasan's call sites unchanged.

type (
	IPairingService       = fleetnode.IPairingService
	PairingStatus         = fleetnode.PairingStatus
	AdoptRequest          = fleetnode.AdoptRequest
	AdoptResult           = fleetnode.AdoptResult
	Enrollment            = fleetnode.Enrollment
	EnrollmentManager     = fleetnode.EnrollmentManager
	ControlChannelManager = fleetnode.ControlChannelManager
	ControlDispatcher     = fleetnode.ControlDispatcher
)

var (
	ErrPairingAlreadyPaired = fleetnode.ErrPairingAlreadyPaired
	ErrPairingBadAssertion  = fleetnode.ErrPairingBadAssertion
	ErrPairingBadClaimCode  = fleetnode.ErrPairingBadClaimCode
	ErrPairingBadToken      = fleetnode.ErrPairingBadToken
	ErrPairingFleetKeyUnset = fleetnode.ErrPairingFleetKeyUnset
	ErrPairingFleetKeyShort = fleetnode.ErrPairingFleetKeyShort
)

// NewPairingService builds mymatasan's pairing service. It reports KindCamera: what the control
// plane adopted is a camera node, and it needs to know that — a camera node has recordings and
// live views; a sensor node has telemetry and relays, and they are not interchangeable.
func NewPairingService(repo dbsql.IGenericRepo[sharedentities.RuntimeSetting], cipher *atrest.Cipher, name, version string) IPairingService {
	return fleetnode.NewPairingService(repo, cipher, "mymatasan", name, version, fleetnode.KindCamera)
}

// NewEnrollmentManager builds the node's mTLS enrollment loop.
var NewEnrollmentManager = fleetnode.NewEnrollmentManager

// NewControlChannelManager dials the parent's control channel.
var NewControlChannelManager = fleetnode.NewControlChannelManager

// NewControlEventSink forwards node notifications up the control channel.
var NewControlEventSink = fleetnode.NewControlEventSink
