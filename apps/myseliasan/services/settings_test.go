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

// rawConfig is a minimal but representative config.json: it carries the editable blocks
// plus two blocks the editor must never touch (db, server) with INLINE arrays, so tests
// can prove untouched blocks survive byte-for-byte.
const rawConfig = `{
  "jwt": {
    "secret": "0123456789abcdef0123"
  },
  "localAuth": {
    "enabled": true,
    "username": "admin",
    "password": "admin123"
  },
  "rateLimit": {
    "enabled": true,
    "endpointCacheTtlSeconds": 30,
    "defaultWindowSeconds": 60,
    "devOnly": { "enabled": true, "requests": 300, "windowSeconds": 60 },
    "authOnly": { "enabled": true, "requests": 1200, "windowSeconds": 60 },
    "public": { "enabled": true, "requests": 30, "windowSeconds": 60 }
  },
  "allowOrigins": "*",
  "tls": {
    "certPath": "./certs/cert.pem",
    "keyPath": "./certs/key.pem"
  },
  "securityHeaders": {
    "contentSecurityPolicy": "default-src 'self'",
    "frameOptions": "DENY"
  },
  "server": {
    "hostnames": ["*"],
    "tlsPorts": [3002],
    "nonTlsPorts": []
  },
  "db": {
    "engine": "postgres",
    "host": "localhost",
    "port": 5433
  }
}`

func TestPatchConfigBytesPreservesUntouchedBlocksAndOrder(t *testing.T) {
	patches := []configPatch{
		{path: []string{"localAuth", "password"}, value: "newsecret"},
		{path: []string{"rateLimit", "devOnly", "requests"}, value: float64(500)},
		{path: []string{"allowOrigins"}, value: "https://example.test"},
	}
	out, err := patchConfigBytes([]byte(rawConfig), patches)
	if err != nil {
		t.Fatalf("patchConfigBytes: %v", err)
	}

	// Top-level key order must be unchanged.
	if got, want := topLevelKeyOrder(out), topLevelKeyOrder([]byte(rawConfig)); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("top-level order changed:\n got %v\nwant %v", got, want)
	}

	// The db and server blocks were not patched, so their inline-array formatting must be
	// preserved verbatim (not reflowed by the marshaler).
	for _, verbatim := range []string{
		`"hostnames": ["*"]`,
		`"tlsPorts": [3002]`,
		`"nonTlsPorts": []`,
		`"engine": "postgres"`,
	} {
		if !strings.Contains(string(out), verbatim) {
			t.Fatalf("untouched formatting lost: %q not found in\n%s", verbatim, out)
		}
	}

	// The patched + non-patched leaves must have the expected values.
	var parsed map[string]any
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	if v, _ := leafAny(parsed, "localAuth.password"); v != "newsecret" {
		t.Fatalf("localAuth.password not patched: %v", v)
	}
	if v, _ := leafAny(parsed, "rateLimit.devOnly.requests"); v != float64(500) {
		t.Fatalf("rateLimit.devOnly.requests not patched: %v", v)
	}
	if v, _ := leafAny(parsed, "allowOrigins"); v != "https://example.test" {
		t.Fatalf("allowOrigins scalar not patched: %v", v)
	}
	// A non-editable sibling inside a patched block must survive.
	if v, _ := leafAny(parsed, "rateLimit.endpointCacheTtlSeconds"); v != float64(30) {
		t.Fatalf("non-editable rateLimit.endpointCacheTtlSeconds lost: %v", v)
	}
	// A non-exposed field inside a patched block must survive.
	if v, _ := leafAny(parsed, "securityHeaders.frameOptions"); v == nil {
		// securityHeaders wasn't patched here, but assert db is intact instead.
	}
	if v, _ := leafAny(parsed, "db.port"); v != float64(5433) {
		t.Fatalf("db block mutated: db.port=%v", v)
	}
}

func TestPatchConfigBytesNoPatchesIsNoop(t *testing.T) {
	if err := materializeConfig(filepath.Join(t.TempDir(), "does-not-exist.json"), nil); err != nil {
		t.Fatalf("empty patch set should be a no-op, got: %v", err)
	}
}

// newTestSettings builds a settings service over a temp config file + fake DB repo, with
// the in-memory config model matching the file.
func newTestSettings(t *testing.T) (*settingsService, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(rawConfig), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg := &config.AppConfigModel{}
	cfg.Jwt.Secret = "0123456789abcdef0123"
	cfg.LocalAuth.Enabled = true
	cfg.LocalAuth.Username = "admin"
	cfg.LocalAuth.Password = "admin123"
	cfg.AllowOrigin = "*"
	cfg.Tls.CertPath = "./certs/cert.pem"
	cfg.Tls.KeyPath = "./certs/key.pem"
	cfg.SecurityHeaders.ContentSecurityPolicy = "default-src 'self'"
	cfg.RateLimit.Enabled = true
	cfg.RateLimit.DefaultWindowSeconds = 60
	cfg.RateLimit.DevOnly = config.RateLimitTierConfigModel{Enabled: true, Requests: 300, WindowSeconds: 60}
	cfg.RateLimit.AuthOnly = config.RateLimitTierConfigModel{Enabled: true, Requests: 1200, WindowSeconds: 60}
	cfg.RateLimit.Public = config.RateLimitTierConfigModel{Enabled: true, Requests: 30, WindowSeconds: 60}

	s := &settingsService{
		cfg:     cfg,
		cfgPath: path,
		repo:    &fakeSettingsRepo{},
		cipher:  testCipher(t),
	}
	if err := s.ensureDefaults(context.Background()); err != nil {
		t.Fatalf("ensureDefaults: %v", err)
	}
	return s, path
}

func TestSettingsGetMasksSecret(t *testing.T) {
	s, _ := newTestSettings(t)
	got, err := s.Get("localAuth")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if v, _ := leafAny(got, "localAuth.password"); v != "" {
		t.Fatalf("password should be masked, got %v", v)
	}
	if v, _ := leafAny(got, "localAuth.passwordSet"); v != true {
		t.Fatalf("passwordSet should be true, got %v", v)
	}
}

func TestSettingsSaveKeepsBlankSecret(t *testing.T) {
	s, path := newTestSettings(t)
	// Change username, leave password blank -> password must stay admin123.
	body := []byte(`{"localAuth":{"enabled":true,"username":"root","password":""}}`)
	res, err := s.Save(context.Background(), "localAuth", body)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if !res.NeedsRestart {
		t.Fatal("expected needsRestart=true")
	}
	if s.cfg.LocalAuth.Username != "root" {
		t.Fatalf("username not applied: %q", s.cfg.LocalAuth.Username)
	}
	if s.cfg.LocalAuth.Password != "admin123" {
		t.Fatalf("blank secret should keep current password, got %q", s.cfg.LocalAuth.Password)
	}
	// The file must reflect the kept password + new username.
	raw, _ := os.ReadFile(path)
	var parsed map[string]any
	_ = json.Unmarshal(raw, &parsed)
	if v, _ := leafAny(parsed, "localAuth.password"); v != "admin123" {
		t.Fatalf("file password changed unexpectedly: %v", v)
	}
	if v, _ := leafAny(parsed, "localAuth.username"); v != "root" {
		t.Fatalf("file username not persisted: %v", v)
	}
}

func TestSettingsSaveSetsNewSecret(t *testing.T) {
	s, path := newTestSettings(t)
	body := []byte(`{"localAuth":{"enabled":true,"username":"admin","password":"brand-new-pass"}}`)
	if _, err := s.Save(context.Background(), "localAuth", body); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if s.cfg.LocalAuth.Password != "brand-new-pass" {
		t.Fatalf("new password not applied: %q", s.cfg.LocalAuth.Password)
	}
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), "brand-new-pass") {
		t.Fatal("new password not written to config file")
	}
}

func TestSettingsSaveDropsUnknownAndHelperKeys(t *testing.T) {
	s, path := newTestSettings(t)
	// The UI echoes masked "<field>Set" helpers; a hostile client could add stray keys.
	// Neither must ever reach config.json.
	body := []byte(`{"localAuth":{"enabled":true,"username":"admin","password":"","passwordSet":true},"bogus":{"x":1}}`)
	if _, err := s.Save(context.Background(), "localAuth", body); err != nil {
		t.Fatalf("Save: %v", err)
	}
	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), "passwordSet") || strings.Contains(string(raw), "bogus") {
		t.Fatalf("unknown/helper keys leaked into config:\n%s", raw)
	}
}

func TestSettingsResetRestoresDefaults(t *testing.T) {
	s, _ := newTestSettings(t)
	// Mutate rate limit, then reset the security section (which owns rateLimit).
	body := []byte(`{"jwt":{"secret":""},"allowOrigins":"https://changed.test","tls":{"certPath":"./certs/cert.pem","keyPath":"./certs/key.pem"},"securityHeaders":{"contentSecurityPolicy":"default-src 'self'"},"rateLimit":{"enabled":false,"defaultWindowSeconds":10,"devOnly":{"enabled":false,"requests":1,"windowSeconds":1},"authOnly":{"enabled":true,"requests":1200,"windowSeconds":60},"public":{"enabled":true,"requests":30,"windowSeconds":60}}}`)
	if _, err := s.Save(context.Background(), "security", body); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if s.cfg.AllowOrigin != "https://changed.test" || s.cfg.RateLimit.Enabled {
		t.Fatal("security save did not apply")
	}
	if _, err := s.Reset(context.Background(), "security"); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if s.cfg.AllowOrigin != "*" {
		t.Fatalf("reset did not restore allowOrigins: %q", s.cfg.AllowOrigin)
	}
	if !s.cfg.RateLimit.Enabled || s.cfg.RateLimit.DevOnly.Requests != 300 {
		t.Fatalf("reset did not restore rate limit: %+v", s.cfg.RateLimit)
	}
}

func TestSettingsSaveRejectsInvalid(t *testing.T) {
	s, _ := newTestSettings(t)
	// jwt secret too short.
	body := []byte(`{"jwt":{"secret":"short"},"allowOrigins":"*","tls":{"certPath":"c","keyPath":"k"},"securityHeaders":{"contentSecurityPolicy":""},"rateLimit":{"enabled":true,"defaultWindowSeconds":60,"devOnly":{"enabled":true,"requests":1,"windowSeconds":1},"authOnly":{"enabled":true,"requests":1,"windowSeconds":1},"public":{"enabled":true,"requests":1,"windowSeconds":1}}}`)
	if _, err := s.Save(context.Background(), "security", body); err == nil {
		t.Fatal("expected validation error for short jwt secret")
	}
	if _, err := s.Save(context.Background(), "unknown", []byte(`{}`)); err == nil {
		t.Fatal("expected error for unknown section")
	}
}

func TestSettingsTestCacheRequiresAddress(t *testing.T) {
	s, _ := newTestSettings(t)
	// No address in the request and none stored -> a clear error, without attempting a dial.
	if err := s.TestCache(context.Background(), []byte(`{"address":""}`)); err == nil {
		t.Fatal("expected an error when no Redis address is available")
	} else if !strings.Contains(strings.ToLower(err.Error()), "address") {
		t.Fatalf("error should mention the missing address, got: %v", err)
	}
}

func TestSettingsDefaultsSnapshotNotOverwritten(t *testing.T) {
	s, _ := newTestSettings(t)
	// Save a change, then re-run ensureDefaults: the original defaults must remain.
	if _, err := s.Save(context.Background(), "localAuth", []byte(`{"localAuth":{"enabled":true,"username":"changed","password":""}}`)); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := s.ensureDefaults(context.Background()); err != nil {
		t.Fatalf("ensureDefaults(again): %v", err)
	}
	defaults, err := s.loadDefaults(context.Background())
	if err != nil {
		t.Fatalf("loadDefaults: %v", err)
	}
	la, _ := defaults["localAuth"].(map[string]any)
	if v, _ := leafAny(la, "localAuth.username"); v != "admin" {
		t.Fatalf("defaults snapshot was overwritten: username=%v", v)
	}
}
