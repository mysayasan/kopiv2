package apis

import (
	"io"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/myiotsan/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
)

type profilesApi struct {
	profiles *services.ProfileService
	ingest   *services.Ingest
	// commands is here for ONE reason: a profile is where a command's topic is declared, so
	// editing a profile changes which topics are reserved to the guarded actuation path. The
	// guard caches that set, and a stale cache is a window in which a flow could publish
	// straight to a freshly declared command topic.
	commands *services.CommandService
}

// NewProfilesApi registers the device-type catalog.
func NewProfilesApi(router *mux.Router, profiles *services.ProfileService, ingest *services.Ingest, commands *services.CommandService) {
	h := &profilesApi{profiles: profiles, ingest: ingest, commands: commands}
	g := router.PathPrefix("/profiles").Subrouter()
	g.HandleFunc("", h.list).Methods("GET")
	g.HandleFunc("", h.create).Methods("POST")
	// Registered before /{id} so the literal path wins.
	g.HandleFunc("/import", h.importProfile).Methods("POST")
	g.HandleFunc("/{id}", h.detail).Methods("GET")
	g.HandleFunc("/{id}/export", h.export).Methods("GET")
	g.HandleFunc("/{id}", h.update).Methods("PUT")
	g.HandleFunc("/{id}", h.remove).Methods("DELETE")
}

func (a *profilesApi) list(w http.ResponseWriter, r *http.Request) {
	rows, err := a.profiles.List(r.Context())
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{"items": rows}, "succeed")
}

func (a *profilesApi) detail(w http.ResponseWriter, r *http.Request) {
	id, ok := readID(w, r)
	if !ok {
		return
	}
	detail, err := a.profiles.Detail(r.Context(), id)
	if err != nil {
		controllers.SendError(w, controllers.ErrNotFound, err.Error())
		return
	}
	controllers.SendResult(w, detail, "succeed")
}

func (a *profilesApi) create(w http.ResponseWriter, r *http.Request) {
	var body services.SaveProfileRequest
	if !decode(w, r, &body) {
		return
	}
	detail, err := a.profiles.Create(r.Context(), body, actorId(r))
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	a.reservedTopicsChanged()
	controllers.SendResult(w, detail, "succeed")
}

func (a *profilesApi) update(w http.ResponseWriter, r *http.Request) {
	id, ok := readID(w, r)
	if !ok {
		return
	}
	var body services.SaveProfileRequest
	if !decode(w, r, &body) {
		return
	}
	detail, err := a.profiles.Update(r.Context(), id, body, actorId(r))
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	// An edited deadband must take effect on the NEXT message, not the next restart.
	a.ingest.InvalidateProfile(id)
	a.reservedTopicsChanged()
	controllers.SendResult(w, detail, "succeed")
}

func (a *profilesApi) remove(w http.ResponseWriter, r *http.Request) {
	id, ok := readID(w, r)
	if !ok {
		return
	}
	if err := a.profiles.Delete(r.Context(), id); err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	a.ingest.InvalidateProfile(id)
	a.reservedTopicsChanged()
	controllers.SendResult(w, map[string]any{"deleted": id}, "succeed")
}

// export renders a profile as a portable document. Tuning a deadband for a particular sensor in
// a particular building is real work; an integrator who does it once should be able to carry it
// to the next site.
func (a *profilesApi) export(w http.ResponseWriter, r *http.Request) {
	id, ok := readID(w, r)
	if !ok {
		return
	}
	doc, err := a.profiles.Export(r.Context(), id)
	if err != nil {
		controllers.SendError(w, controllers.ErrNotFound, err.Error())
		return
	}
	controllers.SendResult(w, doc, "succeed")
}

// importProfile creates a profile from a document. An imported profile is never builtin, and a
// slug collision is REPORTED rather than silently overwritten — quietly replacing a profile
// would re-point every device using it at different decoding rules, which is data corruption
// wearing the costume of a successful import.
func (a *profilesApi) importProfile(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 256*1024)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	detail, err := a.profiles.Import(r.Context(), raw, actorId(r))
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	a.reservedTopicsChanged()
	controllers.SendResult(w, detail, "succeed")
}

// reservedTopicsChanged tells the actuation guard to re-read which topics command a device.
func (a *profilesApi) reservedTopicsChanged() {
	if a.commands != nil {
		a.commands.InvalidateTopics()
	}
}

func parseInt(v string) (int64, error) {
	return strconv.ParseInt(v, 10, 64)
}
