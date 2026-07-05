package app

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/mysayasan/kopiv2/apps/mymatasan/services"
	"github.com/mysayasan/kopiv2/infra/config"
)

func TestFirstRunConsoleURL(t *testing.T) {
	cases := []struct {
		name string
		set  func(*config.AppConfigModel)
		want string
	}{
		{"tls", func(c *config.AppConfigModel) { c.Server.TLSPorts = []int{3000} }, "https://localhost:3000"},
		{"nontls", func(c *config.AppConfigModel) { c.Server.NonTLSPorts = []int{8080} }, "http://localhost:8080"},
		{"plain-ports", func(c *config.AppConfigModel) { c.Server.Ports = []int{9000} }, "http://localhost:9000"},
		{"default", func(c *config.AppConfigModel) {}, "http://localhost:3000"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.AppConfigModel{}
			tc.set(cfg)
			if got := firstRunConsoleURL(cfg); got != tc.want {
				t.Fatalf("firstRunConsoleURL = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestWriteFirstRunCredentialFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, firstRunCredentialFile)
	seed := services.AdminSeedResult{Seeded: true, Username: "admin", Password: "Xh7Kqmn23PdRtuvw", Generated: true}

	if err := writeFirstRunCredentialFile(path, "https://localhost:3000", seed); err != nil {
		t.Fatalf("writeFirstRunCredentialFile: %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	body := string(b)
	for _, want := range []string{"admin", seed.Password, "https://localhost:3000", "first sign-in", "Delete this file"} {
		if !strings.Contains(body, want) {
			t.Fatalf("credential file missing %q; got:\n%s", want, body)
		}
	}
	// The bootstrap password sits in plaintext on disk, so it must be owner-only.
	// Unix perms aren't meaningfully enforced on Windows (Go reports 0666), so the
	// mode assertion only applies elsewhere.
	if runtime.GOOS != "windows" {
		if info, err := os.Stat(path); err == nil {
			if perm := info.Mode().Perm(); perm&0o077 != 0 {
				t.Fatalf("credential file mode = %o, want no group/other access", perm)
			}
		}
	}
}
