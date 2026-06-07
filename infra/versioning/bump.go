package versioning

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Change describes one pending version change consumed by the GitHub workflow.
type Change struct {
	Level   string `json:"level"`
	Scope   string `json:"scope"`
	App     string `json:"app"`
	Summary string `json:"summary"`
}

// ApplyOptions configures pending changelog consumption.
type ApplyOptions struct {
	ManifestPath string
	PendingDir   string
	AppliedDir   string
	Commit       string
	Now          time.Time
}

// ApplyResult summarizes a version bump run.
type ApplyResult struct {
	Changed        bool
	ProcessedFiles []string
	Manifest       Manifest
}

// ApplyPendingChanges bumps the manifest from pending changelog entries and moves them to applied.
func ApplyPendingChanges(opts ApplyOptions) (ApplyResult, error) {
	if strings.TrimSpace(opts.ManifestPath) == "" {
		return ApplyResult{}, errors.New("manifest path is required")
	}
	if strings.TrimSpace(opts.PendingDir) == "" {
		return ApplyResult{}, errors.New("pending directory is required")
	}
	if strings.TrimSpace(opts.AppliedDir) == "" {
		return ApplyResult{}, errors.New("applied directory is required")
	}

	manifestData, err := os.ReadFile(opts.ManifestPath)
	if err != nil {
		return ApplyResult{}, err
	}
	manifest, err := DecodeManifest(manifestData)
	if err != nil {
		return ApplyResult{}, err
	}

	changeFiles, err := pendingChangeFiles(opts.PendingDir)
	if err != nil {
		return ApplyResult{}, err
	}
	if len(changeFiles) == 0 {
		return ApplyResult{Manifest: manifest}, nil
	}

	for _, path := range changeFiles {
		change, err := readChange(path)
		if err != nil {
			return ApplyResult{}, fmt.Errorf("%s: %w", path, err)
		}
		if err := applyChange(&manifest, change); err != nil {
			return ApplyResult{}, fmt.Errorf("%s: %w", path, err)
		}
	}

	if opts.Now.IsZero() {
		opts.Now = time.Now().UTC()
	}
	manifest.Commit = strings.TrimSpace(opts.Commit)
	manifest.UpdatedAt = opts.Now.UTC().Format(time.RFC3339)

	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return ApplyResult{}, err
	}
	encoded = append(encoded, '\n')
	if err := os.WriteFile(opts.ManifestPath, encoded, 0644); err != nil {
		return ApplyResult{}, err
	}

	if err := movePendingParents(changeFiles, opts.PendingDir, opts.AppliedDir); err != nil {
		return ApplyResult{}, err
	}

	return ApplyResult{Changed: true, ProcessedFiles: changeFiles, Manifest: manifest}, nil
}

func pendingChangeFiles(pendingDir string) ([]string, error) {
	if _, err := os.Stat(pendingDir); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}

	var files []string
	err := filepath.WalkDir(pendingDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if strings.EqualFold(filepath.Base(path), "change.json") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

func readChange(path string) (Change, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Change{}, err
	}
	var change Change
	if err := json.Unmarshal(data, &change); err != nil {
		return Change{}, err
	}
	return change, validateChange(change)
}

func validateChange(change Change) error {
	level := strings.ToLower(strings.TrimSpace(change.Level))
	if level != "major" && level != "minor" && level != "patch" {
		return fmt.Errorf("level must be major, minor, or patch")
	}

	scope := strings.ToLower(strings.TrimSpace(change.Scope))
	if scope != "core" && scope != "app" && scope != "both" {
		return fmt.Errorf("scope must be core, app, or both")
	}
	if (scope == "app" || scope == "both") && strings.TrimSpace(change.App) == "" {
		return fmt.Errorf("app is required when scope is app or both")
	}
	if strings.TrimSpace(change.Summary) == "" {
		return fmt.Errorf("summary is required")
	}
	return nil
}

func applyChange(manifest *Manifest, change Change) error {
	level := strings.ToLower(strings.TrimSpace(change.Level))
	scope := strings.ToLower(strings.TrimSpace(change.Scope))

	if scope == "core" || scope == "both" {
		next, err := bumpVersion(manifest.Core.Version, level)
		if err != nil {
			return err
		}
		manifest.Core.Version = next
	}

	if scope == "app" || scope == "both" {
		appName := strings.TrimSpace(change.App)
		if manifest.Apps == nil {
			manifest.Apps = map[string]Entry{}
		}
		entry := manifest.Apps[appName]
		if strings.TrimSpace(entry.Version) == "" {
			entry.Version = "0.0.0"
		}
		next, err := bumpVersion(entry.Version, level)
		if err != nil {
			return err
		}
		entry.Version = next
		manifest.Apps[appName] = entry
	}

	return manifest.Validate()
}

func bumpVersion(value string, level string) (string, error) {
	parsed, err := ParseSemVer(value)
	if err != nil {
		return "", err
	}
	next, err := parsed.Bump(level)
	if err != nil {
		return "", err
	}
	return next.String(), nil
}

func movePendingParents(changeFiles []string, pendingDir string, appliedDir string) error {
	parents := map[string]struct{}{}
	for _, file := range changeFiles {
		parents[filepath.Clean(filepath.Dir(file))] = struct{}{}
	}

	orderedParents := make([]string, 0, len(parents))
	for parent := range parents {
		orderedParents = append(orderedParents, parent)
	}
	sort.Strings(orderedParents)

	if err := os.MkdirAll(appliedDir, 0755); err != nil {
		return err
	}

	for _, parent := range orderedParents {
		rel, err := filepath.Rel(pendingDir, parent)
		if err != nil {
			return err
		}
		if rel == "." {
			return fmt.Errorf("change files must be inside timestamped folders under %s", pendingDir)
		}
		if strings.HasPrefix(rel, "..") {
			return fmt.Errorf("pending change path %s is outside %s", parent, pendingDir)
		}

		target := filepath.Join(appliedDir, rel)
		if _, err := os.Stat(target); err == nil {
			target = target + "-" + time.Now().UTC().Format("20060102150405")
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		if err := os.Rename(parent, target); err != nil {
			return err
		}
	}

	return nil
}
