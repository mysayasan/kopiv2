package apis

import (
	"net/http"
	"time"

	"github.com/gorilla/mux"
	sharedapis "github.com/mysayasan/kopiv2/domain/shared/apis"
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
func NewSystemApi(router *mux.Router, r restarter) {
	h := &systemApi{restarter: r}
	g := router.PathPrefix("/system").Subrouter()
	g.HandleFunc("/restart", h.restart).Methods("POST")
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
