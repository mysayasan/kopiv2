package apis

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/myiotsan/services"
	sharedapis "github.com/mysayasan/kopiv2/domain/shared/apis"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
)

type devicesApi struct {
	devices   *services.DeviceService
	telemetry *services.TelemetryService
	profiles  *services.ProfileService
	ingest    *services.Ingest
}

// NewDevicesApi registers the device inventory and its telemetry.
func NewDevicesApi(router *mux.Router, devices *services.DeviceService, telemetry *services.TelemetryService, profiles *services.ProfileService, ingest *services.Ingest) {
	h := &devicesApi{devices: devices, telemetry: telemetry, profiles: profiles, ingest: ingest}
	g := router.PathPrefix("/devices").Subrouter()
	g.HandleFunc("", h.list).Methods("GET")
	g.HandleFunc("", h.create).Methods("POST")
	// Registered before /{id} so the literal path wins over the id pattern.
	g.HandleFunc("/stats", h.stats).Methods("GET")
	g.HandleFunc("/{id}", h.get).Methods("GET")
	g.HandleFunc("/{id}", h.update).Methods("PUT")
	g.HandleFunc("/{id}", h.remove).Methods("DELETE")
	g.HandleFunc("/{id}/password", h.rotate).Methods("POST")
	g.HandleFunc("/{id}/readings", h.readings).Methods("GET")
	g.HandleFunc("/{id}/latest", h.latest).Methods("GET")
}

func (a *devicesApi) list(w http.ResponseWriter, r *http.Request) {
	limit, offset := readPaging(r)
	rows, total, err := a.devices.List(r.Context(), limit, offset)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{"items": rows, "total": total}, "succeed")
}

func (a *devicesApi) get(w http.ResponseWriter, r *http.Request) {
	id, ok := readID(w, r)
	if !ok {
		return
	}
	dev, err := a.devices.GetById(r.Context(), id)
	if err != nil {
		controllers.SendError(w, controllers.ErrNotFound, err.Error())
		return
	}
	controllers.SendResult(w, dev, "succeed")
}

func (a *devicesApi) create(w http.ResponseWriter, r *http.Request) {
	var body services.CreateDeviceRequest
	if !decode(w, r, &body) {
		return
	}
	out, err := a.devices.Create(r.Context(), body, actorId(r))
	if err != nil {
		if errors.Is(err, services.ErrDeviceKeyTaken) {
			controllers.SendError(w, controllers.ErrBadRequest, err.Error())
			return
		}
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	// The generated broker password is in this response and NOWHERE else, ever.
	controllers.SendResult(w, out, "succeed")
}

func (a *devicesApi) update(w http.ResponseWriter, r *http.Request) {
	id, ok := readID(w, r)
	if !ok {
		return
	}
	var body services.UpdateDeviceRequest
	if !decode(w, r, &body) {
		return
	}
	dev, err := a.devices.Update(r.Context(), id, body, actorId(r))
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, dev, "succeed")
}

func (a *devicesApi) remove(w http.ResponseWriter, r *http.Request) {
	id, ok := readID(w, r)
	if !ok {
		return
	}
	if err := a.devices.Delete(r.Context(), id); err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	// The gate must forget this device's baselines, or they outlive it — see Ingest.ForgetDevice.
	a.ingest.ForgetDevice(id)
	controllers.SendResult(w, map[string]any{"deleted": id}, "succeed")
}

func (a *devicesApi) rotate(w http.ResponseWriter, r *http.Request) {
	id, ok := readID(w, r)
	if !ok {
		return
	}
	password, err := a.devices.RotatePassword(r.Context(), id, actorId(r))
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{"password": password}, "succeed")
}

// seriesMaxPoints caps one chart's points. A chart 900 pixels wide cannot draw more, and over
// the cap the series is served from rollups rather than by discarding the recent end of the
// window — see TelemetryService.Series.
const seriesMaxPoints = 2000

// readings returns a time series for one key. Charts read this.
func (a *devicesApi) readings(w http.ResponseWriter, r *http.Request) {
	id, ok := readID(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	key := q.Get("key")
	if key == "" {
		controllers.SendError(w, controllers.ErrBadRequest, "a telemetry key is required")
		return
	}
	from, _ := strconv.ParseInt(q.Get("from"), 10, 64)
	to, _ := strconv.ParseInt(q.Get("to"), 10, 64)
	page, err := a.telemetry.Series(r.Context(), id, key, from, to, seriesMaxPoints)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	// `span` and `truncated` travel with the points because a chart that silently changes what
	// it is drawing is a chart that lies: over a wide window these are rollup buckets, not the
	// sensor's own samples, and the page has to be able to say so.
	controllers.SendResult(w, map[string]any{
		"items": page.Items, "span": page.Span, "truncated": page.Truncated,
	}, "succeed")
}

// latest is what the device page shows at the top: the current value of every key.
func (a *devicesApi) latest(w http.ResponseWriter, r *http.Request) {
	id, ok := readID(w, r)
	if !ok {
		return
	}
	// The profile's declared keys, so every key gets its own seek instead of all of them
	// competing for room in one shared tail — see TelemetryService.Latest. A device with no
	// profile passes none, and falls back to the tail.
	var keys []string
	if dev, err := a.devices.GetById(r.Context(), id); err == nil && dev != nil && dev.ProfileId > 0 {
		if declared, err := a.profiles.KeysFor(r.Context(), dev.ProfileId); err == nil {
			for _, k := range declared {
				keys = append(keys, k.Key)
			}
		}
	}
	latest, err := a.telemetry.Latest(r.Context(), id, keys)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, latest, "succeed")
}

// stats exposes what the ingest path is doing. The ratio of suppressed to stored IS the
// storage design working or not working, and dropped > 0 means the disk cannot keep up — both
// are things an operator has to be able to see rather than infer.
func (a *devicesApi) stats(w http.ResponseWriter, r *http.Request) {
	controllers.SendResult(w, a.ingest.Stats(), "succeed")
}

// --- shared helpers -------------------------------------------------------------------

func readPaging(r *http.Request) (uint64, uint64) {
	q := r.URL.Query()
	limit, _ := strconv.ParseUint(q.Get("limit"), 10, 64)
	offset, _ := strconv.ParseUint(q.Get("offset"), 10, 64)
	return limit, offset
}

func readID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	raw := mux.Vars(r)["id"]
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		controllers.SendError(w, controllers.ErrBadRequest, "invalid id")
		return 0, false
	}
	return id, true
}

func decode(w http.ResponseWriter, r *http.Request, into any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1*1024*1024)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return false
	}
	return true
}

// actorId is the signed-in user, for the created_by/updated_by trail.
func actorId(r *http.Request) int64 {
	if user, ok := sharedapis.LocalUserFromContext(r.Context()); ok {
		return user.Id
	}
	return 0
}

// actorName is WHO the caller is, by name.
//
// It matters because a caller can arrive over the fleet tunnel from the control plane, and that
// operator has no account on this node — their id is 0. The tunnel carries their name ("cp:alice")
// on the synthetic principal, and recording only the id would leave the node's own audit trail
// saying a relay was switched by "System". It was switched by a person.
func actorName(r *http.Request) string {
	if user, ok := sharedapis.LocalUserFromContext(r.Context()); ok {
		return user.Username
	}
	return ""
}
