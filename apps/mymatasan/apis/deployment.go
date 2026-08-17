package apis

import (
	"github.com/gorilla/mux"
	sharedapis "github.com/mysayasan/kopiv2/domain/shared/apis"
	sharedservices "github.com/mysayasan/kopiv2/domain/shared/services"
)

// NewDeploymentApi registers the read-only deployment answer for mymatasan.
//
// The answer is fixed: this app cannot be clustered. It owns its camera capture
// pipelines, writes recordings to local disk, and pins detection to this host's
// GPU. A second instance would not share that work — it would open its own streams
// against the same cameras and write a second, divergent set of recordings.
//
// Availability for an NVR is a redundancy question (a second recorder, RAID, a
// spare host) rather than a load-balancer one, which is what the UI says instead.
//
// There is deliberately no POST route — see mypintusan's equivalent.
func NewDeploymentApi(router *mux.Router) {
	h := sharedapis.NewDeploymentHandlers(nil, func() sharedservices.DeploymentEnv {
		return sharedservices.DeploymentEnv{
			Appliance:       true,
			ApplianceReason: sharedservices.ApplianceLocalMedia,
		}
	})
	g := router.PathPrefix("/deployment").Subrouter()
	g.HandleFunc("/preflight", h.Preflight).Methods("GET")
}
