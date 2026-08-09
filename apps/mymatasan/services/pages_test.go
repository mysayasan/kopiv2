package services

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"

	sharedentities "github.com/mysayasan/kopiv2/domain/entities"

	sharedservices "github.com/mysayasan/kopiv2/domain/shared/services"
)

// verbs renders a permission row as a stable string, so a diff between two matrices reads.
func verbs(canGet, canPost, canPut, canDelete bool) string {
	out := ""
	for _, v := range []struct {
		on bool
		ch string
	}{{canGet, "G"}, {canPost, "P"}, {canPut, "U"}, {canDelete, "D"}} {
		if v.on {
			out += v.ch
		} else {
			out += "-"
		}
	}
	return out
}

// matrixFromPolicy renders what a role is granted TODAY, straight from the catalog that has been
// seeding installs all along.
func matrixFromPolicy(roleName string) map[string]string {
	out := map[string]string{}
	for _, row := range sharedservices.RolePermissions(1, roleName, Policy()) {
		out[row.Path] = verbs(row.CanGet, row.CanPost, row.CanPut, row.CanDelete)
	}
	return out
}

// matrixFromPages renders what the same role would be granted through its page preset.
func matrixFromPages(roleName string) map[string]string {
	out := map[string]string{}
	for _, row := range sharedservices.DerivePermissions(1, Pages(), RolePresets()[roleName]) {
		out[row.Path] = verbs(row.CanGet, row.CanPost, row.CanPut, row.CanDelete)
	}
	return out
}

// diff describes how two matrices differ, in a form somebody can act on.
func diff(want, got map[string]string) []string {
	paths := map[string]bool{}
	for p := range want {
		paths[p] = true
	}
	for p := range got {
		paths[p] = true
	}
	var keys []string
	for p := range paths {
		keys = append(keys, p)
	}
	sort.Strings(keys)

	var out []string
	for _, p := range keys {
		w, inWant := want[p]
		g, inGot := got[p]
		// An all-false row on either side grants nothing. Policy() writes many of them purely so
		// the matrix reads as a complete description of the API surface, and their ABSENCE from
		// the derived matrix changes no decision: deny-by-default already refuses anything the
		// role was not granted. What matters — and what this test is for — is a path where the
		// two sides grant DIFFERENT verbs, in either direction.
		switch {
		case inWant && !inGot:
			if w == "----" {
				continue
			}
			out = append(out, fmt.Sprintf("  LOST     %-34s policy grants %s, pages grant nothing", p, w))
		case !inWant && inGot:
			if g == "----" {
				continue
			}
			out = append(out, fmt.Sprintf("  WIDENED  %-34s pages grant %s, policy grants nothing", p, g))
		case w != g:
			out = append(out, fmt.Sprintf("  DIFFERS  %-34s policy %s, pages %s", p, w, g))
		}
	}
	return out
}

// THE safety property of page-level access.
//
// Deriving a built-in role from its page preset must produce exactly the grants the shipped
// Policy() catalog produces. If it does not, adopting pages silently changed what an existing
// role can reach — and a widened role is precisely how a usability refactor becomes a security
// regression. This is the test that makes the migration provably a no-op.
func TestPagePresets_MatchTodaysPolicyExactly(t *testing.T) {
	for _, roleName := range []string{RoleViewer, RoleOperator} {
		t.Run(roleName, func(t *testing.T) {
			differences := diff(matrixFromPolicy(roleName), matrixFromPages(roleName))
			if len(differences) > 0 {
				t.Errorf("the %s page preset does not reproduce its current permissions:\n%s",
					roleName, strings.Join(differences, "\n"))
			}
		})
	}
}

// The derived matrix must decide the same way the current one does, not merely look similar. This
// asserts against the REAL matcher, on the cases the role model exists to express.
func TestPagePresets_AuthorizeIdenticallyToPolicy(t *testing.T) {
	ctx := context.Background()

	probes := []struct{ method, path string }{
		{"GET", "/api/auth/session"},
		{"POST", "/api/auth/change-password"},
		{"GET", "/api/cameras"},
		{"POST", "/api/cameras/3/webrtc/offer"},
		{"POST", "/api/cameras/3/ptz/move"},
		{"POST", "/api/cameras/3/talk/offer"},
		{"GET", "/api/vision/alerts"},
		{"POST", "/api/vision/alerts/9/ack"},
		{"GET", "/api/notifications"},
		{"POST", "/api/notifications/9/read"},
		{"GET", "/api/recording/segments"},
		{"DELETE", "/api/recording/segments/9"},
		{"GET", "/api/observations"},
		{"GET", "/api/settings"},
		{"GET", "/api/settings/users"},
		{"GET", "/api/settings/roles"},
		{"POST", "/api/settings/users"},
		{"POST", "/api/onvif/discover"},
		{"POST", "/api/system/reset"},
		{"GET", "/api/some-future-endpoint"},
	}

	for _, roleName := range []string{RoleViewer, RoleOperator} {
		policy := newMatrix(t, sharedservices.RolePermissions(1, roleName, Policy()))
		pages := newMatrix(t, sharedservices.DerivePermissions(1, Pages(), RolePresets()[roleName]))

		for _, p := range probes {
			wantOk, _ := policy.Authorize(ctx, 1, p.path, p.method)
			gotOk, _ := pages.Authorize(ctx, 1, p.path, p.method)
			if wantOk != gotOk {
				t.Errorf("%s: %s %s — policy says %v, pages say %v", roleName, p.method, p.path, wantOk, gotOk)
			}
		}
	}
}

// The carve-outs are the one piece that can hand out access nobody granted. A role holding Live
// Views is granted GET on /api/settings so the shell can render; without an explicit all-false
// row beneath it, the most-specific-wins matcher lets that role enumerate every account on the
// appliance.
func TestPages_CarveOutsDenyWhatABroaderGrantWouldExpose(t *testing.T) {
	ctx := context.Background()
	m := newMatrix(t, sharedservices.DerivePermissions(1, Pages(), []sharedservices.PageGrant{
		{PageId: PageLiveViews, Level: LevelView},
	}))

	if ok, _ := m.Authorize(ctx, 1, "/api/settings", "GET"); !ok {
		t.Error("Live Views needs to read runtime settings to render at all")
	}
	for _, path := range []string{"/api/settings/users", "/api/settings/roles"} {
		if ok, _ := m.Authorize(ctx, 1, path, "GET"); ok {
			t.Errorf("%s must be carved out — a broader grant on /api/settings exposed it", path)
		}
	}
}

// Levels are cumulative: holding "use" must imply "view". A role that could act on a page it
// cannot see would be a UI that shows nothing and an API that accepts writes.
func TestPages_LevelsAreCumulative(t *testing.T) {
	ctx := context.Background()
	m := newMatrix(t, sharedservices.DerivePermissions(1, Pages(), []sharedservices.PageGrant{
		{PageId: PageLiveViews, Level: LevelUse},
	}))

	for _, p := range []struct{ method, path string }{
		{"GET", "/api/cameras"},                 // the view rung
		{"POST", "/api/cameras/3/webrtc/offer"}, // the view rung
		{"POST", "/api/cameras/3/ptz/move"},     // the use rung
	} {
		if ok, _ := m.Authorize(ctx, 1, p.path, p.method); !ok {
			t.Errorf("level %q should include %s %s", LevelUse, p.method, p.path)
		}
	}
}

// An admin-only page can never be handed out, even if a role somehow holds a row for it — a
// stale row from a downgrade must not become an escalation.
func TestPages_AdminOnlyPagesCannotBeGranted(t *testing.T) {
	ctx := context.Background()
	m := newMatrix(t, sharedservices.DerivePermissions(1, Pages(), []sharedservices.PageGrant{
		{PageId: PageSettings, Level: LevelManage},
		{PageId: PageCameras, Level: LevelManage},
	}))

	for _, p := range []struct{ method, path string }{
		{"POST", "/api/settings/users"},
		{"POST", "/api/system/reset"},
		{"DELETE", "/api/cameras/3"},
	} {
		if ok, _ := m.Authorize(ctx, 1, p.path, p.method); ok {
			t.Errorf("an admin-only page was granted: %s %s", p.method, p.path)
		}
	}
}

// A page id that no longer exists must degrade to "not held", not break the boot — a catalog can
// lose a page across an upgrade while a role still holds a row for it. The baseline is still
// granted, because that is what lets the affected user sign in and be fixed.
func TestPages_UnknownPageIsIgnored(t *testing.T) {
	baseline := map[string]bool{}
	for _, g := range Pages().Baseline {
		baseline[g.Path] = true
	}
	rows := sharedservices.DerivePermissions(1, Pages(), []sharedservices.PageGrant{
		{PageId: "a-page-from-a-future-release", Level: "view"},
	})
	for _, r := range rows {
		if baseline[r.Path] {
			continue
		}
		if r.CanGet || r.CanPost || r.CanPut || r.CanDelete {
			t.Errorf("an unknown page granted %s", r.Path)
		}
	}
}

// Every path a page level grants must be one the API catalog actually governs. A page granting a
// path Policy() has never heard of is a permission that governs nothing, or worse, a typo that
// silently grants nothing while the UI claims otherwise.
func TestPages_GrantOnlyPathsThePolicyGoverns(t *testing.T) {
	governed := map[string]bool{}
	for _, rule := range Policy() {
		governed[rule.Path] = true
	}
	for _, page := range Pages().Pages {
		for _, level := range page.Levels {
			for _, g := range level.Grants {
				if !governed[g.Path] {
					t.Errorf("page %q level %q grants %q, which Policy() does not govern", page.Id, level.Id, g.Path)
				}
			}
		}
	}
	for _, path := range Pages().Carveouts {
		if !governed[path] {
			t.Errorf("carve-out %q is not a path Policy() governs", path)
		}
	}
}

// newMatrix builds a real permission service over an in-memory store (memPermRepo, shared with
// rbac_test.go), seeded with the given rows — so these tests exercise the REAL matcher rather
// than re-implementing its precedence rules, which is where the interesting behaviour lives.
func newMatrix(t *testing.T, rows []sharedentities.AccessRolePermission) sharedservices.IAccessPermissionService {
	t.Helper()
	perms := sharedservices.NewAccessPermissionServiceWithRepo(&memPermRepo{})
	for _, row := range rows {
		if _, err := perms.Set(context.Background(), row); err != nil {
			t.Fatalf("seed %s: %v", row.Path, err)
		}
	}
	return perms
}
