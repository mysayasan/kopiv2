package apis

import (
	"net/http"

	"github.com/gorilla/mux"
	sharedapis "github.com/mysayasan/kopiv2/domain/shared/apis"
	"github.com/mysayasan/kopiv2/domain/shared/fleetnode"
	sharedservices "github.com/mysayasan/kopiv2/domain/shared/services"
)

// The pairing API and the control dispatcher moved to domain/shared/apis with the rest of the
// node-side fleet stack — they were glue over IPairingService and the app's own router, and
// myiotsan needs them identically. These bindings keep mymatasan's call sites unchanged.

// NewPairingPublicApi registers the unauthenticated pairing endpoints (adopt, release,
// self-drop) that a control plane calls on the node.
func NewPairingPublicApi(router *mux.Router, svc fleetnode.IPairingService, kick func()) {
	sharedapis.NewPairingPublicApi(router, svc, kick)
}

// NewPairingApi registers the authenticated pairing admin endpoints.
func NewPairingApi(router *mux.Router, svc fleetnode.IPairingService) {
	sharedapis.NewPairingApi(router, svc)
}

// NewControlDispatcher builds the node-side handler for tunneled parent->node commands. The
// parent asserts a ROLE NAME; the node resolves it against its OWN roles and matrix, because
// the node owns the data and a compromised parent must not get to say who may delete its
// footage.
func NewControlDispatcher(router http.Handler, roles sharedservices.IAccessRoleService) fleetnode.ControlDispatcher {
	return sharedapis.NewControlDispatcher(router, roles)
}
