package firstboot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestWizard(t *testing.T, body string) (*wizard, string) {
	t.Helper()
	path := writeConfig(t, body)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	current, err := currentAnswers(raw)
	if err != nil {
		t.Fatal(err)
	}
	return &wizard{
		opts:    Options{AppName: "testapp", ConfigPath: path, DataDir: filepath.Dir(path)},
		current: current,
		port:    9080,
		done:    make(chan struct{}),
	}, path
}

func do(t *testing.T, w *wizard, method, target string, body any) (*http.Response, map[string]any) {
	t.Helper()
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(encoded)
	}
	req := httptest.NewRequest(method, target, reader)
	if w.token != "" {
		req.Header.Set("X-Setup-Token", w.token)
	}
	rec := httptest.NewRecorder()
	w.routes().ServeHTTP(rec, req)
	res := rec.Result()
	var payload map[string]any
	_ = json.NewDecoder(res.Body).Decode(&payload)
	return res, payload
}

// The page never receives a stored secret: it gets a blank plus a "…Set" flag. A leak
// here would put the database password in the DOM of an unauthenticated page.
func TestStateMasksStoredSecrets(t *testing.T) {
	w, _ := newTestWizard(t, sampleConfig)
	res, payload := do(t, w, http.MethodGet, "/api/state", nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d", res.StatusCode)
	}
	encoded, _ := json.Marshal(payload)
	for _, secret := range []string{"stored-db-pw", "stored-redis-pw", "stored-admin-pw"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("state leaked %q:\n%s", secret, encoded)
		}
	}
	for _, flag := range []string{"dbPasswordSet", "cachePasswordSet", "adminPasswordSet"} {
		if payload[flag] != true {
			t.Fatalf("%s = %v, want true", flag, payload[flag])
		}
	}
	// The rest must still arrive, or the form opens blank.
	answers, _ := payload["answers"].(map[string]any)
	db, _ := answers["db"].(map[string]any)
	if db["host"] != "localhost" {
		t.Fatalf("answers not sent: %v", answers)
	}
}

// The masking contract only holds if a blank submit means "keep the stored value".
// Without this, an operator who never opened the password field would blank three
// credentials at once and the app would fail its very next boot.
func TestCompleteKeepsStoredSecretsWhenSubmittedBlank(t *testing.T) {
	w, path := newTestWizard(t, sampleConfig)
	res, payload := do(t, w, http.MethodPost, "/api/complete", Answers{
		DB:    DBSettings{Engine: "postgres", Host: "localhost", Port: 5433, User: "postgres", DBName: "myseliasandb", SSLMode: "disable"},
		Cache: CacheSettings{Provider: "redis", Address: "localhost:6379"},
		Web:   WebSettings{EnableTLS: true, TLSPorts: []int{3002}, Hostnames: []string{"*"}},
		Admin: AdminSettings{Enabled: true, Username: "admin"},
	})
	if res.StatusCode != http.StatusOK || payload["ok"] != true {
		t.Fatalf("complete failed: %d %v", res.StatusCode, payload)
	}
	doc := readConfig(t, path)
	if got := leaf(t, doc, "db", "password"); got != "stored-db-pw" {
		t.Fatalf("db password = %v, want the stored one", got)
	}
	if got := leaf(t, doc, "cache", "redis", "password"); got != "stored-redis-pw" {
		t.Fatalf("redis password = %v, want the stored one", got)
	}
	if got := leaf(t, doc, "localAuth", "password"); got != "stored-admin-pw" {
		t.Fatalf("admin password = %v, want the stored one", got)
	}
	if payload["startUrl"] != "https://localhost:3002" {
		t.Fatalf("startUrl = %v", payload["startUrl"])
	}
	select {
	case <-w.done:
	default:
		t.Fatal("a completed wizard must release the boot sequence")
	}
}

func TestCompleteRejectsInvalidAnswersWithoutWriting(t *testing.T) {
	w, path := newTestWizard(t, sampleConfig)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	res, payload := do(t, w, http.MethodPost, "/api/complete", Answers{
		DB:    DBSettings{Engine: "postgres", Host: "", Port: 5433, User: "u", DBName: "d"},
		Cache: CacheSettings{Provider: "default"},
		Web:   WebSettings{EnableTLS: true, TLSPorts: []int{3002}},
		Admin: AdminSettings{Enabled: true, Username: "admin"},
	})
	if res.StatusCode != http.StatusBadRequest || payload["ok"] == true {
		t.Fatalf("expected a rejection, got %d %v", res.StatusCode, payload)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("a rejected submit must not touch config.json")
	}
	select {
	case <-w.done:
		t.Fatal("a rejected submit must not release the boot sequence")
	default:
	}
}

// A re-posted form or a second browser tab must not rewrite the file the app is already
// booting from — and must not panic on a second close of the done channel.
func TestCompleteIsIdempotent(t *testing.T) {
	w, _ := newTestWizard(t, sampleConfig)
	answers := Answers{
		DB:    DBSettings{Engine: "sqlite", DBName: "app.db"},
		Cache: CacheSettings{Provider: "default"},
		Web:   WebSettings{EnableTLS: false, NonTLSPorts: []int{8080}, Hostnames: []string{"*"}},
		Admin: AdminSettings{Enabled: false},
	}
	if res, payload := do(t, w, http.MethodPost, "/api/complete", answers); res.StatusCode != http.StatusOK || payload["ok"] != true {
		t.Fatalf("first submit: %d %v", res.StatusCode, payload)
	}
	res, payload := do(t, w, http.MethodPost, "/api/complete", answers)
	if res.StatusCode != http.StatusOK || payload["ok"] != true {
		t.Fatalf("second submit: %d %v", res.StatusCode, payload)
	}
	if payload["startUrl"] != "http://localhost:8080" {
		t.Fatalf("second submit changed the start URL: %v", payload["startUrl"])
	}
}

// The token exists because this page writes database credentials with nothing
// authenticating the caller. When it is in force it has to actually be enforced.
func TestTokenIsRequiredWhenSet(t *testing.T) {
	w, _ := newTestWizard(t, sampleConfig)
	w.token = "s3cr3t"

	req := httptest.NewRequest(http.MethodGet, "/api/state", nil)
	rec := httptest.NewRecorder()
	w.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token: status %d, want 401", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/state?t=wrong", nil)
	rec = httptest.NewRecorder()
	w.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token: status %d, want 401", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/state?t=s3cr3t", nil)
	rec = httptest.NewRecorder()
	w.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("correct token in the query: status %d, want 200", rec.Code)
	}
}

// The page ships its CSS and JS as separate files precisely so the policy can stay
// script-src 'self' with no inline exception. An inline script creeping in later would
// be invisible without this.
func TestPageIsServedWithAStrictPolicyAndNoInlineScript(t *testing.T) {
	w, _ := newTestWizard(t, sampleConfig)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	w.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	csp := rec.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "script-src 'self'") || strings.Contains(csp, "unsafe-inline") {
		t.Fatalf("policy is not strict: %q", csp)
	}
	body := rec.Body.String()
	if strings.Contains(body, "<script>") {
		t.Fatal("the page carries an inline script, which the policy blocks")
	}
	for _, asset := range []string{"assets/setup.css", "assets/setup.js"} {
		if !strings.Contains(body, asset) {
			t.Fatalf("page does not reference %s", asset)
		}
	}
}

// The final pane polls the app's own URL — a different origin from this page — to know
// when it is up and follow it. A bare connect-src 'self' blocks every one of those
// probes silently, which is exactly what the first live run of this page did: the
// browser refused the fetch, the pane waited out its retries, and the automatic hand-off
// never happened even though the app was already serving.
func TestPolicyPermitsTheReadinessPollAtLoopback(t *testing.T) {
	w, _ := newTestWizard(t, sampleConfig)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	w.routes().ServeHTTP(rec, req)

	csp := rec.Header().Get("Content-Security-Policy")
	connect := ""
	for _, directive := range strings.Split(csp, ";") {
		if strings.HasPrefix(strings.TrimSpace(directive), "connect-src") {
			connect = strings.TrimSpace(directive)
		}
	}
	if connect == "" {
		t.Fatalf("no connect-src directive in %q", csp)
	}
	for _, origin := range []string{"http://localhost:*", "https://localhost:*"} {
		if !strings.Contains(connect, origin) {
			t.Fatalf("connect-src %q does not allow %s, so the app-readiness poll is blocked", connect, origin)
		}
	}
	// The allowance is for the loopback hand-off only; it must not become a wildcard.
	if strings.Contains(connect, "*;") || strings.Contains(connect, "connect-src *") || strings.Contains(connect, " *") {
		t.Fatalf("connect-src widened beyond loopback: %q", connect)
	}
}

// Without a declared icon the browser asks for /favicon.ico, which this server does not
// serve — a 404 on the operator's very first page load.
func TestPageDeclaresAnEmbeddedIcon(t *testing.T) {
	body, err := assetsFS.ReadFile("assets/index.html")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(body, []byte(`rel="icon"`)) {
		t.Fatal("the page declares no icon, so the browser will request a /favicon.ico that does not exist")
	}
	if !bytes.Contains(body, []byte(`href="data:image/svg+xml`)) {
		t.Fatal("the icon is not embedded — an external icon can 404 or, on an air-gapped box, hang")
	}
}

// Every asset the page asks for has to be embedded in the binary: these installs are
// air-gapped, and a 404 here is a blank, unusable setup page on a box with no internet.
func TestEmbeddedAssetsAreServed(t *testing.T) {
	w, _ := newTestWizard(t, sampleConfig)
	for _, path := range []string{"/assets/setup.css", "/assets/setup.js"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		w.routes().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK || rec.Body.Len() == 0 {
			t.Fatalf("%s: status %d, %d bytes", path, rec.Code, rec.Body.Len())
		}
	}
}

// The theme picker offers the three choices the operator expects, and every one of them
// is translated in every language — a picker whose options fall back to English keys is
// worse than no picker.
func TestThemePickerIsPresentAndTranslatedEverywhere(t *testing.T) {
	page, err := assetsFS.ReadFile("assets/index.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`id="theme"`, `value="system"`, `value="light"`, `value="dark"`} {
		if !bytes.Contains(page, []byte(want)) {
			t.Fatalf("theme picker is missing %s", want)
		}
	}

	script, err := assetsFS.ReadFile("assets/setup.js")
	if err != nil {
		t.Fatal(err)
	}
	// Four languages x four keys: one missing entry shows the operator a raw key.
	for _, key := range []string{"head.theme", "theme.system", "theme.light", "theme.dark"} {
		if got := bytes.Count(script, []byte("'"+key+"'")); got < 4 {
			t.Errorf("%q appears in %d language dictionaries, want 4", key, got)
		}
	}
}

// The theme class has to land on <html> before the body paints, or a dark-theme operator
// gets a white flash on every load. That is only true while the script is loaded from
// the HEAD and is not deferred — an easy thing to "tidy up" later and silently regress.
func TestThemeScriptLoadsBeforeTheBodyPaints(t *testing.T) {
	page, err := assetsFS.ReadFile("assets/index.html")
	if err != nil {
		t.Fatal(err)
	}
	headEnd := bytes.Index(page, []byte("</head>"))
	if headEnd < 0 {
		t.Fatal("no </head> in the setup page")
	}
	script := bytes.Index(page, []byte(`src="assets/setup.js"`))
	if script < 0 {
		t.Fatal("the setup page does not load setup.js")
	}
	if script > headEnd {
		t.Fatal("setup.js is loaded after </head>, so the theme lands only after the body has painted")
	}
	if bytes.Count(page, []byte(`src="assets/setup.js"`)) != 1 {
		t.Fatal("setup.js is included more than once, so the whole wizard would initialise twice")
	}
	// defer would postpone execution until after parsing, reintroducing the flash.
	tag := page[script-40 : script+40]
	if bytes.Contains(tag, []byte("defer")) || bytes.Contains(tag, []byte("async")) {
		t.Fatalf("setup.js must not be deferred or async: %s", tag)
	}
}

// The theme mechanism is the app's: a theme-<name> class on the root element. If this
// ever drifts to a data-attribute or a body class, the page and the app diverge.
func TestThemeUsesTheAppsRootClassContract(t *testing.T) {
	script, err := assetsFS.ReadFile("assets/setup.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"documentElement", "theme-light", "theme-dark", "prefers-color-scheme"} {
		if !bytes.Contains(script, []byte(want)) {
			t.Fatalf("the theme engine no longer references %q", want)
		}
	}
	css, err := assetsFS.ReadFile("assets/setup.css")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(css, []byte(":root.theme-dark")) {
		t.Fatal("the stylesheet has no dark palette keyed on the root theme class")
	}
	// Native widgets (the select popup, the scrollbar) are painted by the browser, not
	// this stylesheet; without color-scheme a dark card sprouts a white dropdown.
	if !bytes.Contains(css, []byte("color-scheme: dark")) {
		t.Fatal("the dark palette does not set color-scheme, so native controls stay light")
	}
}

// The four suite languages must all be present in the embedded page, or an operator
// picks a language and gets English keys.
func TestEverySuiteLanguageIsBundled(t *testing.T) {
	script, err := assetsFS.ReadFile("assets/setup.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, lang := range []string{"en:", "ms:", "zh:", "ar:"} {
		if !bytes.Contains(script, []byte(lang)) {
			t.Fatalf("no %s dictionary in the setup page", strings.TrimSuffix(lang, ":"))
		}
	}
}

func TestTestCacheNeedsNoConnectionForTheInProcessStore(t *testing.T) {
	w, _ := newTestWizard(t, sampleConfig)
	res, payload := do(t, w, http.MethodPost, "/api/test/cache", CacheSettings{Provider: "default"})
	if res.StatusCode != http.StatusOK || payload["ok"] != true {
		t.Fatalf("in-process cache test: %d %v", res.StatusCode, payload)
	}

	// The page renders this straight into the result line, and it speaks four
	// languages: a sentence from the server would reach the operator in English no
	// matter how well the page's own dictionary was kept. So the server sends a CODE
	// and the page owns the wording — and the page must actually have that wording, in
	// every language, or the operator sees a raw identifier.
	note, _ := payload["note"].(string)
	if note == "" {
		t.Fatal("no note returned for the in-process cache")
	}
	if strings.ContainsAny(note, " .") {
		t.Fatalf("note %q looks like prose; the server must send a code the page can translate", note)
	}
	script, err := assetsFS.ReadFile("assets/setup.js")
	if err != nil {
		t.Fatal(err)
	}
	if got := bytes.Count(script, []byte("'result."+note+"'")); got < 4 {
		t.Errorf("note code %q has %d translations, want one per language (4)", note, got)
	}
}

// A probe the build cannot perform must say so rather than reporting a pass the
// operator would read as "verified".
func TestTestDBWithoutAProbeReportsItCannotVerify(t *testing.T) {
	w, _ := newTestWizard(t, sampleConfig)
	res, payload := do(t, w, http.MethodPost, "/api/test/db", DBSettings{Engine: "sqlite", DBName: "app.db"})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d", res.StatusCode)
	}
	if payload["ok"] == true {
		t.Fatalf("a missing probe must not report success: %v", payload)
	}
}

func TestTestDBReportsTheProbeFailure(t *testing.T) {
	w, _ := newTestWizard(t, sampleConfig)
	w.opts.ProbeDB = func(context.Context, DBSettings) error { return errors.New("password authentication failed") }
	res, payload := do(t, w, http.MethodPost, "/api/test/db", DBSettings{Engine: "postgres", Host: "h", Port: 5432, User: "u", DBName: "d"})
	if res.StatusCode != http.StatusOK || payload["ok"] == true {
		t.Fatalf("expected a reported failure, got %d %v", res.StatusCode, payload)
	}
	if !strings.Contains(payload["error"].(string), "password authentication failed") {
		t.Fatalf("probe error not surfaced to the operator: %v", payload["error"])
	}
}

// The probe is handed the STORED password when the field is left blank, so "Test" on an
// untouched form tests the configuration the app will actually boot with.
func TestTestDBUsesTheStoredPasswordWhenBlank(t *testing.T) {
	w, _ := newTestWizard(t, sampleConfig)
	var seen string
	w.opts.ProbeDB = func(_ context.Context, cfg DBSettings) error {
		seen = cfg.Password
		return nil
	}
	do(t, w, http.MethodPost, "/api/test/db", DBSettings{Engine: "postgres", Host: "h", Port: 5432, User: "u", DBName: "d"})
	if seen != "stored-db-pw" {
		t.Fatalf("probe got password %q, want the stored one", seen)
	}
}

/* ---------- the whole run ---------- */

// Run is the piece apphost depends on: it must bind, serve, and return only once the
// operator has finished — with the config written and the app's URL in hand.
func TestRunServesAndReturnsOnCompletion(t *testing.T) {
	t.Setenv("KOPIV2_SETUP_ADDR", "127.0.0.1:0")
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(sampleConfig), 0o600); err != nil {
		t.Fatal(err)
	}

	type outcome struct {
		result Result
		err    error
	}
	out := make(chan outcome, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	go func() {
		result, err := Run(ctx, Options{AppName: "testapp", ConfigPath: path, DataDir: dir})
		out <- outcome{result: result, err: err}
	}()

	// The announcement file is how an operator with no console finds the page, so it
	// doubles as this test's way of learning where the wizard landed.
	var url string
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		raw, err := os.ReadFile(filepath.Join(dir, setupURLFile))
		if err == nil {
			for _, line := range strings.Split(string(raw), "\n") {
				if strings.HasPrefix(line, "http://") {
					url = strings.TrimSpace(line)
				}
			}
		}
		if url != "" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if url == "" {
		t.Fatal("the wizard never announced a URL")
	}

	if _, err := http.Get(url); err != nil {
		t.Fatalf("setup page not reachable at %s: %v", url, err)
	}

	body, _ := json.Marshal(Answers{
		DB:    DBSettings{Engine: "sqlite", DBName: "app.db"},
		Cache: CacheSettings{Provider: "default"},
		Web:   WebSettings{EnableTLS: false, NonTLSPorts: []int{8080}, Hostnames: []string{"*"}},
		Admin: AdminSettings{Enabled: true, Username: "admin"},
	})
	res, err := http.Post(strings.TrimSuffix(url, "/")+"/api/complete", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	res.Body.Close()

	select {
	case got := <-out:
		if got.err != nil {
			t.Fatalf("Run: %v", got.err)
		}
		if got.result.StartURL != "http://localhost:8080" {
			t.Fatalf("StartURL = %q", got.result.StartURL)
		}
		doc := readConfig(t, path)
		if leaf(t, doc, "setup", "completed") != true {
			t.Fatal("setup was not marked complete")
		}
		if leaf(t, doc, "db", "engine") != "sqlite" {
			t.Fatal("answers were not written")
		}
	case <-time.After(15 * time.Second):
		t.Fatal("Run did not return after the wizard was completed")
	}

	// The pointer to an unfinished setup must not outlive it, or the next operator
	// follows a dead URL.
	if _, err := os.Stat(filepath.Join(dir, setupURLFile)); !os.IsNotExist(err) {
		t.Fatal("SETUP_URL.txt survived completion")
	}
}

// An abandoned setup (Ctrl+C, a service stop) must abort rather than hang, and must
// leave the config exactly as it found it.
func TestRunAbortsOnCancelWithoutWriting(t *testing.T) {
	t.Setenv("KOPIV2_SETUP_ADDR", "127.0.0.1:0")
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(sampleConfig), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		_, err := Run(ctx, Options{AppName: "testapp", ConfigPath: path, DataDir: dir})
		errCh <- err
	}()
	time.Sleep(200 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run err = %v, want context.Canceled", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return after cancellation")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != sampleConfig {
		t.Fatal("an abandoned setup rewrote config.json")
	}
}
