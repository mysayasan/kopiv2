package apis

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/myseliasan/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
)

type replayHorizonApi struct {
	monitor *services.ReplayHorizonMonitor
}

// NewReplayHorizonApi registers
//
//	GET /api/nodes/replay-horizon
//
// which reports, per node, how its current disconnect stands against the window inside
// which its missed events can still be replayed.
//
// It SWEEPS on request rather than serving the last background pass. The sweep is a node
// list and some arithmetic — no tunnel calls, nothing that touches an appliance — so it is
// cheap enough to run on a page load, and an operator asking "is this node past the point
// of no return yet" deserves the answer as of now rather than as of up to fifteen minutes
// ago. The monitor's own transition dedup means refreshing the page cannot spam anyone
// with repeat warnings.
//
// Mounted under /api/nodes so the fleet grant every role already holds covers it: this is
// health information, and hiding which appliances are quietly losing events helps nobody.
func NewReplayHorizonApi(router *mux.Router, auth middlewares.AuthMidware, monitor *services.ReplayHorizonMonitor) {
	h := &replayHorizonApi{monitor: monitor}
	g := router.PathPrefix("/nodes").Subrouter()
	g.Use(auth.Middleware)
	g.HandleFunc("/replay-horizon", h.report).Methods("GET")
}

func (a *replayHorizonApi) report(w http.ResponseWriter, r *http.Request) {
	if a.monitor == nil {
		controllers.SendError(w, controllers.ErrInternalServerError, "replay horizon monitor unavailable")
		return
	}
	report, err := a.monitor.Sweep(r.Context())
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, report, "succeed")
}
