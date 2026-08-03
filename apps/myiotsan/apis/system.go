package apis

import (
	"net/http"
	"time"

	"github.com/gorilla/mux"
	sharedapis "github.com/mysayasan/kopiv2/domain/shared/apis"
	sharedservices "github.com/mysayasan/kopiv2/domain/shared/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
)

// restarter gracefully relaunches the running process. Satisfied by apphost.Restarter.
type restarter interface {
	Restart(reason string)
}

type systemApi struct {
	restarter restarter
}

// NewSystemApi registers the system controls the Settings > System tab needs. Version and health are
// already served by the host runtime (GET /api/version, /api/health, /api/ready); the only thing
// missing was a way to RESTART the appliance — needed to apply the storage/broker settings, which
// are read once at boot. Admin-only (see services.Policy).
//	POST /system/restart        — relaunch so boot-read settings take effect
//	GET  /system/reset/state    — whether factory reset is available, and the phrase to type
//	POST /system/reset          — start a factory reset (body: {"confirm": "<phrase>"})
//	GET  /system/reset/progress — in-memory progress of a running reset
//
// The reset routes carry the same in-handler admin check the settings surface uses, on top
// of the matrix, and the POST additionally requires the typed confirmation server-side.
func NewSystemApi(router *mux.Router, r restarter, reset *sharedservices.SystemResetService) {
	h := &systemApi{restarter: r}
	g := router.PathPrefix("/system").Subrouter()
	g.HandleFunc("/restart", h.restart).Methods("POST")

	if reset != nil {
		rh := sharedapis.NewSystemResetHandlers(reset)
		g.HandleFunc("/reset/state", requireAdmin(rh.State)).Methods("GET")
		g.HandleFunc("/reset/progress", requireAdmin(rh.Progress)).Methods("GET")
		g.HandleFunc("/reset", requireAdmin(rh.Start)).Methods("POST")
	}
}

// requireAdmin is a self-gate on top of the permission matrix, mirroring apis/settings.go:
// defence in depth on the one route that can erase the whole hub.
func requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := sharedapis.LocalUserFromContext(r.Context())
		if !ok || !user.IsAdmin {
			controllers.SendError(w, controllers.ErrLimitedAccess, "administrators only")
			return
		}
		next(w, r)
	}
}

func (a *systemApi) restart(w http.ResponseWriter, r *http.Request) {
	if !requireAdminUser(w, r) {
		return
	}
	if a.restarter == nil {
		controllers.SendError(w, controllers.ErrInternalServerError, "restart is not available on this deployment")
		return
	}
	// Respond first, then restart a beat later, so the browser gets the 200 and can show its
	// "restarting…" overlay before the process goes down.
	controllers.SendResult(w, map[string]any{"restarting": true}, "succeed")
	go func() {
		time.Sleep(500 * time.Millisecond)
		a.restarter.Restart("api restart request")
	}()
}

// requireAdminUser gates a handler to administrators, on top of the RBAC matrix — defence in depth
// on a surface (restarting the appliance) that a viewer or operator must never reach.
func requireAdminUser(w http.ResponseWriter, r *http.Request) bool {
	user, ok := sharedapis.LocalUserFromContext(r.Context())
	if !ok || !user.IsAdmin {
		controllers.SendError(w, controllers.ErrLimitedAccess, "administrators only")
		return false
	}
	return true
}
