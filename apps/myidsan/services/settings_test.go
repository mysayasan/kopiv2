package services

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mysayasan/kopiv2/infra/config"
)

// idsanRawConfig is a minimal but representative myidsan config.json. It carries the
// editable blocks, an `audit` block the editor must NEVER touch, and `db`/`server` blocks
// so tests can prove untouched blocks survive byte-for-byte.
const idsanRawConfig = `{
  "jwt": {
    "secret": "0123456789abcdef0123"
  },
  "sso": {
    "issuer": "myidsan",
    "audience": "myidsan,myseliasan",
    "sessionTtlSeconds": 259200,
    "authCodeTtlSeconds": 300,
    "accessTokenTtlSeconds": 900,
    "policyCacheTtlSeconds": 60,
    "internalToken": "super-secret-introspection-token"
  },
  "localAuth": {
    "enabled": true,
    "username": "admin",
    "password": "admin123"
  },
  "audit": {
    "retention": { "enabled": false, "maxRetentionDays": 365 }
  },
  "db": { "engine": "sqlite", "db_name": "./data/idsan.db" },
  "server": { "hostnames": ["*"], "tlsPorts": [3001] }
}`

func newIdsanSettings(t *testing.T) (ISettingsService, *config.AppConfigModel, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(idsanRawConfig), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg := &config.AppConfigModel{}
	if err := json.Unmarshal([]byte(idsanRawConfig), cfg); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}
	// nil db/cipher: the defaults snapshot is best-effort and only Reset depends on it,
	// which these tests do not exercise.
	return NewSettingsService(cfg, path, nil, nil, nil), cfg, path
}

// THE ONE THAT MATTERS MOST. audit.retention is the security trail's only removal path,
// and it is config-file-only ON PURPOSE: trimming the trail must require filesystem access
// to the server rather than a session on it, so the superadmin whose own actions the trail
// records cannot reach for it from inside the product. Surfacing it in this editor — even
// read-only, even by accident through a wildcard — hands back exactly the capability that
// design withholds.
func TestAuditRetentionIsNotReachableFromTheSettingsEditor(t *testing.T) {
	svc, _, _ := newIdsanSettings(t)

	for _, id := range svc.Sections() {
		if id == "audit" {
			t.Fatal("audit is exposed as an editable section; retention must stay config-file-only")
		}
	}
	if _, err := svc.Get("audit"); err == nil {
		t.Fatal("Get(\"audit\") succeeded; the section must be unknown to the editor")
	}

	// And it must not appear nested inside any other section's payload either.
	//
	// Matched on the AUDIT-specific shape, not on a bare "maxRetentionDays": logging and
	// apiLog have their own cleanup.maxRetentionDays fields which are unrelated to the
	// security trail and are legitimately editable. A needle that catches those fails for
	// the wrong reason and would have to be weakened until it caught nothing at all.
	all := svc.GetAll()
	if _, ok := all["audit"]; ok {
		t.Fatalf("an 'audit' key is present in the settings payload: %v", all["audit"])
	}
	blob, err := json.Marshal(all)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, needle := range []string{`"audit"`, `"retention"`} {
		if strings.Contains(string(blob), needle) {
			t.Fatalf("%s leaked into the settings payload: %s", needle, blob)
		}
	}
}

// A save must not be able to reach it either, whatever the caller posts.
func TestSavingAuditRetentionIsRefused(t *testing.T) {
	svc, cfg, path := newIdsanSettings(t)

	if _, err := svc.Save(context.Background(), "audit",
		json.RawMessage(`{"audit":{"retention":{"enabled":true,"maxRetentionDays":1}}}`)); err == nil {
		t.Fatal("saving an unknown 'audit' section must be refused")
	}
	// Smuggling it through a section that IS editable must be dropped by the shape
	// projection rather than written through.
	if _, err := svc.Save(context.Background(), "logging",
		json.RawMessage(`{"audit":{"retention":{"enabled":true,"maxRetentionDays":1}}}`)); err != nil {
		t.Fatalf("logging save: %v", err)
	}
	if cfg.Audit.Retention.Enabled {
		t.Fatal("audit retention was enabled in memory via a logging save")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("config is no longer valid JSON: %v", err)
	}
	audit, _ := doc["audit"].(map[string]any)
	ret, _ := audit["retention"].(map[string]any)
	if ret["enabled"] != false {
		t.Fatalf("audit.retention was rewritten on disk: %v", ret)
	}
}

// Secrets are never returned. The SSO internal token authenticates server-to-server
// introspection, so echoing it to a settings form would put a bearer credential into
// every browser devtools panel and HAR file.
func TestSecretsAreMasked(t *testing.T) {
	svc, _, _ := newIdsanSettings(t)

	sso, err := svc.Get("sso")
	if err != nil {
		t.Fatalf("Get(sso): %v", err)
	}
	blob, _ := json.Marshal(sso)
	if strings.Contains(string(blob), "super-secret-introspection-token") {
		t.Fatalf("internalToken leaked: %s", blob)
	}

	local, err := svc.Get("localAuth")
	if err != nil {
		t.Fatalf("Get(localAuth): %v", err)
	}
	blob, _ = json.Marshal(local)
	if strings.Contains(string(blob), "admin123") {
		t.Fatalf("local password leaked: %s", blob)
	}
}

// A blank incoming secret means "leave it alone" — otherwise merely opening the form and
// pressing Save would wipe the token, because the form never received it in the first place.
func TestBlankSecretKeepsTheStoredValue(t *testing.T) {
	svc, cfg, _ := newIdsanSettings(t)

	if _, err := svc.Save(context.Background(), "sso", json.RawMessage(
		`{"sso":{"issuer":"myidsan","audience":"myidsan","sessionTtlSeconds":100,"authCodeTtlSeconds":60,"accessTokenTtlSeconds":300,"policyCacheTtlSeconds":30,"internalToken":""}}`)); err != nil {
		t.Fatalf("save: %v", err)
	}
	if cfg.SSO.InternalToken != "super-secret-introspection-token" {
		t.Fatalf("a blank secret wiped the stored token: %q", cfg.SSO.InternalToken)
	}
	if cfg.SSO.SessionTTLSeconds != 100 {
		t.Fatalf("the non-secret field did not save: %d", cfg.SSO.SessionTTLSeconds)
	}
}

// The identity policies are absent from the shipped config and resolve through
// Effective*(). The editor must show what is IN FORCE, not the zero values of an absent
// block — otherwise the page reports the lockout as off while it is actually on, and an
// operator "fixes" a setting that was never broken.
func TestSecurityShowsResolvedPolicyDefaultsNotZeroValues(t *testing.T) {
	svc, _, _ := newIdsanSettings(t)

	sec, err := svc.Get("security")
	if err != nil {
		t.Fatalf("Get(security): %v", err)
	}
	login, _ := sec["loginSecurity"].(map[string]any)
	if login == nil {
		t.Fatal("security section does not expose loginSecurity")
	}
	if login["enabled"] != true {
		t.Fatalf("lockout reported as %v; an absent block resolves to ON and the page must say so", login["enabled"])
	}
	if n, _ := login["maxAttempts"].(int); n <= 1 {
		t.Fatalf("maxAttempts reported as %v — the zero value of an absent block, not the resolved default", login["maxAttempts"])
	}

	pw, _ := sec["passwordPolicy"].(map[string]any)
	if pw == nil {
		t.Fatal("security section does not expose passwordPolicy")
	}
	if n, _ := pw["minLength"].(int); n < 8 {
		t.Fatalf("password minLength reported as %v, not the resolved default", pw["minLength"])
	}

	mfa, _ := sec["mfa"].(map[string]any)
	if mfa == nil || strings.TrimSpace(mfa["policy"].(string)) == "" {
		t.Fatalf("mfa policy missing or empty: %v", mfa)
	}
}

// Validation must refuse settings that look like policy but aren't.
func TestSecurityValidationRefusesUselessPolicies(t *testing.T) {
	svc, _, _ := newIdsanSettings(t)
	base := func(overrides string) json.RawMessage {
		return json.RawMessage(`{"jwt":{"secret":"0123456789abcdef0123"},"allowOrigins":"*",` +
			`"tls":{"certPath":"c","keyPath":"k"},"securityHeaders":{"contentSecurityPolicy":"default-src 'self'"},` +
			`"rateLimit":{"enabled":true,"defaultWindowSeconds":60,` +
			`"devOnly":{"enabled":true,"requests":1,"windowSeconds":1},` +
			`"authOnly":{"enabled":true,"requests":1,"windowSeconds":1},` +
			`"public":{"enabled":true,"requests":1,"windowSeconds":1}},` + overrides + `}`)
	}
	cases := map[string]string{
		"lockout that trips before the first attempt": `"loginSecurity":{"enabled":true,"maxAttempts":1},"passwordPolicy":{"minLength":12},"mfa":{"policy":"optional"}`,
		"password floor below 8":                      `"loginSecurity":{"enabled":true,"maxAttempts":8},"passwordPolicy":{"minLength":4},"mfa":{"policy":"optional"}`,
		"unknown mfa policy":                          `"loginSecurity":{"enabled":true,"maxAttempts":8},"passwordPolicy":{"minLength":12},"mfa":{"policy":"mandatory"}`,
	}
	for name, override := range cases {
		if _, err := svc.Save(context.Background(), "security", base(override)); err == nil {
			t.Fatalf("%s was accepted", name)
		}
	}
}

// An authorization code lives in a redirect URL, so it reaches browser history, proxy logs
// and Referer headers. A long-lived one is a credential sitting in all of those.
func TestLongAuthCodeTTLIsRefused(t *testing.T) {
	svc, _, _ := newIdsanSettings(t)
	if _, err := svc.Save(context.Background(), "sso", json.RawMessage(
		`{"sso":{"issuer":"myidsan","audience":"myidsan","sessionTtlSeconds":100,"authCodeTtlSeconds":86400,"accessTokenTtlSeconds":300,"policyCacheTtlSeconds":30,"internalToken":""}}`)); err == nil {
		t.Fatal("a 24-hour authorization-code TTL was accepted")
	}
}

// Blocks the editor never exposes must survive a save byte-for-byte, so an edit can never
// reformat or damage the parts that would take the server offline.
func TestUntouchedBlocksSurviveASave(t *testing.T) {
	svc, _, path := newIdsanSettings(t)

	if _, err := svc.Save(context.Background(), "localAuth",
		json.RawMessage(`{"localAuth":{"enabled":true,"username":"root","password":""}}`)); err != nil {
		t.Fatalf("save: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	for _, fragment := range []string{
		`"db": { "engine": "sqlite", "db_name": "./data/idsan.db" }`,
		`"server": { "hostnames": ["*"], "tlsPorts": [3001] }`,
	} {
		if !strings.Contains(string(raw), fragment) {
			t.Fatalf("an untouched block was reformatted or lost; expected verbatim:\n%s\ngot:\n%s", fragment, raw)
		}
	}
}

// Every save needs a restart, because the host reads these blocks once at boot. Reporting
// otherwise would leave an operator believing a change had taken effect.
func TestSaveReportsNeedsRestart(t *testing.T) {
	svc, _, _ := newIdsanSettings(t)
	res, err := svc.Save(context.Background(), "localAuth",
		json.RawMessage(`{"localAuth":{"enabled":true,"username":"root","password":""}}`))
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if !res.NeedsRestart {
		t.Fatal("a save must report needsRestart; the host only reads these blocks at boot")
	}
}
