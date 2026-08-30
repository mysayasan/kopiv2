package apis

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/mymatasan/services"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
)

// facesApi is the enrollment/roster surface for face recognition: manage the people the system
// should recognize and their faceprints. Recognition itself is configured as a per-camera detection
// rule (DetectionType "face") through the normal rules API — this is only the global gallery.
//
// FACE TEMPLATES ARE BIOMETRIC DATA. Every route here is admin-only (enforced by the RBAC policy on
// /api/faces, same as the rest of the admin surface). Enrollment accepts a base64 image so the SAME
// endpoint serves an uploaded photo and a snapshot the browser grabbed from a live camera.
type facesApi struct {
	gallery *services.FaceGalleryService
	// models installs/reports the face-recognition prerequisites. Nil-safe: a build without it
	// answers "unavailable" rather than panicking.
	models *services.FaceModelsInstaller
	// vision answers "when was this person last seen?" from the alert log. The roster is the only
	// screen where that question is asked about a PERSON rather than about a camera.
	vision services.IVisionService
}

func NewFacesApi(router *mux.Router, gallery *services.FaceGalleryService, models *services.FaceModelsInstaller, vision services.IVisionService) {
	h := &facesApi{gallery: gallery, models: models, vision: vision}
	g := router.PathPrefix("/faces").Subrouter()
	g.HandleFunc("", h.list).Methods("GET")
	g.HandleFunc("", h.create).Methods("POST")
	// The setup routes sit BEFORE /{id} so "models" is never read as a person id.
	g.HandleFunc("/models", h.modelsStatus).Methods("GET")
	g.HandleFunc("/models/install", h.modelsInstall).Methods("POST")
	g.HandleFunc("/models/install/status", h.modelsInstallStatus).Methods("GET")
	g.HandleFunc("/sightings", h.sightings).Methods("GET")
	g.HandleFunc("/embeddings/{id}", h.deleteEmbedding).Methods("DELETE")
	g.HandleFunc("/{id}", h.update).Methods("PUT")
	g.HandleFunc("/{id}", h.remove).Methods("DELETE")
	g.HandleFunc("/{id}/enroll", h.enroll).Methods("POST")
	g.HandleFunc("/{id}/embeddings", h.embeddings).Methods("GET")
}

// modelsStatus reports what face recognition still needs on this host: the two .onnx models, the
// worker script, and whether the detector's Python can load them. The enrolment screen calls it
// when it opens, so the operator is told BEFORE they pick a photo rather than by a failure after.
func (a *facesApi) modelsStatus(w http.ResponseWriter, r *http.Request) {
	if a.models == nil {
		controllers.SendError(w, controllers.ErrInternalServerError, "the face-model installer is unavailable")
		return
	}
	controllers.SendResult(w, a.models.Status(r.Context()), "succeed")
}

// modelsInstall starts the background download/install and returns immediately; the client polls
// modelsInstallStatus for the live log. Admin-gated like every other route in this file.
func (a *facesApi) modelsInstall(w http.ResponseWriter, r *http.Request) {
	if a.models == nil {
		controllers.SendError(w, controllers.ErrInternalServerError, "the face-model installer is unavailable")
		return
	}
	if err := a.models.StartInstall(r.Context()); err != nil {
		// "already running" and "directory not writable" are both the caller's business.
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, a.models.InstallStatus(), "succeed")
}

func (a *facesApi) modelsInstallStatus(w http.ResponseWriter, r *http.Request) {
	if a.models == nil {
		controllers.SendError(w, controllers.ErrInternalServerError, "the face-model installer is unavailable")
		return
	}
	controllers.SendResult(w, a.models.InstallStatus(), "succeed")
}

func (a *facesApi) list(w http.ResponseWriter, r *http.Request) {
	people, err := a.gallery.ListPeopleWithCounts(r.Context())
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{"items": people}, "succeed")
}

type personBody struct {
	Name    string `json:"name"`
	Notes   string `json:"notes"`
	Enabled bool   `json:"enabled"`
}

func (a *facesApi) create(w http.ResponseWriter, r *http.Request) {
	var body personBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, "invalid body")
		return
	}
	p, err := a.gallery.CreatePerson(r.Context(), body.Name, body.Notes, 0)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, p, "succeed")
}

func (a *facesApi) update(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var body personBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, "invalid body")
		return
	}
	if err := a.gallery.UpdatePerson(r.Context(), int64(id), body.Name, body.Notes, body.Enabled, 0); err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{"updated": id}, "succeed")
}

func (a *facesApi) remove(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := a.gallery.DeletePerson(r.Context(), int64(id)); err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{"deleted": id}, "succeed")
}

type enrollBody struct {
	// Image is a base64 JPEG — an uploaded photo, or a snapshot the browser grabbed from a camera.
	Image  string `json:"image"`
	Source string `json:"source"` // "upload" | "camera"
}

func (a *facesApi) enroll(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var body enrollBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, "invalid body")
		return
	}
	raw := strings.TrimSpace(body.Image)
	if i := strings.Index(raw, ","); strings.HasPrefix(raw, "data:") && i > 0 {
		raw = raw[i+1:] // strip a data-URL prefix if the browser sent one
	}
	img, err := base64.StdEncoding.DecodeString(raw)
	if err != nil || len(img) == 0 {
		controllers.SendError(w, controllers.ErrBadRequest, "image must be a base64 JPEG")
		return
	}
	emb, err := a.gallery.Enroll(r.Context(), int64(id), img, body.Source, 0)
	if err != nil {
		// Enrollment rejections (no face / multiple faces / too small) are user-actionable messages.
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, emb, "succeed")
}

func (a *facesApi) embeddings(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	rows, err := a.gallery.ListEmbeddings(r.Context(), int64(id))
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{"items": rows}, "succeed")
}

func (a *facesApi) deleteEmbedding(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := a.gallery.DeleteEmbedding(r.Context(), int64(id)); err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{"deleted": id}, "succeed")
}


// faceSighting is one person's most recent recognition: when, where, and by which alert.
type faceSighting struct {
	PersonId   int64   `json:"personId"`
	PersonName string  `json:"personName"`
	CameraId   int64   `json:"cameraId"`
	AlertId    int64   `json:"alertId"`
	At         int64   `json:"at"`
	Confidence float64 `json:"confidence"`
}

// sightings answers the question the roster could not: has any of this ever worked?
//
// Enrolling a person and switching on a camera produces alerts, event clips and notifications —
// all of which land on OTHER screens. From the People page the feature was indistinguishable from
// one that does nothing, which is how somebody ends up asking what it is for. This returns the
// LATEST recognition per person so each card can say "seen 4 minutes ago on Lobby Entrance".
//
// It reads the alert log rather than keeping its own tally: the alert IS the record, and a second
// counter would be a second thing to keep true. `unknownAt` reports the most recent UNRECOGNIZED
// face separately — it belongs to nobody, so it cannot sit on a card, but "strangers are being
// seen and none of them are enrolled" is exactly what a screen showing only known people would
// hide.
func (a *facesApi) sightings(w http.ResponseWriter, r *http.Request) {
	if a.vision == nil {
		controllers.SendError(w, controllers.ErrInternalServerError, "the alert log is unavailable")
		return
	}
	// Scan a bounded window of recent face alerts, newest first. A person seen once a year is not
	// in it, which is the right trade: this line is "recently seen", not an attendance record.
	limit := uint64(500)
	if v := strings.TrimSpace(r.URL.Query().Get("scan")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 2000 {
			limit = uint64(n)
		}
	}
	rows, _, err := a.vision.GetAlerts(r.Context(), limit, 0, 0, "detections",
		[]sqldataenums.Filter{{FieldName: "DetectionType", Compare: sqldataenums.Equal, Value: "face"}},
		[]sqldataenums.Sorter{{FieldName: "CreatedAt", Sort: sqldataenums.DESC}})
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}

	latest := map[int64]faceSighting{}
	unknownAt := int64(0)
	unknownCount := 0
	for _, row := range rows {
		if row == nil {
			continue
		}
		var meta struct {
			PersonId   int64   `json:"personId"`
			PersonName string  `json:"personName"`
			Recognized bool    `json:"recognized"`
			Confidence float64 `json:"faceConfidence"`
		}
		if strings.TrimSpace(row.Metadata) != "" {
			_ = json.Unmarshal([]byte(row.Metadata), &meta)
		}
		if !meta.Recognized || meta.PersonId <= 0 {
			unknownCount++
			if row.CreatedAt > unknownAt {
				unknownAt = row.CreatedAt
			}
			continue
		}
		// Rows arrive newest-first, so the first one seen for a person is their latest.
		if _, seen := latest[meta.PersonId]; seen {
			continue
		}
		latest[meta.PersonId] = faceSighting{
			PersonId:   meta.PersonId,
			PersonName: meta.PersonName,
			CameraId:   row.CameraId,
			AlertId:    row.Id,
			At:         row.CreatedAt,
			Confidence: meta.Confidence,
		}
	}

	items := make([]faceSighting, 0, len(latest))
	for _, s := range latest {
		items = append(items, s)
	}
	controllers.SendResult(w, map[string]any{
		"items":        items,
		"scanned":      len(rows),
		"unknownAt":    unknownAt,
		"unknownCount": unknownCount,
	}, "succeed")
}
