package services

import (
	"strings"
	"testing"
)

func TestPythonDownloadURL(t *testing.T) {
	cases := []struct {
		goos, goarch string
		wantErr      bool
		exeSuffix    string
		tripleFrag   string
	}{
		{"linux", "amd64", false, "python3", "x86_64-unknown-linux-gnu"},
		{"linux", "arm64", false, "python3", "aarch64-unknown-linux-gnu"},
		{"windows", "amd64", false, "python.exe", "x86_64-pc-windows-msvc"},
		{"windows", "arm64", false, "python.exe", "aarch64-pc-windows-msvc"},
		{"darwin", "amd64", true, "", ""},
	}
	for _, c := range cases {
		url, exe, err := pythonDownloadURL(c.goos, c.goarch)
		if c.wantErr {
			if err == nil {
				t.Errorf("%s/%s: expected unsupported error", c.goos, c.goarch)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s/%s: unexpected error %v", c.goos, c.goarch, err)
			continue
		}
		if !strings.Contains(url, pyStandaloneVersion) || !strings.Contains(url, pyStandaloneRelease) {
			t.Errorf("%s/%s: url missing pinned version/release: %s", c.goos, c.goarch, url)
		}
		if !strings.Contains(url, c.tripleFrag) || !strings.HasSuffix(url, "install_only.tar.gz") {
			t.Errorf("%s/%s: url shape wrong: %s", c.goos, c.goarch, url)
		}
		if !strings.HasSuffix(exe, c.exeSuffix) {
			t.Errorf("%s/%s: exe %q should end with %q", c.goos, c.goarch, exe, c.exeSuffix)
		}
	}
}
