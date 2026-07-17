package apis

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/mymatasan/services"
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
}

func NewFacesApi(router *mux.Router, gallery *services.FaceGalleryService) {
	h := &facesApi{gallery: gallery}
	g := router.PathPrefix("/faces").Subrouter()
	g.HandleFunc("", h.list).Methods("GET")
	g.HandleFunc("", h.create).Methods("POST")
	g.HandleFunc("/embeddings/{id}", h.deleteEmbedding).Methods("DELETE")
	g.HandleFunc("/{id}", h.update).Methods("PUT")
	g.HandleFunc("/{id}", h.remove).Methods("DELETE")
	g.HandleFunc("/{id}/enroll", h.enroll).Methods("POST")
	g.HandleFunc("/{id}/embeddings", h.embeddings).Methods("GET")
}

func (a *facesApi) list(w http.ResponseWriter, r *http.Request) {
	people, err := a.gallery.ListPeople(r.Context())
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
