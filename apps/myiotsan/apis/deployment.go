package apis

import (
	"github.com/gorilla/mux"
	sharedapis "github.com/mysayasan/kopiv2/domain/shared/apis"
	sharedservices "github.com/mysayasan/kopiv2/domain/shared/services"
)

// NewDeploymentApi registers the read-only deployment answer for myiotsan.
//
// The answer is fixed: this app cannot be clustered. Its Modbus RTU pollers open
// serial ports (COM3, /dev/ttyUSB0) that exactly one process on one host can hold,
// and the TCP pollers keep long-lived device sessions that a second instance would
// contend for rather than share.
//
// There is deliberately no POST route — see mypintusan's equivalent.
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
