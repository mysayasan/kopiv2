package firstboot

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sampleConfig is shaped like a real shipped config: the blocks the wizard writes, plus
// blocks it must never touch, and inline arrays that prove untouched bytes survive.
const sampleConfig = `{
  "jwt": {
    "secret": "0123456789abcdef0123"
  },
  "localAuth": {
    "enabled": true,
    "username": "admin",
    "password": "stored-admin-pw"
  },
  "cache": {
    "provider": "default",
    "keyPrefix": "kopiv2",
    "ttlSeconds": 30,
    "redis": {
      "address": "localhost:6379",
      "password": "stored-redis-pw",
      "db": 0,
      "useTls": false
    }
  },
  "pairing": {
    "multicastAddr": "239.255.90.21:49531",
    "mtlsPort": 39532
  },
  "server": {
    "hostnames": ["*"],
    "tlsPorts": [3002],
    "nonTlsPorts": []
  },
  "setup": {
    "completed": false
  },
  "db": {
    "engine": "postgres",
    "host": "localhost",
    "port": 5433,
    "user": "postgres",
    "password": "stored-db-pw",
    "db_name": "myseliasandb",
    "ssl_mode": "disable"
  }
}`

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func readConfig(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("config is not valid JSON after the write: %v\n%s", err, raw)
	}
	return doc
}

func leaf(t *testing.T, doc map[string]any, path ...string) any {
	t.Helper()
	var cur any = doc
	for _, seg := range path {
		obj, ok := cur.(map[string]any)
		if !ok {
			t.Fatalf("path %v: %q is not an object", path, seg)
		}
		cur = obj[seg]
	}
	return cur
}

/* ---------- when the wizard runs ---------- */

// The trigger is the whole safety story: an existing install must never be ambushed by
// a configuration wizard, and a fresh one must never boot past it.
func TestNeeded(t *testing.T) {
	t.Setenv("KOPIV2_SETUP", "")

	cases := []struct {
		name string
		body string
		want bool
	}{
		{"explicit not completed runs the wizard", sampleConfig, true},
		{"completed does not", strings.Replace(sampleConfig, `"completed": false`, `"completed": true`, 1), false},
		{"a config with no setup block is treated as already set up",
			strings.Replace(sampleConfig, "\"setup\": {\n    \"completed\": false\n  },\n  ", "", 1), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeConfig(t, tc.body)
			got, reason, err := Needed(path)
			if err != nil {
				t.Fatalf("Needed: %v", err)
			}
			if got != tc.want {
				t.Fatalf("Needed = %v (%s), want %v", got, reason, tc.want)
			}
		})
	}
}

// A missing config file is apphost's error to report, with a far better message about
// where it looked. The wizard must stay out of the way rather than pre-empting it.
func TestNeededMissingConfigDefersToTheLoader(t *testing.T) {
	t.Setenv("KOPIV2_SETUP", "")
	got, _, err := Needed(filepath.Join(t.TempDir(), "absent.json"))
	if err != nil {
		t.Fatalf("Needed: %v", err)
	}
	if got {
		t.Fatal("a missing config file must not trigger the wizard")
	}
}

// KOPIV2_SETUP=1 is the recovery path for an install that can no longer boot, so it has
// to win over a config that says setup is finished — and its off value has to win the
// other way, for an unattended install that must never wait on a human.
func TestNeededEnvOverride(t *testing.T) {
	completed := strings.Replace(sampleConfig, `"completed": false`, `"completed": true`, 1)

	t.Run("forces the wizard on a completed install", func(t *testing.T) {
		t.Setenv("KOPIV2_SETUP", "1")
		got, _, err := Needed(writeConfig(t, completed))
		if err != nil || !got {
			t.Fatalf("Needed = %v, %v; want true", got, err)
		}
	})
	t.Run("suppresses it on an unconfigured install", func(t *testing.T) {
		t.Setenv("KOPIV2_SETUP", "off")
		got, _, err := Needed(writeConfig(t, sampleConfig))
		if err != nil || got {
			t.Fatalf("Needed = %v, %v; want false", got, err)
		}
	})
	// An unrecognized value must not silently mean "off" — that would boot an
	// unconfigured app straight into a failure with no page to fix it from.
	t.Run("an unrecognized value falls back to the config", func(t *testing.T) {
		t.Setenv("KOPIV2_SETUP", "maybe")
		got, _, err := Needed(writeConfig(t, sampleConfig))
		if err != nil || !got {
			t.Fatalf("Needed = %v, %v; want true", got, err)
		}
	})
}

/* ---------- reading the current install ---------- */

func TestCurrentAnswersPrefillsFromConfig(t *testing.T) {
	got, err := currentAnswers([]byte(sampleConfig))
	if err != nil {
		t.Fatalf("currentAnswers: %v", err)
	}
	if got.DB.Engine != "postgres" || got.DB.Port != 5433 || got.DB.DBName != "myseliasandb" {
		t.Fatalf("db not prefilled: %+v", got.DB)
	}
	if got.DB.Password != "stored-db-pw" {
		t.Fatalf("stored db password not read: %q", got.DB.Password)
	}
	if got.Cache.Provider != "default" || got.Cache.Address != "localhost:6379" {
		t.Fatalf("cache not prefilled: %+v", got.Cache)
	}
	if len(got.Web.TLSPorts) != 1 || got.Web.TLSPorts[0] != 3002 || !got.Web.EnableTLS {
		t.Fatalf("web not prefilled: %+v", got.Web)
	}
	if got.Admin.Username != "admin" || !got.Admin.Enabled {
		t.Fatalf("admin not prefilled: %+v", got.Admin)
	}
}

// "ports" is the legacy single list the host still honours. An install using it must not
// open a blank form, or the operator would "confirm" an app that then listens nowhere.
func TestCurrentAnswersUnderstandsLegacyPortsList(t *testing.T) {
	body := strings.Replace(sampleConfig,
		`"hostnames": ["*"],
    "tlsPorts": [3002],
    "nonTlsPorts": []`,
		`"hostnames": ["*"],
    "ports": [8080],
    "enableTls": false`, 1)
	got, err := currentAnswers([]byte(body))
	if err != nil {
		t.Fatalf("currentAnswers: %v", err)
	}
	if len(got.Web.NonTLSPorts) != 1 || got.Web.NonTLSPorts[0] != 8080 {
		t.Fatalf("legacy ports not surfaced: %+v", got.Web)
	}
	if got.Web.EnableTLS {
		t.Fatal("enableTls:false with a legacy ports list must not read as HTTPS")
	}
}

/* ---------- writing the answers ---------- */

func TestCommitWritesAnswersAndMarksSetupComplete(t *testing.T) {
	path := writeConfig(t, sampleConfig)
	answers := Answers{
		DB:    DBSettings{Engine: "postgres", Host: "db.internal", Port: 5432, User: "kopi", Password: "pw", DBName: "kopidb", SSLMode: "require"},
		Cache: CacheSettings{Provider: "redis", Address: "cache.internal:6379", Password: "rpw", DB: 3, UseTLS: true},
		Web:   WebSettings{EnableTLS: true, TLSPorts: []int{8443}, NonTLSPorts: []int{8080}, Hostnames: []string{"fleet.internal"}},
		Admin: AdminSettings{Enabled: true, Username: "operator", Password: "operator-pw"},
	}
	if err := commit(path, answers); err != nil {
		t.Fatalf("commit: %v", err)
	}

	doc := readConfig(t, path)
	if got := leaf(t, doc, "db", "host"); got != "db.internal" {
		t.Fatalf("db.host = %v", got)
	}
	if got := leaf(t, doc, "db", "db_name"); got != "kopidb" {
		t.Fatalf("db.db_name = %v", got)
	}
	if got := leaf(t, doc, "cache", "provider"); got != "redis" {
		t.Fatalf("cache.provider = %v", got)
	}
	if got := leaf(t, doc, "cache", "redis", "address"); got != "cache.internal:6379" {
		t.Fatalf("cache.redis.address = %v", got)
	}
	if got := leaf(t, doc, "server", "tlsPorts"); len(got.([]any)) != 1 || got.([]any)[0] != float64(8443) {
		t.Fatalf("server.tlsPorts = %v", got)
	}
	if got := leaf(t, doc, "localAuth", "username"); got != "operator" {
		t.Fatalf("localAuth.username = %v", got)
	}
	// Without this the wizard would reappear on every subsequent boot.
	if got := leaf(t, doc, "setup", "completed"); got != true {
		t.Fatalf("setup.completed = %v", got)
	}
	// Blocks the wizard never asks about must come through untouched.
	if got := leaf(t, doc, "pairing", "mtlsPort"); got != float64(39532) {
		t.Fatalf("pairing block mutated: %v", got)
	}
	if got := leaf(t, doc, "jwt", "secret"); got != "0123456789abcdef0123" {
		t.Fatalf("jwt block mutated: %v", got)
	}
	// So must keys inside the blocks it does write.
	if got := leaf(t, doc, "cache", "keyPrefix"); got != "kopiv2" {
		t.Fatalf("cache.keyPrefix lost: %v", got)
	}
}

// Choosing the in-process cache must not stamp blank Redis settings over the ones the
// install shipped: the operator may well switch to Redis later, and a wizard that
// quietly erased the address would make that a support call.
func TestCommitLeavesRedisSettingsAloneWhenNotChosen(t *testing.T) {
	path := writeConfig(t, sampleConfig)
	answers := Answers{
		DB:    DBSettings{Engine: "sqlite", DBName: "./data/app.db"},
		Cache: CacheSettings{Provider: "default"},
		Web:   WebSettings{EnableTLS: true, TLSPorts: []int{3002}, Hostnames: []string{"*"}},
		Admin: AdminSettings{Enabled: true, Username: "admin", Password: "kept"},
	}
	if err := commit(path, answers); err != nil {
		t.Fatalf("commit: %v", err)
	}
	doc := readConfig(t, path)
	if got := leaf(t, doc, "cache", "redis", "address"); got != "localhost:6379" {
		t.Fatalf("redis address overwritten with a blank: %v", got)
	}
	if got := leaf(t, doc, "cache", "redis", "password"); got != "stored-redis-pw" {
		t.Fatalf("redis password overwritten with a blank: %v", got)
	}
	if got := leaf(t, doc, "cache", "provider"); got != "default" {
		t.Fatalf("cache.provider = %v", got)
	}
}

/* ---------- validation ---------- */

func TestValidateRejectsUnusableAnswers(t *testing.T) {
	base := func() Answers {
		return Answers{
			DB:    DBSettings{Engine: "postgres", Host: "localhost", Port: 5432, User: "u", DBName: "d"},
			Cache: CacheSettings{Provider: "default"},
			Web:   WebSettings{EnableTLS: true, TLSPorts: []int{3002}, Hostnames: []string{"*"}},
			Admin: AdminSettings{Enabled: true, Username: "admin"},
		}
	}
	cases := []struct {
		name    string
		mutate  func(*Answers)
		wantSub string
	}{
		{"unknown engine", func(a *Answers) { a.DB.Engine = "oracle" }, "engine"},
		{"no host", func(a *Answers) { a.DB.Host = "" }, "host"},
		{"no database name", func(a *Answers) { a.DB.DBName = "" }, "database name"},
		{"port out of range", func(a *Answers) { a.DB.Port = 70000 }, "port"},
		{"sqlite with no file", func(a *Answers) { a.DB = DBSettings{Engine: "sqlite"} }, "file path"},
		{"redis with no address", func(a *Answers) { a.Cache = CacheSettings{Provider: "redis"} }, "Redis address"},
		{"no ports at all", func(a *Answers) { a.Web = WebSettings{} }, "at least one"},
		{"the same port twice", func(a *Answers) { a.Web.NonTLSPorts = []int{3002} }, "twice"},
		{"admin with no username", func(a *Answers) { a.Admin.Username = "" }, "username"},
		{"a too-short password", func(a *Answers) { a.Admin.Password = "short" }, "8 characters"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := base()
			tc.mutate(&a)
			err := validate(&a, 9080)
			if err == nil {
				t.Fatalf("expected an error mentioning %q", tc.wantSub)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("error %q does not mention %q", err, tc.wantSub)
			}
		})
	}
}

// Handing the app the port the wizard is sitting on produces an install that cannot
// bind on the very next boot — the unbootable state this whole feature exists to avoid.
func TestValidateRejectsTheWizardsOwnPort(t *testing.T) {
	a := Answers{
		DB:    DBSettings{Engine: "sqlite", DBName: "app.db"},
		Cache: CacheSettings{Provider: "default"},
		Web:   WebSettings{EnableTLS: false, NonTLSPorts: []int{9080}, Hostnames: []string{"*"}},
		Admin: AdminSettings{Enabled: false},
	}
	err := validate(&a, 9080)
	if err == nil || !strings.Contains(err.Error(), "setup page") {
		t.Fatalf("expected the reserved-port error, got %v", err)
	}
}

// SQLite has no server to reach, so leftovers from a previously configured engine must
// not be written into the file where they would read as meaningful configuration.
func TestValidateClearsServerFieldsForSQLite(t *testing.T) {
	a := Answers{
		DB:    DBSettings{Engine: "sqlite", DBName: "app.db", Host: "leftover", Port: 5432, User: "leftover", Password: "leftover", SSLMode: "require"},
		Cache: CacheSettings{Provider: "default"},
		Web:   WebSettings{NonTLSPorts: []int{8080}, Hostnames: []string{"*"}},
		Admin: AdminSettings{Enabled: false},
	}
	if err := validate(&a, 0); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if a.DB.Host != "" || a.DB.User != "" || a.DB.Password != "" || a.DB.SSLMode != "" || a.DB.Port != 0 {
		t.Fatalf("server fields survived a switch to SQLite: %+v", a.DB)
	}
}

// The host reads enableTls to decide whether to serve the TLS list at all, so ports the
// operator typed must not be silently dropped by an unticked box.
func TestValidateTakesTypedTLSPortsAsIntent(t *testing.T) {
	a := Answers{
		DB:    DBSettings{Engine: "sqlite", DBName: "app.db"},
		Cache: CacheSettings{Provider: "default"},
		Web:   WebSettings{EnableTLS: false, TLSPorts: []int{8443}, Hostnames: []string{"*"}},
		Admin: AdminSettings{Enabled: false},
	}
	if err := validate(&a, 0); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if !a.Web.EnableTLS {
		t.Fatal("typed HTTPS ports must switch HTTPS on rather than being discarded")
	}
}

func TestStartURL(t *testing.T) {
	cases := []struct {
		name string
		web  WebSettings
		want string
	}{
		{"https wins when enabled", WebSettings{EnableTLS: true, TLSPorts: []int{8443}, NonTLSPorts: []int{8080}}, "https://localhost:8443"},
		{"http when TLS is off", WebSettings{EnableTLS: false, NonTLSPorts: []int{8080}}, "http://localhost:8080"},
		{"nothing configured", WebSettings{}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := startURL(tc.web); got != tc.want {
				t.Fatalf("startURL = %q, want %q", got, tc.want)
			}
		})
	}
}
