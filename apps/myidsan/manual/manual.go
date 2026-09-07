// Package manual holds myidsan's built-in user manual — the articles a reader sees under Help,
// compiled into the binary so the documentation is always the one that matches the running
// software and always works with no network access.
//
// myidsan is the app where a missing manual costs the most. It is the first screen a stranger to
// the suite ever sees (every other app's sign-in redirects here), it is the one nobody can sign
// in to when it is misconfigured, and the questions it raises — where is the bootstrap password,
// why does my account have no role, why am I stuck on the enrolment screen — are all asked by
// somebody who is NOT authenticated. A manual on a website would be unreachable from the air-gapped
// side of the fleet, and a manual behind the session cookie would be unreachable at exactly the
// moment it is wanted. This one is neither.
//
// The articles are the source of truth and live as plain markdown under the language folders.
// Everything about how they are indexed, searched, printed and served is shared with the rest of
// the suite (domain/shared/manual); this package is only content plus the embed.
package manual

import (
	"embed"

	sharedmanual "github.com/mysayasan/kopiv2/domain/shared/manual"
)

// Adding a language means adding its folder here AND to this pattern. Adding an article means
// adding the file to EVERY language folder — apps/myidsan/manual/manual_test.go fails otherwise,
// which is the only reliable way a four-language manual stays four languages.
//
//go:embed en ms zh ar assets
var files embed.FS

// Library is myidsan's manual. Loading is lazy, so this costs nothing at init.
var Library = sharedmanual.New(files, ".")
