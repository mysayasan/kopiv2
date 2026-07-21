package apis

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
)

// osmAttribution is the ODbL-required credit for the OpenStreetMap-derived
// basemap. It is a STATIC STRING rendered in the map corner — never a network
// call — which is what keeps attribution compatible with an intranet install.
const osmAttribution = "© OpenStreetMap contributors"

// nameRe guards the tiles route against path traversal: a region file is a bare
// name of allowed characters ending in .pmtiles, resolved inside the basemap dir only.
var nameRe = regexp.MustCompile(`^[A-Za-z0-9_.-]+\.pmtiles$`)

// basemapApi serves the offline vector basemap as a DIRECTORY of PMTiles region
// archives (a fleet may cover several disjoint areas), and — when a remote tile
// source is configured — can DOWNLOAD a new region on demand by extracting its
// bounding box from that source with the `pmtiles` tool. Downloads deliberately
// reach the internet; they are the one online action in an otherwise air-gapped
// install, gated behind auth + session RBAC and off unless a source is set.
type basemapApi struct {
	dir        string     // directory holding *.pmtiles region archives
	source     string     // remote pmtiles URL to extract new regions from ("" disables downloads)
	pmtilesBin string     // pmtiles binary name/path (default "pmtiles")
	mu         sync.Mutex // serialises downloads (one extract at a time)
}

// NewBasemapApi mounts the multi-region basemap.
//
//	GET  /api/basemap/info           → availability + attribution + regions[] (each with bounds)
//	GET  /api/basemap/tiles/{name}   → one region archive, Range-served
//	POST /api/basemap/download       → extract a new region (bbox) from the configured source
//
// dir is the basemap directory (every *.pmtiles inside is a region). source is the
// remote pmtiles URL new regions are extracted from; empty disables downloads. bin
// is the pmtiles executable (defaults to "pmtiles" on PATH).
func NewBasemapApi(router *mux.Router, auth middlewares.AuthMidware, session *middlewares.AccessSessionMidware, dir, source, bin string) {
	if strings.TrimSpace(bin) == "" {
		bin = "pmtiles"
	}
	h := &basemapApi{dir: dir, source: strings.TrimSpace(source), pmtilesBin: bin}
	g := router.PathPrefix("/basemap").Subrouter()
	g.Use(auth.Middleware)
	g.Use(session.Middleware)
	g.HandleFunc("/info", h.info).Methods("GET")
	g.HandleFunc("/config", h.getConfig).Methods("GET")
	g.HandleFunc("/config", h.setConfig).Methods("PUT")
	g.HandleFunc("/download", h.download).Methods("POST")
	g.HandleFunc("/tiles/{name}", h.tiles).Methods("GET")
}

// sourceFile is where a runtime-configured remote source URL is persisted (so an operator can set
// it from the UI without an env var + restart). The env var, when set, always wins and locks it.
func (a *basemapApi) sourceFile() string { return filepath.Join(a.dir, "source.txt") }

// currentSource resolves the remote tile source: the env override if set, else the runtime file.
func (a *basemapApi) currentSource() string {
	if a.source != "" {
		return a.source
	}
	if a.dir == "" {
		return ""
	}
	b, err := os.ReadFile(a.sourceFile())
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// hasTool reports whether the pmtiles executable is resolvable — surfaced so the UI can tell the
// operator when the one remaining server-side install (the pmtiles binary) is missing.
func (a *basemapApi) hasTool() bool {
	if strings.ContainsAny(a.pmtilesBin, `/\`) {
		if st, err := os.Stat(a.pmtilesBin); err == nil && !st.IsDir() {
			return true
		}
		return false
	}
	_, err := exec.LookPath(a.pmtilesBin)
	return err == nil
}

type basemapConfig struct {
	Source      string `json:"source"` // current remote source (may be masked-length only)
	CanDownload bool   `json:"canDownload"`
	HasTool     bool   `json:"hasTool"`    // the pmtiles binary is installed on the server
	EnvManaged  bool   `json:"envManaged"` // set via env var → read-only in the UI
}

// getConfig reports the download setup state so the UI can guide the operator.
func (a *basemapApi) getConfig(w http.ResponseWriter, r *http.Request) {
	src := a.currentSource()
	controllers.SendResult(w, basemapConfig{Source: src, CanDownload: src != "", HasTool: a.hasTool(), EnvManaged: a.source != ""}, "succeed")
}

// setConfig stores the remote source URL at runtime (persisted to the basemap dir). Admin-gated by
// the router middleware. Refused when the env var manages it. An empty value clears it (downloads
// off). Validates a plain http(s) URL — the server will fetch from it, so it must be deliberate.
func (a *basemapApi) setConfig(w http.ResponseWriter, r *http.Request) {
	if a.source != "" {
		controllers.SendError(w, controllers.ErrBadRequest, "source is managed by an environment variable")
		return
	}
	var body struct {
		Source string `json:"source"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	src := strings.TrimSpace(body.Source)
	if src != "" && !strings.HasPrefix(src, "http://") && !strings.HasPrefix(src, "https://") {
		controllers.SendError(w, controllers.ErrBadRequest, "source must be an http(s) URL to a .pmtiles archive")
		return
	}
	if a.dir == "" {
		controllers.SendError(w, controllers.ErrBadRequest, "no basemap directory configured")
		return
	}
	if err := os.MkdirAll(a.dir, 0o755); err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	if src == "" {
		_ = os.Remove(a.sourceFile())
	} else if err := os.WriteFile(a.sourceFile(), []byte(src), 0o644); err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, basemapConfig{Source: src, CanDownload: src != "", HasTool: a.hasTool(), EnvManaged: false}, "succeed")
}

type regionInfo struct {
	Name      string    `json:"name"`
	Bounds    []float64 `json:"bounds"` // [minLon, minLat, maxLon, maxLat] or nil
	SizeBytes int64     `json:"sizeBytes"`
}

type basemapInfo struct {
	Available   bool         `json:"available"`
	Attribution string       `json:"attribution"`
	CanDownload bool         `json:"canDownload"` // a remote source is configured
	Regions     []regionInfo `json:"regions"`
}

// regionFiles lists the *.pmtiles archives in the basemap dir.
func (a *basemapApi) regionFiles() []string {
	if a.dir == "" {
		return nil
	}
	entries, err := os.ReadDir(a.dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && nameRe.MatchString(e.Name()) {
			out = append(out, e.Name())
		}
	}
	return out
}

// info lets the frontend add a basemap layer per region (each covers its own
// bounds), and decide whether to offer the "download this region" action.
func (a *basemapApi) info(w http.ResponseWriter, r *http.Request) {
	out := basemapInfo{Attribution: osmAttribution, CanDownload: a.currentSource() != ""}
	for _, name := range a.regionFiles() {
		full := filepath.Join(a.dir, name)
		st, err := os.Stat(full)
		if err != nil || st.IsDir() {
			continue
		}
		out.Regions = append(out.Regions, regionInfo{Name: name, Bounds: readPMTilesBounds(full), SizeBytes: st.Size()})
	}
	out.Available = len(out.Regions) > 0
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

// tiles serves one region archive. http.ServeContent does all the Range work (206 +
// Accept-Ranges + If-Range revalidation), which is the whole reason PMTiles needs no
// bespoke tile handler. The archive's tiles are already gzip-compressed internally and
// addressed by absolute byte offset, so this route must NEVER be re-compressed.
func (a *basemapApi) tiles(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	if !nameRe.MatchString(name) {
		controllers.SendError(w, controllers.ErrBadRequest, "invalid region name")
		return
	}
	full := filepath.Join(a.dir, name)
	f, err := os.Open(full)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, "region not available")
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.IsDir() {
		controllers.SendError(w, controllers.ErrBadRequest, "region not available")
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	http.ServeContent(w, r, name, st.ModTime(), f)
}

// download extracts a new region (bounding box) from the configured remote pmtiles
// source using the `pmtiles` tool, saving it as another region file. This is the one
// action that reaches the internet; it is disabled unless a source is configured.
func (a *basemapApi) download(w http.ResponseWriter, r *http.Request) {
	source := a.currentSource()
	if source == "" {
		controllers.SendError(w, controllers.ErrBadRequest, "region download is not configured (no tile source set)")
		return
	}
	var body struct {
		MinLon  float64 `json:"minLon"`
		MinLat  float64 `json:"minLat"`
		MaxLon  float64 `json:"maxLon"`
		MaxLat  float64 `json:"maxLat"`
		MaxZoom int     `json:"maxZoom"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	if body.MinLon >= body.MaxLon || body.MinLat >= body.MaxLat ||
		body.MinLon < -180 || body.MaxLon > 180 || body.MinLat < -90 || body.MaxLat > 90 {
		controllers.SendError(w, controllers.ErrBadRequest, "invalid bounding box")
		return
	}
	// Cap zoom and area so one request can't try to pull the whole planet at street level.
	mz := body.MaxZoom
	if mz <= 0 || mz > 14 {
		mz = 14
	}
	if (body.MaxLon-body.MinLon) > 25 || (body.MaxLat-body.MinLat) > 25 {
		controllers.SendError(w, controllers.ErrBadRequest, "region is too large; zoom in and download a smaller area")
		return
	}

	// Only one extract at a time (they are heavy and write to the shared dir).
	if !a.mu.TryLock() {
		controllers.SendError(w, controllers.ErrBadRequest, "a region download is already in progress")
		return
	}
	defer a.mu.Unlock()

	if err := os.MkdirAll(a.dir, 0o755); err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, "cannot create basemap dir: "+err.Error())
		return
	}
	name := fmt.Sprintf("region_%d_%d_%d_%d_z%d.pmtiles",
		int(body.MinLon*100), int(body.MinLat*100), int(body.MaxLon*100), int(body.MaxLat*100), mz)
	out := filepath.Join(a.dir, name)
	tmp := out + ".part"
	_ = os.Remove(tmp)

	// pmtiles extract SOURCE OUT --bbox=w,s,e,n --maxzoom=z (downloads only the bbox tiles via
	// HTTP range requests). A generous timeout bounds a stuck download.
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Minute)
	defer cancel()
	bbox := fmt.Sprintf("--bbox=%s,%s,%s,%s",
		strconv.FormatFloat(body.MinLon, 'f', -1, 64), strconv.FormatFloat(body.MinLat, 'f', -1, 64),
		strconv.FormatFloat(body.MaxLon, 'f', -1, 64), strconv.FormatFloat(body.MaxLat, 'f', -1, 64))
	cmd := exec.CommandContext(ctx, a.pmtilesBin, "extract", source, tmp, bbox, fmt.Sprintf("--maxzoom=%d", mz))
	outBytes, err := cmd.CombinedOutput()
	if err != nil {
		_ = os.Remove(tmp)
		msg := strings.TrimSpace(string(outBytes))
		if len(msg) > 400 {
			msg = msg[:400]
		}
		if msg == "" {
			msg = err.Error()
		}
		controllers.SendError(w, controllers.ErrInternalServerError, "extract failed: "+msg)
		return
	}
	if err := os.Rename(tmp, out); err != nil {
		_ = os.Remove(tmp)
		controllers.SendError(w, controllers.ErrInternalServerError, "cannot save region: "+err.Error())
		return
	}
	st, _ := os.Stat(out)
	var size int64
	if st != nil {
		size = st.Size()
	}
	controllers.SendResult(w, regionInfo{Name: name, Bounds: []float64{body.MinLon, body.MinLat, body.MaxLon, body.MaxLat}, SizeBytes: size}, "succeed")
}

// readPMTilesBounds parses the geographic extent from a PMTiles v3 header (a fixed
// 127-byte prefix). The bounds live at byte offsets 102–117 as four little-endian
// int32s in E7 degrees (min lon, min lat, max lon, max lat). Returns nil on any read
// error or an obviously empty box.
func readPMTilesBounds(path string) []float64 {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var h [127]byte
	if _, err := io.ReadFull(f, h[:]); err != nil {
		return nil
	}
	if string(h[0:7]) != "PMTiles" || h[7] != 3 {
		return nil
	}
	e7 := func(off int) float64 {
		return float64(int32(binary.LittleEndian.Uint32(h[off:off+4]))) / 1e7
	}
	minLon, minLat, maxLon, maxLat := e7(102), e7(106), e7(110), e7(114)
	if minLon == 0 && minLat == 0 && maxLon == 0 && maxLat == 0 {
		return nil
	}
	return []float64{minLon, minLat, maxLon, maxLat}
}

// ResolveBasemapDir returns the directory that holds the basemap region archives.
// An explicit configured path wins; otherwise the conventional <dataDir>/basemap is
// used. The map is useful without cartography, so a missing dir is not an error.
func ResolveBasemapDir(dataDir, configured string) string {
	candidate := strings.TrimSpace(configured)
	if candidate == "" {
		candidate = "basemap"
	}
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(dataDir, candidate)
	}
	return candidate
}
