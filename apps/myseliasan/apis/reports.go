package apis

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/myseliasan/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
)

// reportsApi mounts the on-demand PDF report endpoints. Each returns a rendered PDF as
// an attachment (never JSON), so the browser downloads/opens it directly. Generating a
// report is a sensitive read of fleet-wide data, so every generation is written to the
// audit trail (reusing auditActor/clientIP from audit.go).
type reportsApi struct {
	svc     services.IReportService
	audit   services.IAuditService
	session *middlewares.AccessSessionMidware
}

// NewReportsApi wires the report routes under /api/reports. auth + session gate the
// group exactly like the other control-plane APIs; the security report additionally
// requires a superadmin (it exposes users, the permission matrix, and the audit trail).
//
//	GET /api/reports/fleet-health.pdf?range=30
//	GET /api/reports/inventory.pdf?siteId=
//	GET /api/reports/security.pdf?range=30        (superadmin only)
//	GET /api/reports/incident.pdf?range=30&notificationId=
func NewReportsApi(router *mux.Router, auth middlewares.AuthMidware, session *middlewares.AccessSessionMidware, svc services.IReportService, audit services.IAuditService) {
	h := &reportsApi{svc: svc, audit: audit, session: session}
	g := router.PathPrefix("/reports").Subrouter()
	g.Use(auth.Middleware)
	g.Use(session.Middleware)
	g.HandleFunc("/fleet-health.pdf", h.fleetHealth).Methods("GET")
	g.HandleFunc("/inventory.pdf", h.inventory).Methods("GET")
	g.HandleFunc("/security.pdf", h.requireSuper(h.security)).Methods("GET")
	g.HandleFunc("/incident.pdf", h.incident).Methods("GET")
}

// requireSuper restricts a handler to a superadmin session.
func (a *reportsApi) requireSuper(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if a.session == nil || !a.session.IsSuperadmin(r) {
			controllers.SendError(w, controllers.ErrLimitedAccess, "superadmin access required")
			return
		}
		next(w, r)
	}
}

func (a *reportsApi) fleetHealth(w http.ResponseWriter, r *http.Request) {
	rep, err := a.svc.FleetHealth(r.Context(), time.Now(), queryDays(r, "range"))
	a.deliver(w, r, "report.fleet_health", rep, err)
}

func (a *reportsApi) inventory(w http.ResponseWriter, r *http.Request) {
	siteID, _ := strconv.ParseInt(r.URL.Query().Get("siteId"), 10, 64)
	rep, err := a.svc.Inventory(r.Context(), time.Now(), siteID)
	a.deliver(w, r, "report.inventory", rep, err)
}

func (a *reportsApi) security(w http.ResponseWriter, r *http.Request) {
	rep, err := a.svc.Security(r.Context(), time.Now(), queryDays(r, "range"))
	a.deliver(w, r, "report.security", rep, err)
}

func (a *reportsApi) incident(w http.ResponseWriter, r *http.Request) {
	nid, _ := strconv.ParseInt(r.URL.Query().Get("notificationId"), 10, 64)
	rep, err := a.svc.Incident(r.Context(), time.Now(), queryDays(r, "range"), nid)
	a.deliver(w, r, "report.incident", rep, err)
}

// deliver streams a rendered report as a PDF attachment and records the generation in
// the audit trail. On error it returns a JSON error like the other endpoints.
func (a *reportsApi) deliver(w http.ResponseWriter, r *http.Request, action string, rep *services.Report, err error) {
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	if a.audit != nil {
		actorID, actorEmail, actorRole := auditActor(r)
		a.audit.Record(r.Context(), services.AuditEntry{
			Action:     action,
			ActorId:    actorID,
			ActorEmail: actorEmail,
			ActorRole:  actorRole,
			TargetType: "report",
			TargetId:   rep.Filename,
			Detail:     "generated PDF report",
			ClientIp:   clientIP(r),
		})
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename=%q`, rep.Filename))
	w.Header().Set("Content-Length", strconv.Itoa(len(rep.Data)))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(rep.Data)
}

// queryDays parses a trailing-window size in days from a query param, defaulting to 30.
func queryDays(r *http.Request, key string) int {
	d, _ := strconv.Atoi(r.URL.Query().Get(key))
	if d <= 0 {
		return 30
	}
	return d
}
