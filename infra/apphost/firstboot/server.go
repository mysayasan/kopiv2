package firstboot

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

//go:embed assets
var assetsFS embed.FS

// defaultSetupAddr is where the wizard listens unless the config or the environment
// says otherwise. It is loopback-only by default and deliberately so: this page writes
// database credentials and the administrator password with no authentication in front
// of it, because there is no user store yet to authenticate against. Exposing it on a
// network is an explicit decision (setup.allowRemote / KOPIV2_SETUP_ALLOW_REMOTE=1),
// and taking it adds a one-time token to the URL.
//
// The port sits in the band the suite already reserves for itself (pairing uses
// 39532-39534), rather than in the popular 8000/9000 ranges: the first machine this ran
// on already had an audio-driver service holding 9080, and a default that collides on an
// ordinary desktop sends every first install down the fallback path below.
const defaultSetupAddr = "127.0.0.1:39530"

// shutdownGrace bounds the wait for the completion response to reach the browser
// before the wizard's listener closes and the app's own boot continues.
const shutdownGrace = 5 * time.Second

// Run serves the wizard until the operator completes it, and returns what was written.
// It blocks. A cancelled ctx (Ctrl+C, a service stop) aborts with ctx.Err(), so an
// interrupted setup never leaves a half-written config behind — commit is a single
// atomic write at the very end.
func Run(ctx context.Context, opts Options) (Result, error) {
	logf := opts.Logf
	if logf == nil {
		logf = func(string, ...any) {}
	}
	if strings.TrimSpace(opts.ConfigPath) == "" {
		return Result{}, errors.New("firstboot: no config path")
	}
	raw, err := os.ReadFile(opts.ConfigPath)
	if err != nil {
		return Result{}, fmt.Errorf("firstboot: read config: %w", err)
	}
	block, err := readSetupBlock(raw)
	if err != nil {
		return Result{}, fmt.Errorf("firstboot: %w", err)
	}
	current, err := currentAnswers(raw)
	if err != nil {
		return Result{}, fmt.Errorf("firstboot: %w", err)
	}

	listener, err := listen(block, logf)
	if err != nil {
		return Result{}, err
	}
	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port
	remote := !isLoopbackListener(listener)
	token := ""
	if remote {
		if token, err = newToken(); err != nil {
			return Result{}, fmt.Errorf("firstboot: generate setup token: %w", err)
		}
	}

	srv := &wizard{
		opts:    opts,
		current: current,
		port:    port,
		token:   token,
		done:    make(chan struct{}),
	}
	url := srv.pageURL(listener.Addr().String())
	announce(opts, url, remote, logf)

	httpServer := &http.Server{
		Handler:           srv.routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	serveErr := make(chan error, 1)
	go func() {
		err := httpServer.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		serveErr <- err
	}()

	select {
	case <-ctx.Done():
		shutdown(httpServer)
		return Result{}, ctx.Err()
	case err := <-serveErr:
		if err != nil {
			return Result{}, fmt.Errorf("firstboot: setup server: %w", err)
		}
		return Result{}, errors.New("firstboot: setup server stopped before setup was completed")
	case <-srv.done:
		// Shutdown drains the in-flight completion response before the listener
		// closes, so the browser always receives the URL it is about to poll.
		shutdown(httpServer)
		_ = removeSetupURLFile(opts.DataDir)
		result := srv.result()
		logf("setup completed; continuing startup — %s will be reachable at %s", opts.AppName, result.StartURL)
		return result, nil
	}
}

func shutdown(httpServer *http.Server) {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancel()
	_ = httpServer.Shutdown(ctx)
}

// wizard holds the run's mutable state. Answers are only ever committed once, under
// mu, so two browser tabs racing on Finish cannot interleave two writes of the file.
type wizard struct {
	opts    Options
	current Answers
	port    int
	token   string

	mu        sync.Mutex
	completed bool
	startURL  string
	done      chan struct{}
}

func (w *wizard) result() Result {
	w.mu.Lock()
	defer w.mu.Unlock()
	return Result{ConfigPath: w.opts.ConfigPath, StartURL: w.startURL}
}

func (w *wizard) routes() http.Handler {
	mux := http.NewServeMux()
	assets, _ := fs.Sub(assetsFS, "assets")
	mux.Handle("/assets/", http.StripPrefix("/assets/", http.FileServer(http.FS(assets))))
	mux.HandleFunc("/api/state", w.handleState)
	mux.HandleFunc("/api/test/db", w.handleTestDB)
	mux.HandleFunc("/api/test/cache", w.handleTestCache)
	mux.HandleFunc("/api/complete", w.handleComplete)
	mux.HandleFunc("/", w.handlePage)
	return w.guard(mux)
}

// setupCSP is the wizard page's content policy. Script and style are 'self' with no
// inline exception — the page ships its CSS and JS as separate files for exactly that
// reason — and nothing is fetched off-box, which the air-gapped installs require.
//
// connect-src additionally allows the loopback origins, and only those, because the
// final pane polls the app's own URL to know when it is up and follow it. That URL is
// always localhost on a port the operator chose, i.e. a DIFFERENT origin from this page,
// so a bare 'self' silently blocks every probe: the page then waits out its retries and
// tells the operator to click the link, which is the one thing this pane exists to save
// them from. The header is fixed at page load, before the port is known, so the origins
// cannot be narrowed further than the host.
const setupCSP = "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; " +
	"connect-src 'self' http://localhost:* https://localhost:* http://127.0.0.1:* https://127.0.0.1:*; " +
	"base-uri 'none'; form-action 'self'; frame-ancestors 'none'"

// guard applies the wizard's security headers and, when the page is exposed beyond
// loopback, the one-time token.
func (w *wizard) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Content-Security-Policy", setupCSP)
		rw.Header().Set("X-Content-Type-Options", "nosniff")
		rw.Header().Set("Referrer-Policy", "no-referrer")
		rw.Header().Set("Cache-Control", "no-store")
		if w.token != "" && !w.tokenOK(r) {
			http.Error(rw, "setup token required — open the URL printed in the console or in SETUP_URL.txt", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(rw, r)
	})
}

// tokenOK accepts the token from the query string (how the operator first arrives) or
// from a header (how the page's own fetches present it), compared in constant time.
func (w *wizard) tokenOK(r *http.Request) bool {
	supplied := r.Header.Get("X-Setup-Token")
	if supplied == "" {
		supplied = r.URL.Query().Get("t")
	}
	return subtle.ConstantTimeCompare([]byte(supplied), []byte(w.token)) == 1
}

func (w *wizard) handlePage(rw http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(rw, r)
		return
	}
	body, err := assetsFS.ReadFile("assets/index.html")
	if err != nil {
		http.Error(rw, "setup page missing", http.StatusInternalServerError)
		return
	}
	rw.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = rw.Write(body)
}

// handleState hands the page the values the install already ships, so every field opens
// pre-filled. Secrets are never sent back: each is blanked and paired with a "…Set"
// flag, and a blank secret on submit means "keep the stored one" — the same contract the
// in-app Settings editor uses.
func (w *wizard) handleState(rw http.ResponseWriter, r *http.Request) {
	shown := w.current
	dbPasswordSet := shown.DB.Password != ""
	cachePasswordSet := shown.Cache.Password != ""
	adminPasswordSet := shown.Admin.Password != ""
	shown.DB.Password = ""
	shown.Cache.Password = ""
	shown.Admin.Password = ""

	writeJSON(rw, http.StatusOK, map[string]any{
		"app":              w.opts.AppName,
		"configPath":       w.opts.ConfigPath,
		"setupPort":        w.port,
		"answers":          shown,
		"dbPasswordSet":    dbPasswordSet,
		"cachePasswordSet": cachePasswordSet,
		"adminPasswordSet": adminPasswordSet,
		"canTestDb":        w.opts.ProbeDB != nil,
		"canTestCache":     w.opts.ProbeCache != nil,
	})
}

func (w *wizard) handleTestDB(rw http.ResponseWriter, r *http.Request) {
	var body DBSettings
	if !decode(rw, r, &body) {
		return
	}
	w.fillDBSecret(&body)
	if err := validateDB(&body); err != nil {
		writeJSON(rw, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if w.opts.ProbeDB == nil {
		writeJSON(rw, http.StatusOK, map[string]any{"ok": false, "error": "this build cannot test a database connection from the setup page"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	if err := w.opts.ProbeDB(ctx, body); err != nil {
		writeJSON(rw, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(rw, http.StatusOK, map[string]any{"ok": true})
}

func (w *wizard) handleTestCache(rw http.ResponseWriter, r *http.Request) {
	var body CacheSettings
	if !decode(rw, r, &body) {
		return
	}
	w.fillCacheSecret(&body)
	if err := validateCache(&body); err != nil {
		writeJSON(rw, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if !isRedis(body.Provider) {
		// A CODE, not a sentence. This response is rendered verbatim by a page that
		// speaks four languages, so prose from the server would arrive untranslated
		// however carefully the page's own dictionary was maintained. The client owns
		// the wording; the server only says which case it hit.
		writeJSON(rw, http.StatusOK, map[string]any{"ok": true, "note": "noConnectionNeeded"})
		return
	}
	if w.opts.ProbeCache == nil {
		writeJSON(rw, http.StatusOK, map[string]any{"ok": false, "error": "this build cannot test a cache connection from the setup page"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	if err := w.opts.ProbeCache(ctx, body); err != nil {
		writeJSON(rw, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(rw, http.StatusOK, map[string]any{"ok": true})
}

// handleComplete validates, writes config.json, and releases Run. The write happens
// once: a second submit (a re-posted form, a second tab) is answered with the same
// start URL rather than rewriting the file the app is already booting from.
func (w *wizard) handleComplete(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(rw, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body Answers
	if !decode(rw, r, &body) {
		return
	}
	w.fillDBSecret(&body.DB)
	w.fillCacheSecret(&body.Cache)
	if strings.TrimSpace(body.Admin.Password) == "" {
		body.Admin.Password = w.current.Admin.Password
	}
	if err := validate(&body, w.port); err != nil {
		writeJSON(rw, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}

	w.mu.Lock()
	if w.completed {
		startURL := w.startURL
		w.mu.Unlock()
		writeJSON(rw, http.StatusOK, map[string]any{"ok": true, "startUrl": startURL})
		return
	}
	if err := commit(w.opts.ConfigPath, body); err != nil {
		w.mu.Unlock()
		writeJSON(rw, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	w.completed = true
	w.startURL = startURL(body.Web)
	startURLValue := w.startURL
	w.mu.Unlock()

	writeJSON(rw, http.StatusOK, map[string]any{"ok": true, "startUrl": startURLValue})
	close(w.done)
}

// fillDBSecret restores the stored password when the form submitted a blank one, so an
// operator who never touches the password field cannot blank it by accident.
func (w *wizard) fillDBSecret(db *DBSettings) {
	if strings.TrimSpace(db.Password) == "" {
		db.Password = w.current.DB.Password
	}
}

func (w *wizard) fillCacheSecret(c *CacheSettings) {
	if strings.TrimSpace(c.Password) == "" {
		c.Password = w.current.Cache.Password
	}
}

func (w *wizard) pageURL(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		host, port = "127.0.0.1", fmt.Sprint(w.port)
	}
	// A wildcard bind has no address an operator can type; name the loopback one,
	// which reaches it either way.
	if host == "" || host == "::" || host == "0.0.0.0" {
		host = "127.0.0.1"
	}
	url := "http://" + net.JoinHostPort(host, port) + "/"
	if w.token != "" {
		url += "?t=" + w.token
	}
	return url
}

// startURL is where the app itself will answer once boot continues, derived from the
// answers rather than from the running config — the config has not been reloaded yet.
func startURL(web WebSettings) string {
	if web.EnableTLS && len(web.TLSPorts) > 0 {
		return fmt.Sprintf("https://localhost:%d", web.TLSPorts[0])
	}
	if len(web.NonTLSPorts) > 0 {
		return fmt.Sprintf("http://localhost:%d", web.NonTLSPorts[0])
	}
	if len(web.TLSPorts) > 0 {
		return fmt.Sprintf("https://localhost:%d", web.TLSPorts[0])
	}
	return ""
}

// listen binds the wizard's port. The preferred address comes from the environment,
// then the config, then the loopback default; if it is already taken — a second app
// first-booting on the same host, or any unrelated service holding the port — an
// ephemeral port is used instead and announced, because a wizard that refuses to start
// leaves the operator with nothing at all.
//
// The fallback is reported, never silent. An operator who set setup.address and then
// found the wizard somewhere else entirely has to be told why, or the port they
// configured looks simply ignored.
func listen(block setupBlock, logf func(string, ...any)) (net.Listener, error) {
	addr := strings.TrimSpace(os.Getenv("KOPIV2_SETUP_ADDR"))
	if addr == "" {
		addr = strings.TrimSpace(block.Address)
	}
	if addr == "" {
		addr = defaultSetupAddr
	}
	if !allowRemote(block) {
		addr = forceLoopback(addr)
	}

	listener, err := net.Listen("tcp", addr)
	if err == nil {
		return listener, nil
	}
	host, _, splitErr := net.SplitHostPort(addr)
	if splitErr != nil {
		return nil, fmt.Errorf("firstboot: listen on %s: %w", addr, err)
	}
	fallback, ferr := net.Listen("tcp", net.JoinHostPort(host, "0"))
	if ferr != nil {
		return nil, fmt.Errorf("firstboot: listen on %s: %w", addr, err)
	}
	logf("setup wizard: %s is not available (%v) — using port %d instead", addr, err, fallback.Addr().(*net.TCPAddr).Port)
	return fallback, nil
}

func allowRemote(block setupBlock) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("KOPIV2_SETUP_ALLOW_REMOTE"))) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	return block.AllowRemote
}

// forceLoopback rewrites a wildcard or remote host to loopback, keeping the port. The
// default is not merely a bind preference: it is what keeps an unauthenticated page
// that accepts database credentials off the network.
func forceLoopback(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return defaultSetupAddr
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port)
}

func isLoopbackListener(l net.Listener) bool {
	tcp, ok := l.Addr().(*net.TCPAddr)
	if !ok {
		return false
	}
	return tcp.IP.IsLoopback()
}

func newToken() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// setupURLFile is written into the data dir because the console is not always visible:
// under the Windows Service Control Manager there is no console at all, and a
// container's banner scrolls away. The app's first-run credential file exists for the
// same reason.
const setupURLFile = "SETUP_URL.txt"

func announce(opts Options, url string, remote bool, logf func(string, ...any)) {
	const bar = "======================================================================"
	var b strings.Builder
	fmt.Fprintf(&b, "\n%s\n", bar)
	fmt.Fprintf(&b, "  %s is not configured yet.\n\n", opts.AppName)
	fmt.Fprint(&b, "  Finish setup here:\n")
	fmt.Fprintf(&b, "  %s\n", url)
	if remote {
		fmt.Fprint(&b, "\n  This page is reachable from the network and is protected only by the\n")
		fmt.Fprint(&b, "  one-time token in that URL. Do not share it.\n")
	}
	fmt.Fprintf(&b, "\n  %s will start on the ports you choose as soon as you finish.\n", opts.AppName)
	if path := writeSetupURLFile(opts.DataDir, url); path != "" {
		fmt.Fprintf(&b, "\n  Saved to:  %s\n", path)
	}
	// Launched before the banner prints, so the banner reports what actually happened
	// rather than what was merely intended.
	if opts.Browser != nil {
		opened, reason := opts.Browser(url)
		switch {
		case opened:
			fmt.Fprint(&b, "\n  Opening it in your browser now.\n")
		case reason != "":
			fmt.Fprintf(&b, "\n  (Not opening a browser: %s — open the address above.)\n", reason)
		}
	}
	fmt.Fprintf(&b, "%s\n", bar)
	fmt.Print(b.String())
	logf("setup wizard listening at %s", url)
}

func writeSetupURLFile(dataDir, url string) string {
	if strings.TrimSpace(dataDir) == "" {
		return ""
	}
	path := filepath.Join(dataDir, setupURLFile)
	body := "Setup is not finished yet.\n\nOpen this address to configure the app:\n" + url + "\n\nThis file is removed once setup completes.\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		return ""
	}
	return path
}

func removeSetupURLFile(dataDir string) error {
	if strings.TrimSpace(dataDir) == "" {
		return nil
	}
	err := os.Remove(filepath.Join(dataDir, setupURLFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func decode(rw http.ResponseWriter, r *http.Request, target any) bool {
	if r.Method != http.MethodPost {
		http.Error(rw, "method not allowed", http.StatusMethodNotAllowed)
		return false
	}
	if err := json.NewDecoder(http.MaxBytesReader(rw, r.Body, 1<<20)).Decode(target); err != nil {
		writeJSON(rw, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid request body"})
		return false
	}
	return true
}

func writeJSON(rw http.ResponseWriter, status int, payload any) {
	rw.Header().Set("Content-Type", "application/json")
	rw.WriteHeader(status)
	_ = json.NewEncoder(rw).Encode(payload)
}
