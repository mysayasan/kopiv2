package apis

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
)

// osmAttribution is the ODbL-required credit for the OpenStreetMap-derived
// basemap. It is a STATIC STRING rendered in the map corner — never a network
// call — which is what keeps attribution compatible with an intranet install.
const osmAttribution = "© OpenStreetMap contributors"

type basemapApi struct {
	// path is the resolved location of the .pmtiles archive. Empty when no
	// basemap has been provisioned, which is a supported state: the map still
	// renders (node pins over blank space), it just has no cartography.
	path string
}

// NewBasemapApi mounts the offline vector basemap.
//
//	GET /api/basemap/info            → availability + attribution (cheap, always 200)
//	GET /api/basemap/tiles.pmtiles   → the archive itself, Range-served
//
// WHY A SINGLE FILE INSTEAD OF A TILE SERVER: PMTiles is a flat, range-addressed
// archive, so the browser fetches byte ranges of ONE file and does tile lookup
// client-side. That means no tile-server process, no SQLite (as MBTiles would
// need), and no new Go dependency — net/http already answers Range requests.
// The archive is produced out-of-band with `pmtiles extract` (see docs) and
// dropped in the data dir; nothing here talks to the internet, which is the
// requirement on an air-gapped intranet.
func NewBasemapApi(router *mux.Router, auth middlewares.AuthMidware, session *middlewares.AccessSessionMidware, path string) {
	h := &basemapApi{path: path}
	g := router.PathPrefix("/basemap").Subrouter()
	g.Use(auth.Middleware)
	g.Use(session.Middleware)
	g.HandleFunc("/info", h.info).Methods("GET")
	g.HandleFunc("/tiles.pmtiles", h.tiles).Methods("GET")
}

type basemapInfo struct {
	Available   bool   `json:"available"`
	Attribution string `json:"attribution"`
	SizeBytes   int64  `json:"sizeBytes"`
}

// info lets the frontend decide whether to add a basemap layer at all, so a
// fleet with no provisioned archive gets a working (if blank) map rather than a
// stream of 404s behind every tile request.
func (a *basemapApi) info(w http.ResponseWriter, r *http.Request) {
	out := basemapInfo{Attribution: osmAttribution}
	if a.path != "" {
		if st, err := os.Stat(a.path); err == nil && !st.IsDir() {
			out.Available = true
			out.SizeBytes = st.Size()
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

// tiles serves the archive. http.ServeContent does all the Range work (206 +
// Accept-Ranges + If-Range revalidation), which is the whole reason PMTiles
// needs no bespoke tile handler.
func (a *basemapApi) tiles(w http.ResponseWriter, r *http.Request) {
	if a.path == "" {
		controllers.SendError(w, controllers.ErrBadRequest, "no basemap provisioned")
		return
	}
	f, err := os.Open(a.path)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, "basemap not available")
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, "basemap stat failed")
		return
	}

	// The archive's tiles are ALREADY gzip-compressed internally (pmtiles header
	// "tile compression: gzip"), and the client addresses them by absolute byte
	// offset. Re-compressing the response would both waste CPU on incompressible
	// bytes and — because a compressing middleware rewrites the body — break the
	// byte offsets the client is seeking to. There is deliberately no compression
	// middleware in this codebase; if one is ever added, it MUST skip this route.
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", "public, max-age=86400")

	http.ServeContent(w, r, filepath.Base(a.path), st.ModTime(), f)
}

// ResolveBasemapPath picks the .pmtiles archive to serve. An explicit configured
// path wins; otherwise the conventional <dataDir>/basemap/basemap.pmtiles is used
// when present. Returns "" when there is no archive, which callers treat as
// "basemap not provisioned" rather than an error — a fleet map is useful without
// cartography, and requiring a 200MB asset to boot would be hostile.
func ResolveBasemapPath(dataDir, configured string) string {
	candidate := strings.TrimSpace(configured)
	if candidate == "" {
		candidate = filepath.Join("basemap", "basemap.pmtiles")
	}
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(dataDir, candidate)
	}
	if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
		return candidate
	}
	return ""
}
