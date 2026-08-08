// Package manual holds mymatasan's built-in user manual — the articles a reader sees under Help,
// compiled into the binary so the documentation is always the one that matches the running
// software and always works with no network access.
//
// The articles themselves are the source of truth and live as plain markdown under the language
// folders. Everything about how they are indexed, searched, printed and served is shared with the
// rest of the suite (domain/shared/manual); this package is only content plus the embed.
package manual

import (
	"embed"

	sharedmanual "github.com/mysayasan/kopiv2/domain/shared/manual"
)

// Adding a language means adding its folder here AND to this pattern. Adding an article means
// adding the file to EVERY language folder — apps/mymatasan/manual/manual_test.go fails otherwise,
// which is the only reliable way a four-language manual stays four languages.
//
//go:embed en ms zh ar assets
var files embed.FS

// Library is mymatasan's manual. Loading is lazy, so this costs nothing at init.
var Library = sharedmanual.New(files, ".")
