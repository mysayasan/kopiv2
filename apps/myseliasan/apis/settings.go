package apis

import (
	"io"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/myseliasan/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
)

// maxSettingsBody caps a settings PUT. The largest field is the content-security-policy
// string; 256 KiB is far more than any real config section needs.
const maxSettingsBody = 256 << 10

type settingsApi struct {
	settings    services.ISettingsService
	session     *middlewares.AccessSessionMidware
	audit       services.IAuditService
	browseRoots []string
}

// NewSettingsApi mounts the in-app config editor. Every handler self-gates to superadmin
// regardless of the permission matrix — these values can take the whole control plane
// offline, so they are never delegated to a lesser role.
//
//	GET   /api/settings                 — every editable section (secrets masked)
//	GET   /api/settings/fs/browse        — whitelisted server-side file/folder picker
//	GET   /api/settings/{section}        — one section
//	PUT   /api/settings/{section}        — validate + persist; response reports needsRestart
//	POST  /api/settings/{section}/reset  — restore first-run defaults for a section
//
// browseRoots are extra directories the file picker may browse (e.g. the app's data dir),
// on top of the built-in whitelist (working dir, home, common install locations).
func NewSettingsApi(router *mux.Router, auth middlewares.AuthMidware, session *middlewares.AccessSessionMidware, settings services.ISettingsService, audit services.IAuditService, browseRoots []string) {
	h := &settingsApi{settings: settings, session: session, audit: audit, browseRoots: browseRoots}
	g := router.PathPrefix("/settings").Subrouter()
	g.Use(auth.Middleware)
	g.Use(session.Middleware)
	g.HandleFunc("", h.requireSuper(h.getAll)).Methods("GET")
	// Literal routes registered before the "/{section}" var routes so they aren't captured by them.
	g.HandleFunc("/cache/test", h.requireSuper(h.testCache)).Methods("POST")
	g.HandleFunc("/notification/test", h.requireSuper(h.testMail)).Methods("POST")
	g.HandleFunc("/fs/browse", h.requireSuper(h.browseFilesystem)).Methods("GET")
	g.HandleFunc("/{section}", h.requireSuper(h.getSection)).Methods("GET")
	g.HandleFunc("/{section}", h.requireSuper(h.saveSection)).Methods("PUT")
	g.HandleFunc("/{section}/reset", h.requireSuper(h.resetSection)).Methods("POST")
}

// requireSuper wraps a handler so only a superadmin session may call it.
func (a *settingsApi) requireSuper(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a.session.IsSuperadmin(r) {
			controllers.SendError(w, controllers.ErrLimitedAccess, "superadmin access required")
			return
		}
		next(w, r)
	}
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
	a.record(r, "settings.save", section)
	controllers.SendResult(w, res, "succeed")
}

// browseFilesystem lists a directory for the Settings path pickers, confined to the
// whitelisted roots (see services.BrowseDirectory). Read-only; superadmin-gated.
func (a *settingsApi) browseFilesystem(w http.ResponseWriter, r *http.Request) {
	result, err := services.BrowseDirectory(r.URL.Query().Get("path"), a.browseRoots)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, result, "succeed")
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

// testMail sends a real message through the relay in the request body (blank password
// uses the stored one) so an operator can verify the mail path before an incident
// depends on it. Superadmin-gated, and recorded: a test message is outbound traffic
// leaving the control plane, and the audit trail is what says who caused it.
func (a *settingsApi) testMail(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxSettingsBody))
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, "request body too large")
		return
	}
	if err := a.settings.TestMail(r.Context(), body); err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	a.record(r, "settings.testMail", "notification")
	controllers.SendResult(w, map[string]any{"ok": true}, "succeed")
}

func (a *settingsApi) resetSection(w http.ResponseWriter, r *http.Request) {
	section := mux.Vars(r)["section"]
	res, err := a.settings.Reset(r.Context(), section)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	a.record(r, "settings.reset", section)
	controllers.SendResult(w, res, "succeed")
}

// record writes a best-effort audit entry for a settings change (never the values, which
// may include secrets — only which section changed and by whom).
func (a *settingsApi) record(r *http.Request, action, section string) {
	if a.audit == nil {
		return
	}
	roleId, actor := operatorIdentity(r)
	a.audit.Record(r.Context(), services.AuditEntry{
		Action:     action,
		ActorId:    operatorUserId(r),
		ActorEmail: actor,
		ActorRole:  roleId,
		TargetType: "settings",
		TargetId:   section,
		ClientIp:   clientIP(r),
	})
}
