package apis

import (
	"github.com/gorilla/mux"
	sharedapis "github.com/mysayasan/kopiv2/domain/shared/apis"
	sharedservices "github.com/mysayasan/kopiv2/domain/shared/services"
)

// NewDeploymentApi registers the read-only deployment answer for mypintusan.
//
// The answer is fixed: this app cannot be clustered. It drives the OSDP reader bus
// over a serial port that is opened once and held for the life of the bus, and a
// serial port belongs to exactly one process on exactly one host. A second
// instance would not share the doors, it would fail to open the port at all.
//
// There is deliberately no POST route. The mode is not a choice here, and an
// endpoint that always refuses is a worse answer than one that was never offered.
func NewDeploymentApi(router *mux.Router) {
	h := sharedapis.NewDeploymentHandlers(nil, func() sharedservices.DeploymentEnv {
		return sharedservices.DeploymentEnv{
			Appliance:       true,
			ApplianceReason: sharedservices.ApplianceSerialBus,
		}
	})
	g := router.PathPrefix("/deployment").Subrouter()
	g.HandleFunc("/preflight", h.Preflight).Methods("GET")
}
