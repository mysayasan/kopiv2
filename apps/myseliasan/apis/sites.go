package apis

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/myseliasan/services"
	enumauth "github.com/mysayasan/kopiv2/domain/enums/auth"
	"github.com/mysayasan/kopiv2/domain/models"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
)

// maxPlanUploadBytes caps a floor-plan image upload. Plans are line drawings or scans, not
// video; 24 MiB is generous headroom while bounding memory (the whole image is read to
// decode its dimensions and encrypt it).
const maxPlanUploadBytes = 24 << 20

var allowedPlanTypes = map[string]bool{
	"image/png":  true,
	"image/jpeg": true,
	"image/gif":  true,
}

type sitesApi struct {
	sites services.ISiteService
}

// NewSitesApi mounts site + floor-plan management.
//
//	GET/POST         /api/sites
//	PUT/DELETE       /api/sites/{id}
//	GET/POST         /api/sites/{id}/floors      (POST = multipart plan upload)
//	GET/PUT/DELETE   /api/floors/{id}
//	GET              /api/floors/{id}/image      (decrypted plan image, cookie-authed)
func NewSitesApi(router *mux.Router, auth middlewares.AuthMidware, session *middlewares.AccessSessionMidware, sites services.ISiteService) {
	h := &sitesApi{sites: sites}

	sg := router.PathPrefix("/sites").Subrouter()
	sg.Use(auth.Middleware)
	sg.Use(session.Middleware)
	sg.HandleFunc("", h.listSites).Methods("GET")
	sg.HandleFunc("", h.createSite).Methods("POST")
	sg.HandleFunc("/{id}", h.updateSite).Methods("PUT")
	sg.HandleFunc("/{id}", h.deleteSite).Methods("DELETE")
	sg.HandleFunc("/{id}/floors", h.listFloors).Methods("GET")
	sg.HandleFunc("/{id}/floors", h.uploadFloor).Methods("POST")

	fg := router.PathPrefix("/floors").Subrouter()
	fg.Use(auth.Middleware)
	fg.Use(session.Middleware)
	fg.HandleFunc("/{id}", h.getFloor).Methods("GET")
	fg.HandleFunc("/{id}", h.updateFloor).Methods("PUT")
	fg.HandleFunc("/{id}", h.deleteFloor).Methods("DELETE")
	fg.HandleFunc("/{id}/image", h.floorImage).Methods("GET")
	fg.HandleFunc("/{id}/image", h.replaceFloorImage).Methods("POST")
	fg.HandleFunc("/{id}/background", h.floorBackground).Methods("GET")
	fg.HandleFunc("/{id}/placements", h.listPlacements).Methods("GET")
	fg.HandleFunc("/{id}/placements", h.addPlacement).Methods("POST")

	pg := router.PathPrefix("/placements").Subrouter()
	pg.Use(auth.Middleware)
	pg.Use(session.Middleware)
	pg.HandleFunc("/{id}", h.movePlacement).Methods("PUT")
	pg.HandleFunc("/{id}", h.deletePlacement).Methods("DELETE")

	// Node → its floor plan(s): drives the geo-map drill-down (click a node to open the plan
	// its cameras were placed on). Its own prefix so it never collides with /nodes or /floors.
	ng := router.PathPrefix("/node-floorplan").Subrouter()
	ng.Use(auth.Middleware)
	ng.Use(session.Middleware)
	ng.HandleFunc("/{nodeId}", h.nodeFloorplans).Methods("GET")
}

func actorID(r *http.Request) int64 {
	if claims, ok := r.Context().Value(enumauth.Claims).(*models.JwtCustomClaims); ok && claims != nil {
		return claims.Id
	}
	return 0
}

func pathID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

func (a *sitesApi) listSites(w http.ResponseWriter, r *http.Request) {
	rows, err := a.sites.ListSites(r.Context())
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, rows, "succeed")
}

func (a *sitesApi) createSite(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	if strings.TrimSpace(body.Name) == "" {
		controllers.SendError(w, controllers.ErrBadRequest, "name is required")
		return
	}
	site, err := a.sites.CreateSite(r.Context(), strings.TrimSpace(body.Name), strings.TrimSpace(body.Description), actorID(r))
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, site, "succeed")
}

func (a *sitesApi) updateSite(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		controllers.SendError(w, controllers.ErrBadRequest, "invalid id")
		return
	}
	var body struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Ordinal     int    `json:"ordinal"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	site, err := a.sites.UpdateSite(r.Context(), id, strings.TrimSpace(body.Name), strings.TrimSpace(body.Description), body.Ordinal, actorID(r))
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, site, "succeed")
}

func (a *sitesApi) deleteSite(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		controllers.SendError(w, controllers.ErrBadRequest, "invalid id")
		return
	}
	if err := a.sites.DeleteSite(r.Context(), id); err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{"deleted": true}, "succeed")
}

func (a *sitesApi) listFloors(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		controllers.SendError(w, controllers.ErrBadRequest, "invalid id")
		return
	}
	rows, err := a.sites.ListFloors(r.Context(), id)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, rows, "succeed")
}

// uploadFloor accepts a multipart plan image (field "file", plus form "name"). Mirrors the
// training-upload pattern: MaxBytesReader, then ParseMultipartForm, then FormFile.
func (a *sitesApi) uploadFloor(w http.ResponseWriter, r *http.Request) {
	siteID, ok := pathID(r)
	if !ok {
		controllers.SendError(w, controllers.ErrBadRequest, "invalid id")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxPlanUploadBytes)
	if err := r.ParseMultipartForm(maxPlanUploadBytes); err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, "upload too large or malformed")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, "missing file")
		return
	}
	defer file.Close()

	buf := make([]byte, header.Size)
	if _, err := readFull(file, buf); err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, "could not read file")
		return
	}
	contentType := detectPlanType(header.Header.Get("Content-Type"), buf)
	if !allowedPlanTypes[contentType] {
		controllers.SendError(w, controllers.ErrBadRequest, "only PNG, JPEG or GIF images are supported")
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		name = strings.TrimSpace(header.Filename)
	}
	if name == "" {
		name = "Floor plan"
	}
	design := r.FormValue("design") // drawn-plan vector JSON; "" for an uploaded image
	floor, err := a.sites.AddFloor(r.Context(), siteID, name, buf, contentType, design, actorID(r))
	if err != nil {
		if err == services.ErrBadImage {
			controllers.SendError(w, controllers.ErrBadRequest, "unreadable image")
			return
		}
		if err == services.ErrSiteUnknown {
			controllers.SendError(w, controllers.ErrBadRequest, "unknown site")
			return
		}
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, floor, "succeed")
}

// replaceFloorImage rewrites an existing floor's rasterised image + design — used when the
// operator re-saves a drawn plan from the designer. Same multipart shape as uploadFloor.
func (a *sitesApi) replaceFloorImage(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		controllers.SendError(w, controllers.ErrBadRequest, "invalid id")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxPlanUploadBytes)
	if err := r.ParseMultipartForm(maxPlanUploadBytes); err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, "upload too large or malformed")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, "missing file")
		return
	}
	defer file.Close()
	buf := make([]byte, header.Size)
	if _, err := readFull(file, buf); err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, "could not read file")
		return
	}
	contentType := detectPlanType(header.Header.Get("Content-Type"), buf)
	if !allowedPlanTypes[contentType] {
		controllers.SendError(w, controllers.ErrBadRequest, "only PNG, JPEG or GIF images are supported")
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	design := r.FormValue("design")
	floor, err := a.sites.ReplaceFloorImage(r.Context(), id, name, buf, contentType, design, actorID(r))
	if err != nil {
		if err == services.ErrBadImage {
			controllers.SendError(w, controllers.ErrBadRequest, "unreadable image")
			return
		}
		if err == services.ErrFloorUnknown {
			controllers.SendError(w, controllers.ErrNotFound, "floor not found")
			return
		}
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, floor, "succeed")
}

func (a *sitesApi) getFloor(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		controllers.SendError(w, controllers.ErrBadRequest, "invalid id")
		return
	}
	floor, err := a.sites.GetFloor(r.Context(), id)
	if err != nil {
		controllers.SendError(w, controllers.ErrNotFound, "floor not found")
		return
	}
	controllers.SendResult(w, floor, "succeed")
}

func (a *sitesApi) updateFloor(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		controllers.SendError(w, controllers.ErrBadRequest, "invalid id")
		return
	}
	var body struct {
		Name    string `json:"name"`
		Ordinal int    `json:"ordinal"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	floor, err := a.sites.UpdateFloor(r.Context(), id, strings.TrimSpace(body.Name), body.Ordinal, actorID(r))
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, floor, "succeed")
}

func (a *sitesApi) deleteFloor(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		controllers.SendError(w, controllers.ErrBadRequest, "invalid id")
		return
	}
	if err := a.sites.DeleteFloor(r.Context(), id); err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{"deleted": true}, "succeed")
}

// floorImage serves the DECRYPTED plan image. It is cookie-authed like every other route, so
// an <img src="/api/floors/{id}/image"> works (same-origin, CSP img-src 'self') while a raw
// link without a session does not.
func (a *sitesApi) floorImage(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		controllers.SendError(w, controllers.ErrBadRequest, "invalid id")
		return
	}
	img, err := a.sites.FloorImage(r.Context(), id)
	if err != nil {
		controllers.SendError(w, controllers.ErrNotFound, "image not found")
		return
	}
	w.Header().Set("Content-Type", img.ContentType)
	w.Header().Set("Cache-Control", "private, max-age=300")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(img.Data)
}

// floorBackground serves the pristine background image (uploaded photo) so the designer can draw
// on top of it. 404 when the plan has no background (drawn on a blank canvas).
func (a *sitesApi) floorBackground(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		controllers.SendError(w, controllers.ErrBadRequest, "invalid id")
		return
	}
	img, err := a.sites.FloorBackground(r.Context(), id)
	if err != nil {
		controllers.SendError(w, controllers.ErrNotFound, "no background")
		return
	}
	w.Header().Set("Content-Type", img.ContentType)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(img.Data)
}

// nodeFloorplans returns the floor plan(s) that hold a node's camera placements, each with the
// node's markers — the data the geo map needs to drill from a node pin into its floor plan.
func (a *sitesApi) nodeFloorplans(w http.ResponseWriter, r *http.Request) {
	nodeID := strings.TrimSpace(mux.Vars(r)["nodeId"])
	if nodeID == "" {
		controllers.SendError(w, controllers.ErrBadRequest, "nodeId is required")
		return
	}
	plans, err := a.sites.NodeFloorplans(r.Context(), nodeID)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, plans, "succeed")
}

func (a *sitesApi) listPlacements(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		controllers.SendError(w, controllers.ErrBadRequest, "invalid id")
		return
	}
	rows, err := a.sites.ListPlacements(r.Context(), id)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, rows, "succeed")
}

func (a *sitesApi) addPlacement(w http.ResponseWriter, r *http.Request) {
	floorID, ok := pathID(r)
	if !ok {
		controllers.SendError(w, controllers.ErrBadRequest, "invalid id")
		return
	}
	var body struct {
		NodeId        string  `json:"nodeId"`
		CameraId      string  `json:"cameraId"`
		LastKnownName string  `json:"lastKnownName"`
		X             float64 `json:"x"`
		Y             float64 `json:"y"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	if strings.TrimSpace(body.NodeId) == "" {
		controllers.SendError(w, controllers.ErrBadRequest, "nodeId is required")
		return
	}
	p, err := a.sites.AddPlacement(r.Context(), floorID, strings.TrimSpace(body.NodeId), strings.TrimSpace(body.CameraId), strings.TrimSpace(body.LastKnownName), body.X, body.Y, actorID(r))
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, p, "succeed")
}

func (a *sitesApi) movePlacement(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		controllers.SendError(w, controllers.ErrBadRequest, "invalid id")
		return
	}
	// All fields optional: a drag sends x/y, the FOV editor sends heading/fov. Pointers let us
	// tell "not provided" from "set to 0" so an aim edit never resets the position.
	var body struct {
		X       *float64 `json:"x"`
		Y       *float64 `json:"y"`
		Heading *float64 `json:"heading"`
		Fov     *float64 `json:"fov"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	p, err := a.sites.UpdatePlacement(r.Context(), id, body.X, body.Y, body.Heading, body.Fov, actorID(r))
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, p, "succeed")
}

func (a *sitesApi) deletePlacement(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		controllers.SendError(w, controllers.ErrBadRequest, "invalid id")
		return
	}
	if err := a.sites.DeletePlacement(r.Context(), id); err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{"deleted": true}, "succeed")
}

// detectPlanType prefers the client-declared content type when it is one we allow, else
// sniffs the bytes. Sniffing guards against a wrong or missing header.
func detectPlanType(declared string, data []byte) string {
	d := strings.ToLower(strings.TrimSpace(declared))
	if i := strings.IndexByte(d, ';'); i >= 0 {
		d = strings.TrimSpace(d[:i])
	}
	if allowedPlanTypes[d] {
		return d
	}
	sniffed := http.DetectContentType(data)
	if i := strings.IndexByte(sniffed, ';'); i >= 0 {
		sniffed = strings.TrimSpace(sniffed[:i])
	}
	return sniffed
}

// readFull reads len(buf) bytes (or until EOF/error). Small local helper to avoid pulling io
// into the handler just for this.
func readFull(r interface{ Read([]byte) (int, error) }, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := r.Read(buf[total:])
		total += n
		if err != nil {
			if total == len(buf) {
				return total, nil
			}
			return total, err
		}
	}
	return total, nil
}
