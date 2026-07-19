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

// makeArchive writes a fake .pmtiles blob and returns its path. The contents are
// arbitrary — the handler serves bytes, it does not parse the archive.
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
	p := filepath.Join(bdir, "basemap.pmtiles")
	if err := os.WriteFile(p, blob, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return p
}

// mountTiles wires the tiles handler alone (no auth middleware) so the test can hit
// the byte-serving path directly. The route logic under test is a.tiles.
func mountTiles(path string) http.Handler {
	r := mux.NewRouter()
	h := &basemapApi{path: path}
	r.HandleFunc("/api/basemap/tiles.pmtiles", h.tiles).Methods("GET")
	r.HandleFunc("/api/basemap/info", h.info).Methods("GET")
	return r
}

// TestBasemapRangeRequest is the Phase-0 backend proof: a Range request must yield
// 206 with the exact requested bytes and an Accept-Ranges header — this is what makes
// PMTiles' client-side range addressing work over our own origin.
func TestBasemapRangeRequest(t *testing.T) {
	dir := t.TempDir()
	path := makeArchive(t, dir, 4096)
	srv := mountTiles(path)

	req := httptest.NewRequest("GET", "/api/basemap/tiles.pmtiles", nil)
	req.Header.Set("Range", "bytes=100-199")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusPartialContent {
		t.Fatalf("want 206 Partial Content, got %d", rec.Code)
	}
	if got := rec.Body.Len(); got != 100 {
		t.Fatalf("want 100 bytes, got %d", got)
	}
	// The bytes must be the actual slice [100,200) of the archive, or client-side
	// tile offset lookups would read garbage.
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
// so the response must NOT carry Content-Encoding (a compressing layer would corrupt
// the offsets the client seeks to).
func TestBasemapNoContentEncoding(t *testing.T) {
	dir := t.TempDir()
	path := makeArchive(t, dir, 2048)
	srv := mountTiles(path)

	req := httptest.NewRequest("GET", "/api/basemap/tiles.pmtiles", nil)
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

// TestBasemapInfoAbsentIsGraceful proves an un-provisioned fleet gets a clean
// "available:false" from /info (not a 500), so the map renders without cartography
// instead of erroring.
func TestBasemapInfoAbsentIsGraceful(t *testing.T) {
	srv := mountTiles("") // no archive

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

// TestResolveBasemapPath covers the provisioning convention: present file resolves,
// absent file yields "" (the not-provisioned signal).
func TestResolveBasemapPath(t *testing.T) {
	dir := t.TempDir()
	if got := ResolveBasemapPath(dir, ""); got != "" {
		t.Fatalf("want empty for missing archive, got %q", got)
	}
	makeArchive(t, dir, 16)
	got := ResolveBasemapPath(dir, "")
	if got == "" {
		t.Fatalf("want resolved path for present archive")
	}
	if filepath.Base(got) != "basemap.pmtiles" {
		t.Fatalf("unexpected resolved path %q", got)
	}
}
