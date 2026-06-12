package versioning

import "testing"

func TestInfoForAppReturnsOnlySelectedApp(t *testing.T) {
	manifest := Manifest{
		Core: Entry{Version: "1.2.3"},
		Apps: map[string]Entry{
			"mymatasan": {Version: "2.3.4"},
			"other":     {Version: "9.9.9"},
		},
		Commit:    "abc123",
		UpdatedAt: "2026-06-07T00:00:00Z",
	}

	info, err := manifest.InfoForApp("mymatasan")
	if err != nil {
		t.Fatalf("InfoForApp failed: %v", err)
	}
	if info.App != "mymatasan" || info.AppVersion != "2.3.4" || info.CoreVersion != "1.2.3" {
		t.Fatalf("unexpected info: %+v", info)
	}
}

func TestBumpSemVer(t *testing.T) {
	tests := []struct {
		name  string
		start string
		level string
		want  string
	}{
		{name: "major", start: "1.2.3", level: "major", want: "2.0.0"},
		{name: "minor", start: "1.2.3", level: "minor", want: "1.3.0"},
		{name: "patch", start: "1.2.3", level: "patch", want: "1.2.4"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, err := ParseSemVer(tt.start)
			if err != nil {
				t.Fatalf("ParseSemVer failed: %v", err)
			}
			got, err := parsed.Bump(tt.level)
			if err != nil {
				t.Fatalf("Bump failed: %v", err)
			}
			if got.String() != tt.want {
				t.Fatalf("got %s want %s", got.String(), tt.want)
			}
		})
	}
}
