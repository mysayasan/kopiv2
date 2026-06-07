package versioning

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

//go:embed version.json
var embedded embed.FS

var semverPattern = regexp.MustCompile(`^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$`)

// Entry stores one SemVer value in the version manifest.
type Entry struct {
	Version string `json:"version"`
}

// Manifest stores core and app version state generated from pending changelog entries.
type Manifest struct {
	Core      Entry            `json:"core"`
	Apps      map[string]Entry `json:"apps"`
	Commit    string           `json:"commit"`
	UpdatedAt string           `json:"updatedAt"`
}

// Info is the public version payload exposed to one running app.
type Info struct {
	App         string `json:"app"`
	AppVersion  string `json:"appVersion"`
	CoreVersion string `json:"coreVersion"`
	Commit      string `json:"commit,omitempty"`
	UpdatedAt   string `json:"updatedAt,omitempty"`
}

// SemVer is the numeric form used by the bump workflow.
type SemVer struct {
	Major int
	Minor int
	Patch int
}

// LoadDefault reads the embedded version manifest.
func LoadDefault() (Manifest, error) {
	data, err := embedded.ReadFile("version.json")
	if err != nil {
		return Manifest{}, err
	}
	return DecodeManifest(data)
}

// DecodeManifest parses and validates manifest JSON.
func DecodeManifest(data []byte) (Manifest, error) {
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, err
	}
	if err := manifest.Validate(); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

// Validate checks that all manifest versions are standard major.minor.patch SemVer values.
func (m Manifest) Validate() error {
	if _, err := ParseSemVer(m.Core.Version); err != nil {
		return fmt.Errorf("invalid core version: %w", err)
	}
	for appName, entry := range m.Apps {
		if strings.TrimSpace(appName) == "" {
			return errors.New("app version key cannot be empty")
		}
		if _, err := ParseSemVer(entry.Version); err != nil {
			return fmt.Errorf("invalid app version for %s: %w", appName, err)
		}
	}
	return nil
}

// InfoForApp returns only the selected app's version plus the shared core version.
func (m Manifest) InfoForApp(appName string) (Info, error) {
	appName = strings.TrimSpace(appName)
	if appName == "" {
		return Info{}, errors.New("app name is required")
	}

	entry, ok := m.Apps[appName]
	if !ok {
		return Info{}, fmt.Errorf("version for app %q not found", appName)
	}

	return Info{
		App:         appName,
		AppVersion:  entry.Version,
		CoreVersion: m.Core.Version,
		Commit:      m.Commit,
		UpdatedAt:   m.UpdatedAt,
	}, nil
}

// ParseSemVer parses a strict major.minor.patch SemVer value.
func ParseSemVer(value string) (SemVer, error) {
	value = strings.TrimSpace(value)
	match := semverPattern.FindStringSubmatch(value)
	if match == nil {
		return SemVer{}, fmt.Errorf("%q is not major.minor.patch", value)
	}

	major, _ := strconv.Atoi(match[1])
	minor, _ := strconv.Atoi(match[2])
	patch, _ := strconv.Atoi(match[3])
	return SemVer{Major: major, Minor: minor, Patch: patch}, nil
}

func (v SemVer) String() string {
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
}

// Bump returns the next SemVer value for major, minor, or patch level.
func (v SemVer) Bump(level string) (SemVer, error) {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "major":
		return SemVer{Major: v.Major + 1, Minor: 0, Patch: 0}, nil
	case "minor":
		return SemVer{Major: v.Major, Minor: v.Minor + 1, Patch: 0}, nil
	case "patch":
		return SemVer{Major: v.Major, Minor: v.Minor, Patch: v.Patch + 1}, nil
	default:
		return SemVer{}, fmt.Errorf("unsupported version level %q", level)
	}
}
