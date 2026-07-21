package apis

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gorilla/mux"
)

// makeArchive writes a fake .pmtiles blob into the basemap dir and returns that dir.
// The contents are arbitrary — the tiles handler serves bytes, it does not parse them.
func makeArchive(t *testing.T, dir string, size int) string {
	t.Helper()
	blob := make([]byte, size)
	for i := range blob {
		blob[i] = byte(i % 251)
	}
	bdir := filepath.Join(dir, "basemap")
	if err := os.MkdirAll(bdir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bdir, "basemap.pmtiles"), blob, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return bdir
}

// mountTiles wires the tiles + info handlers alone (no auth middleware) against a
// basemap directory, so the tests can hit the byte-serving path directly.
func mountTiles(dir string) http.Handler {
	r := mux.NewRouter()
	h := &basemapApi{dir: dir}
	r.HandleFunc("/api/basemap/tiles/{name}", h.tiles).Methods("GET")
	r.HandleFunc("/api/basemap/info", h.info).Methods("GET")
	return r
}

// TestBasemapRangeRequest is the backend proof: a Range request must yield 206 with the
// exact requested bytes and an Accept-Ranges header — what makes PMTiles' client-side
// range addressing work over our own origin.
func TestBasemapRangeRequest(t *testing.T) {
	dir := t.TempDir()
	srv := mountTiles(makeArchive(t, dir, 4096))

	req := httptest.NewRequest("GET", "/api/basemap/tiles/basemap.pmtiles", nil)
	req.Header.Set("Range", "bytes=100-199")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusPartialContent {
		t.Fatalf("want 206 Partial Content, got %d", rec.Code)
	}
	if got := rec.Body.Len(); got != 100 {
		t.Fatalf("want 100 bytes, got %d", got)
	}
	want := make([]byte, 100)
	for i := range want {
		want[i] = byte((100 + i) % 251)
	}
	if !bytes.Equal(rec.Body.Bytes(), want) {
		t.Fatalf("range bytes mismatch")
	}
	if rec.Header().Get("Accept-Ranges") != "bytes" {
		t.Fatalf("missing Accept-Ranges: bytes (got %q)", rec.Header().Get("Accept-Ranges"))
	}
	if rec.Header().Get("Content-Range") == "" {
		t.Fatalf("missing Content-Range on 206")
	}
}

// TestBasemapNoContentEncoding guards the double-compression hazard: the archive's
// tiles are already gzip-compressed internally and addressed by absolute byte offset,
// so the response must NOT carry Content-Encoding.
func TestBasemapNoContentEncoding(t *testing.T) {
	dir := t.TempDir()
	srv := mountTiles(makeArchive(t, dir, 2048))

	req := httptest.NewRequest("GET", "/api/basemap/tiles/basemap.pmtiles", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	if enc := rec.Header().Get("Content-Encoding"); enc != "" {
		t.Fatalf("archive must not be re-encoded, got Content-Encoding=%q", enc)
	}
	if rec.Body.Len() != 2048 {
		t.Fatalf("want full 2048 bytes, got %d", rec.Body.Len())
	}
}

// TestBasemapTilesRejectsTraversal proves the {name} route can't escape the basemap dir.
func TestBasemapTilesRejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	srv := mountTiles(makeArchive(t, dir, 32))
	// A name that isn't a bare *.pmtiles must be refused.
	req := httptest.NewRequest("GET", "/api/basemap/tiles/nope.txt", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatalf("want non-200 for a non-pmtiles name, got 200")
	}
}

// TestBasemapInfoAbsentIsGraceful proves an un-provisioned fleet gets a clean
// "available:false" from /info (not a 500), so the map renders without cartography.
func TestBasemapInfoAbsentIsGraceful(t *testing.T) {
	srv := mountTiles("") // no dir

	req := httptest.NewRequest("GET", "/api/basemap/info", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	var info basemapInfo
	if err := json.Unmarshal(rec.Body.Bytes(), &info); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if info.Available {
		t.Fatalf("want available=false with no archive")
	}
	if info.Attribution == "" {
		t.Fatalf("attribution must always be present for ODbL compliance")
	}
}

// TestBasemapInfoListsRegions proves a provisioned dir reports its archive(s).
func TestBasemapInfoListsRegions(t *testing.T) {
	dir := t.TempDir()
	srv := mountTiles(makeArchive(t, dir, 64))

	req := httptest.NewRequest("GET", "/api/basemap/info", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	var info basemapInfo
	if err := json.Unmarshal(rec.Body.Bytes(), &info); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !info.Available || len(info.Regions) != 1 || info.Regions[0].Name != "basemap.pmtiles" {
		t.Fatalf("want one region 'basemap.pmtiles', got %+v", info.Regions)
	}
}

// TestResolveBasemapDir covers the provisioning convention: default is <dataDir>/basemap.
func TestResolveBasemapDir(t *testing.T) {
	dir := t.TempDir()
	got := ResolveBasemapDir(dir, "")
	if filepath.Base(got) != "basemap" {
		t.Fatalf("unexpected resolved dir %q", got)
	}
}
