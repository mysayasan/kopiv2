package versioning

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

// changelogGroups is the render order for a changelog entry's sections
// (Keep a Changelog convention).
var changelogGroups = []string{"Added", "Changed", "Deprecated", "Removed", "Fixed", "Security"}

// changelogGroup maps a change's type to its Keep-a-Changelog section. Anything
// that isn't an explicit category (docs/cleanup/refactor, or a bare
// major/minor/patch level) is recorded under "Changed" so nothing is lost.
func changelogGroup(change Change) string {
	switch strings.ToLower(strings.TrimSpace(change.Type)) {
	case "added":
		return "Added"
	case "deprecated":
		return "Deprecated"
	case "removed":
		return "Removed"
	case "fixed", "fix":
		return "Fixed"
	case "security":
		return "Security"
	default:
		return "Changed"
	}
}

// changeScopeLabel is the bold prefix shown before a change's summary line. It
// prefers the explicit app, then the scope, defaulting to "core".
func changeScopeLabel(change Change) string {
	if app := strings.TrimSpace(change.App); app != "" {
		return app
	}
	if scope := strings.TrimSpace(change.Scope); scope != "" {
		return scope
	}
	return "core"
}

// RenderChangelogEntry builds one dated CHANGELOG section for a bump run: a
// heading listing every component whose version changed (apps sorted, core last)
// plus the short commit, followed by the summaries grouped by type.
func RenderChangelogEntry(changes []Change, manifest Manifest, bumpedCore bool, bumpedApps []string, commit string, now time.Time) string {
	if len(changes) == 0 {
		return ""
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}

	versions := make([]string, 0, len(bumpedApps)+1)
	sortedApps := append([]string(nil), bumpedApps...)
	sort.Strings(sortedApps)
	for _, app := range sortedApps {
		versions = append(versions, fmt.Sprintf("%s %s", app, manifest.Apps[app].Version))
	}
	if bumpedCore {
		versions = append(versions, fmt.Sprintf("core %s", manifest.Core.Version))
	}

	heading := now.UTC().Format("2006-01-02")
	if len(versions) > 0 {
		heading += " — " + strings.Join(versions, ", ")
	}
	if short := shortCommit(commit); short != "" {
		heading += fmt.Sprintf(" (%s)", short)
	}

	grouped := map[string][]Change{}
	for _, change := range changes {
		g := changelogGroup(change)
		grouped[g] = append(grouped[g], change)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "## %s\n\n", heading)
	for _, group := range changelogGroups {
		items := grouped[group]
		if len(items) == 0 {
			continue
		}
		fmt.Fprintf(&b, "### %s\n\n", group)
		for _, change := range items {
			fmt.Fprintf(&b, "- **%s**: %s\n", changeScopeLabel(change), strings.TrimSpace(change.Summary))
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}

func shortCommit(commit string) string {
	commit = strings.TrimSpace(commit)
	if len(commit) > 7 {
		return commit[:7]
	}
	return commit
}

const changelogIntro = "# Changelog\n\nAll notable changes to this project, generated from `changes/` entries on each version bump. Newest first.\n"

// PrependChangelogEntry inserts entry at the top of the changelog file (right
// after the title), creating the file with a standard header when absent so the
// newest release is always first.
func PrependChangelogEntry(path, entry string) error {
	entry = strings.TrimRight(entry, "\n") + "\n"

	existing := ""
	if data, err := os.ReadFile(path); err == nil {
		existing = string(data)
	} else if !os.IsNotExist(err) {
		return err
	}

	var out string
	if strings.HasPrefix(strings.TrimSpace(existing), "# Changelog") {
		// Splice the new entry in front of the first existing "## " section so it
		// leads the list; if there are no prior entries, append after the header.
		if idx := strings.Index(existing, "\n## "); idx >= 0 {
			out = existing[:idx+1] + "\n" + entry + existing[idx+1:]
		} else {
			out = strings.TrimRight(existing, "\n") + "\n\n" + entry
		}
	} else {
		out = changelogIntro + "\n" + entry
		if strings.TrimSpace(existing) != "" {
			out += "\n" + existing
		}
	}

	return os.WriteFile(path, []byte(out), 0644)
}
