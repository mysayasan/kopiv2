package apis

import (
	"io"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/myidsan/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
)

// maxSettingsBody caps a settings PUT. The largest field is the content-security-policy
// string; 256 KiB is far more than any real config section needs.
const maxSettingsBody = 256 << 10

type settingsApi struct {
	settings services.ISettingsService
	auditRecorder
}

// NewSettingsApi mounts the in-app config editor. Every handler self-gates to superadmin
// regardless of the permission matrix — these values can take the whole control plane
// offline, so they are never delegated to a lesser role.
//
//	GET   /api/settings                 — every editable section (secrets masked)
//	GET   /api/settings/{section}        — one section
//	PUT   /api/settings/{section}        — validate + persist; response reports needsRestart
//	POST  /api/settings/{section}/reset  — restore first-run defaults for a section
//
// Deliberately WITHOUT myseliasan's server-side file-browse endpoint. That endpoint lets a
// caller enumerate directories on the host, and this is the identity provider — the
// highest-value target in the suite and the one whose compromise reaches every other app.
// The two paths an operator can set here (the log file and the file-storage root) are
// chosen once at install and can be typed, which is not worth a filesystem-enumeration
// surface on this particular server.
func NewSettingsApi(router *mux.Router, auth middlewares.AuthMidware, access *middlewares.AccessSessionMidware, settings services.ISettingsService, audit services.IAuditService, trustedProxies []string) {
	h := &settingsApi{settings: settings, auditRecorder: newAuditRecorder(audit, trustedProxies)}

	// Superadmin enforced as MIDDLEWARE on the whole group, matching how every other
	// sensitive myidsan surface is gated (audit, backup, mfa-admin, session-admin). These
	// values include the JWT secret and the lockout policy; they are never delegated to a
	// lesser role via the permission matrix.
	g := router.PathPrefix("/settings").Subrouter()
	g.Use(auth.Middleware)
	g.Use(access.Middleware)
	g.Use(access.RequireSuperadmin)

	g.HandleFunc("", h.getAll).Methods("GET")
	// Literal routes registered before the "/{section}" var routes so they aren't captured by them.
	g.HandleFunc("/cache/test", h.testCache).Methods("POST")
	g.HandleFunc("/{section}", h.getSection).Methods("GET")
	g.HandleFunc("/{section}", h.saveSection).Methods("PUT")
	g.HandleFunc("/{section}/reset", h.resetSection).Methods("POST")
}

func (a *settingsApi) getAll(w http.ResponseWriter, r *http.Request) {
	controllers.SendResult(w, map[string]any{
		"sections": a.settings.Sections(),
		"values":   a.settings.GetAll(),
	}, "succeed")
}

func (a *settingsApi) getSection(w http.ResponseWriter, r *http.Request) {
	section := mux.Vars(r)["section"]
	data, err := a.settings.Get(section)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, data, "succeed")
}

func (a *settingsApi) saveSection(w http.ResponseWriter, r *http.Request) {
	section := mux.Vars(r)["section"]
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxSettingsBody))
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, "request body too large")
		return
	}
	res, err := a.settings.Save(r.Context(), section, body)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	a.recordChange(r, "settings.save", section)
	controllers.SendResult(w, res, "succeed")
}

// testCache pings Redis with the settings in the request body (blank password uses the
// stored one) so an operator can verify connectivity before saving. Superadmin-gated.
func (a *settingsApi) testCache(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxSettingsBody))
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, "request body too large")
		return
	}
	if err := a.settings.TestCache(r.Context(), body); err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{"ok": true}, "succeed")
}

func (a *settingsApi) resetSection(w http.ResponseWriter, r *http.Request) {
	section := mux.Vars(r)["section"]
	res, err := a.settings.Reset(r.Context(), section)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	a.recordChange(r, "settings.reset", section)
	controllers.SendResult(w, res, "succeed")
}

// recordChange writes a best-effort audit entry for a settings change. It records WHICH
// section changed and by whom, never the values — a section can contain the JWT secret,
// the SSO internal token or a Redis password, and the audit trail is readable by every
// superadmin and exported to CSV.
func (a *settingsApi) recordChange(r *http.Request, action, section string) {
	a.record(r, services.AuditEntry{
		Action:     action,
		TargetType: "settings",
		TargetId:   section,
	})
}
