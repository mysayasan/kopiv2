package apidocs

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
)

func swaggerTestRouter() *mux.Router {
	router := mux.NewRouter()
	Register(router, "test app", testProvider{})
	return router
}

func get(t *testing.T, router *mux.Router, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

// The whole point of vendoring Swagger UI: the page must not reach off-box. A CDN
// reference here breaks air-gapped installs AND trips the apps' own
// Content-Security-Policy, so /swagger renders blank either way.
func TestSwaggerPageReferencesNoExternalHost(t *testing.T) {
	body := get(t, swaggerTestRouter(), "/swagger").Body.String()

	for _, forbidden := range []string{"cdn.jsdelivr.net", "unpkg.com", "//cdn.", "http://", "https://"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("swagger page references external resource %q:\n%s", forbidden, body)
		}
	}
}

// script-src 'self' forbids inline <script> exactly as firmly as a CDN, so the init code
// has to be its own file. A regression here is invisible locally (no CSP in dev) and only
// shows up on a customer install.
func TestSwaggerPageHasNoInlineScript(t *testing.T) {
	body := get(t, swaggerTestRouter(), "/swagger").Body.String()

	for _, chunk := range strings.Split(body, "<script")[1:] {
		open := strings.Index(chunk, ">")
		if open < 0 {
			t.Fatalf("malformed script tag in:\n%s", body)
		}
		attrs, inner := chunk[:open], chunk[open+1:]
		if !strings.Contains(attrs, "src=") {
			t.Errorf("inline <script> found (violates script-src 'self'): %q", attrs)
		}
		if body := strings.TrimSpace(strings.Split(inner, "</script>")[0]); body != "" {
			t.Errorf("<script> tag carries inline body (violates script-src 'self'): %q", body)
		}
	}
}

func TestSwaggerAssetsAreServedFromTheBinary(t *testing.T) {
	router := swaggerTestRouter()

	cases := []struct {
		path        string
		contentType string
		mustContain string
	}{
		{"/swagger/assets/swagger-ui-bundle.js", "application/javascript", ""},
		{"/swagger/assets/swagger-ui.css", "text/css", ""},
		{"/swagger/assets/swagger-init.js", "application/javascript", "SwaggerUIBundle"},
	}
	for _, tc := range cases {
		rec := get(t, router, tc.path)
		if rec.Code != http.StatusOK {
			t.Errorf("%s status %d want 200", tc.path, rec.Code)
			continue
		}
		if got := rec.Header().Get("Content-Type"); !strings.Contains(got, tc.contentType) {
			t.Errorf("%s content-type %q want %q", tc.path, got, tc.contentType)
		}
		if rec.Body.Len() == 0 {
			t.Errorf("%s served an empty body — is the go:embed pattern still matching?", tc.path)
		}
		if tc.mustContain != "" && !strings.Contains(rec.Body.String(), tc.mustContain) {
			t.Errorf("%s body missing %q", tc.path, tc.mustContain)
		}
	}
}

// Every asset the page asks for must actually resolve; a typo'd href degrades to a blank
// docs page rather than an error anyone notices.
func TestSwaggerPageAssetReferencesResolve(t *testing.T) {
	router := swaggerTestRouter()
	body := get(t, router, "/swagger").Body.String()

	for _, marker := range []string{`href="`, `src="`} {
		for _, chunk := range strings.Split(body, marker)[1:] {
			ref := chunk[:strings.Index(chunk, `"`)]
			if !strings.HasPrefix(ref, "/swagger/") {
				continue
			}
			if rec := get(t, router, ref); rec.Code != http.StatusOK {
				t.Errorf("page references %s but it returns %d", ref, rec.Code)
			}
		}
	}
}

func TestSwaggerUnknownAssetIs404(t *testing.T) {
	if rec := get(t, swaggerTestRouter(), "/swagger/assets/../openapi.go"); rec.Code == http.StatusOK {
		t.Error("asset handler served a path outside the embedded set")
	}
	if rec := get(t, swaggerTestRouter(), "/swagger/assets/nope.js"); rec.Code != http.StatusNotFound {
		t.Errorf("unknown asset status %d want 404", rec.Code)
	}
}
