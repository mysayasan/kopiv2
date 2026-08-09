package services

import (
	"sort"

	"github.com/mysayasan/kopiv2/domain/entities"
)

// Page-level access: the model an administrator actually thinks in.
//
// The permission matrix (AccessRolePermission) is the right thing for a server to authorize
// against and the wrong thing for a human to edit. "/api/cameras/*/ptz + POST" is an engineer's
// model; "this role may use the camera controls" is the question being asked. Worse, an app's
// navigation was gated by something else entirely — a bool on the user — so the menu and the
// server were two independent sources of truth that nothing kept in agreement.
//
// A page catalog fixes both. An administrator grants a role PAGES at a LEVEL; the matrix is
// DERIVED from that; the navigation rail is rendered from the same page set. The menu and the
// enforcement cannot disagree because they stop being two things.
//
// Authorization itself is deliberately unchanged: the request path still consults the matrix, so
// this adds no middleware and no new way to fail open.

// PathGrant is one API path and the verbs a page level needs on it.
type PathGrant struct {
	Path      string
	CanGet    bool
	CanPost   bool
	CanPut    bool
	CanDelete bool
}

// Read and Write are the two grants almost every page level wants.
func Read(path string) PathGrant  { return PathGrant{Path: path, CanGet: true} }
func Write(path string) PathGrant { return PathGrant{Path: path, CanPost: true} }

// Full grants every verb — for the paths a "manage" level genuinely owns.
func Full(path string) PathGrant {
	return PathGrant{Path: path, CanGet: true, CanPost: true, CanPut: true, CanDelete: true}
}

// PageLevel is one rung of access to a page.
//
// Levels are CUMULATIVE and declared in increasing order: granting a level grants every level
// before it. That is what makes "manage without view" unrepresentable rather than merely
// discouraged, and it makes derivation a prefix take instead of a set union an author has to get
// right by hand.
type PageLevel struct {
	// Id is stable and stored against the role — "view", "use", "manage".
	Id string
	// Grants are the paths this rung adds on top of the rung before it.
	Grants []PathGrant
}

// Page is one navigable surface of an app.
type Page struct {
	// Id is stable, stored against a role, and is what the SPA maps to a nav entry and a label.
	// It is never shown to a user directly.
	Id string
	// Group is the nav group the page belongs to ("workspace", "surveillance", …).
	Group string
	Order int
	// Levels in increasing order of access. A page with a single level is all-or-nothing.
	Levels []PageLevel
	// AdminOnly marks a page that only a superadmin may ever hold. It is still listed, so the
	// catalog remains a complete description of the app's surface, but it cannot be granted.
	AdminOnly bool
}

// PageCatalog is an app's complete page surface plus the carve-outs its path shape requires.
type PageCatalog struct {
	Pages []Page
	// Baseline is granted to EVERY role, independently of any page.
	//
	// Some access is not about a page at all — it is about being signed in. Reading your own
	// session, changing your own password, checking whether first-run setup is done: a role
	// without these cannot get through the sign-in sequence to reach any page, so making them a
	// page an administrator could accidentally untick would mean an administrator could build a
	// role that cannot log in.
	//
	// This is exactly the defect that shipped once already — the matrix had no rule for the
	// session probe, and neither non-admin role could sign in at all.
	Baseline []PathGrant
	// Carveouts are paths that must ALWAYS receive an explicit all-false row unless some granted
	// page level names them.
	//
	// This exists because the matcher takes the MOST SPECIFIC matching rule and rules do not
	// union. A role granted GET on /api/settings, with no row at all for /api/settings/users,
	// can enumerate every account on the appliance — the broader grant governs because nothing
	// narrower exists to carve it out. An all-false row is how "…but not this part" is said.
	//
	// Getting this list wrong is the one way this package can hand out access nobody granted, so
	// every path that sits UNDER another page's grant belongs here.
	Carveouts []string
}

// PageGrant is a role holding one page at one level — what an administrator actually chose, and
// what is stored in access_role_page.
type PageGrant struct {
	PageId string
	Level  string
}

// Page returns a page by id.
func (c PageCatalog) Page(id string) (Page, bool) {
	for _, p := range c.Pages {
		if p.Id == id {
			return p, true
		}
	}
	return Page{}, false
}

// Sorted returns the catalog's pages in display order (Group, Order, Id).
func (c PageCatalog) Sorted() []Page {
	out := append([]Page(nil), c.Pages...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Group != out[j].Group {
			return out[i].Group < out[j].Group
		}
		if out[i].Order != out[j].Order {
			return out[i].Order < out[j].Order
		}
		return out[i].Id < out[j].Id
	})
	return out
}

// DerivePermissions turns what an administrator chose into the matrix rows the server enforces.
//
// It unions the grants of every held page up to its level, merging verbs per path, and then
// applies the catalog's carve-outs. The result is the COMPLETE managed matrix for that role —
// callers replace the role's managed rows with it wholesale, which is what makes removing a page
// actually remove the access rather than leaving an orphaned grant behind.
//
// An unknown page id or level is ignored rather than erroring: a catalog can lose a page across
// an upgrade, and a role still holding it should degrade to not having it, not break the boot.
func DerivePermissions(roleId int64, catalog PageCatalog, held []PageGrant) []entities.AccessRolePermission {
	merged := map[string]*entities.AccessRolePermission{}

	add := func(g PathGrant) {
		row := merged[g.Path]
		if row == nil {
			row = &entities.AccessRolePermission{RoleId: roleId, Path: g.Path, Managed: true}
			merged[g.Path] = row
		}
		row.CanGet = row.CanGet || g.CanGet
		row.CanPost = row.CanPost || g.CanPost
		row.CanPut = row.CanPut || g.CanPut
		row.CanDelete = row.CanDelete || g.CanDelete
	}

	// Baseline first: every role gets it, whatever pages it holds.
	for _, g := range catalog.Baseline {
		add(g)
	}

	for _, h := range held {
		page, ok := catalog.Page(h.PageId)
		if !ok || page.AdminOnly {
			continue
		}
		for _, level := range page.Levels {
			for _, g := range level.Grants {
				add(g)
			}
			// Cumulative: stop after the level the role actually holds.
			if level.Id == h.Level {
				break
			}
		}
	}

	// Carve-outs last, and only where nothing granted them: an explicit all-false row under a
	// broader grant is the only way to say "not this part".
	for _, path := range catalog.Carveouts {
		if _, granted := merged[path]; granted {
			continue
		}
		merged[path] = &entities.AccessRolePermission{RoleId: roleId, Path: path, Managed: true}
	}

	out := make([]entities.AccessRolePermission, 0, len(merged))
	for _, row := range merged {
		out = append(out, *row)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// HighestLevel returns a page's most permissive level id, which is what an admin-only or
// preset-everything grant means.
func (p Page) HighestLevel() string {
	if len(p.Levels) == 0 {
		return ""
	}
	return p.Levels[len(p.Levels)-1].Id
}

// HasLevel reports whether a page declares the given level.
func (p Page) HasLevel(id string) bool {
	for _, l := range p.Levels {
		if l.Id == id {
			return true
		}
	}
	return false
}
