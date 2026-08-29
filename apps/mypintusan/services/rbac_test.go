package services

import (
	"context"
	"testing"

	sharedentities "github.com/mysayasan/kopiv2/domain/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	sharedservices "github.com/mysayasan/kopiv2/domain/shared/services"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// mypintusan's authorization catalog had NO test at all until this file — the only app in the
// suite whose policy was never asserted — and what that was hiding is in
// TestPolicy_OperatorCanOpenADoor below.
//
// These tests seed the REAL catalog through the REAL permission service and ask the REAL matcher,
// so what they assert is what a running controller decides. A hand-written fixture of expected
// rows would have agreed with the catalog and been just as wrong.
type memPermRepo struct {
	dbsql.IGenericRepo[sharedentities.AccessRolePermission]
	rows   []*sharedentities.AccessRolePermission
	nextID int64
}

func (m *memPermRepo) Get(_ context.Context, _ string, _ uint64, _ uint64, filters []sqldataenums.Filter, _ []sqldataenums.Sorter) ([]*sharedentities.AccessRolePermission, uint64, error) {
	var out []*sharedentities.AccessRolePermission
	for _, r := range m.rows {
		match := true
		for _, f := range filters {
			if f.FieldName == "RoleId" {
				if id, ok := f.Value.(int64); ok && r.RoleId != id {
					match = false
				}
			}
		}
		if match {
			cp := *r
			out = append(out, &cp)
		}
	}
	return out, uint64(len(out)), nil
}

func (m *memPermRepo) Create(_ context.Context, _ string, row sharedentities.AccessRolePermission) (uint64, error) {
	m.nextID++
	row.Id = m.nextID
	cp := row
	m.rows = append(m.rows, &cp)
	return uint64(m.nextID), nil
}

func (m *memPermRepo) UpdateById(_ context.Context, _ string, row sharedentities.AccessRolePermission) (uint64, error) {
	for i, r := range m.rows {
		if r.Id == row.Id {
			cp := row
			m.rows[i] = &cp
			return 1, nil
		}
	}
	return 0, nil
}

func buildMatrix(t *testing.T) (perms sharedservices.IAccessPermissionService, viewer, operator int64) {
	t.Helper()
	ctx := context.Background()
	perms = sharedservices.NewAccessPermissionServiceWithRepo(&memPermRepo{})
	viewer, operator = int64(2), int64(3)

	for roleId, roleName := range map[int64]string{viewer: RoleViewer, operator: RoleOperator} {
		for _, row := range sharedservices.RolePermissions(roleId, roleName, Policy()) {
			if _, err := perms.Set(ctx, row); err != nil {
				t.Fatalf("seed %s %s: %v", roleName, row.Path, err)
			}
		}
	}
	return perms, viewer, operator
}

// THE test for this app. Opening a door remotely is the operator role's whole reason to exist —
// it is what a receptionist does all day, and rbac.go's header argues the point at length.
//
// The route is /api/doors/{id}/unlock. A catalog rule written "/api/doors/unlock" has three
// segments and can never match a four-segment request, so the most specific rule that DOES match
// is "/api/doors" — read-only. Every remote open by an operator was refused.
func TestPolicy_OperatorCanOpenADoor(t *testing.T) {
	perms, viewer, operator := buildMatrix(t)
	ctx := context.Background()

	if ok, _ := perms.Authorize(ctx, operator, "/api/doors/7/unlock", "POST"); !ok {
		t.Error("operator must be able to open a door remotely: POST /api/doors/7/unlock")
	}
	// The other half of the same claim: a viewer watches, and opens nothing.
	if ok, _ := perms.Authorize(ctx, viewer, "/api/doors/7/unlock", "POST"); ok {
		t.Error("viewer must NOT be able to open a door remotely")
	}
}

// Every catalog rule governs a route the app actually serves. A rule that matches no real request
// is not a grant and not a denial — it is a line of documentation shaped like policy, which is
// worse than an omission because it gets reviewed and believed.
func TestPolicy_EveryRuleGovernsARealRoute(t *testing.T) {
	for _, rule := range Policy() {
		matched := false
		for _, path := range servedRoutes {
			if sharedservices.PathGoverns(rule.Path, path) {
				matched = true
				break
			}
		}
		if !matched {
			t.Errorf("catalog rule %q governs no route this app serves", rule.Path)
		}
	}
}

// The mirror: every route the app serves is covered by SOME rule, so nothing is denied merely
// because nobody remembered to write it down. Deny-by-default is the right default and a silent
// one — the catalog is where a denial becomes a decision somebody made.
func TestPolicy_EveryRouteIsInTheCatalog(t *testing.T) {
	for _, path := range servedRoutes {
		covered := false
		for _, rule := range Policy() {
			if sharedservices.PathGoverns(rule.Path, path) {
				covered = true
				break
			}
		}
		if !covered {
			t.Errorf("route %q is in no catalog rule — nobody can see they are not granting it", path)
		}
	}
}

// servedRoutes is every request path this app answers, with ids filled in the way a browser sends
// them. Kept as data next to the two tests above so adding an endpoint without a catalog rule —
// or a catalog rule without an endpoint — fails the build rather than shipping.
var servedRoutes = []string{
	"/api/auth/session", "/api/auth/change-password", "/api/auth/capabilities",
	"/api/doors", "/api/doors/7", "/api/doors/7/unlock",
	"/api/readers",
	"/api/events",
	"/api/notifications", "/api/notifications/9/read",
	"/api/holders", "/api/holders/3", "/api/holders/3/credentials",
	"/api/holders/3/credentials/4/revoke",
	"/api/groups", "/api/groups/2", "/api/groups/2/members", "/api/groups/2/members/5",
	"/api/schedules", "/api/schedules/1", "/api/schedules/holidays", "/api/schedules/holidays/1",
	"/api/grants", "/api/grants/1",
	"/api/lockdown",
	"/api/settings/access", "/api/settings/access/reset",
	"/api/settings/roles", "/api/settings/users", "/api/settings/users/2",
	"/api/settings/users/2/password",
	"/api/setup/state", "/api/setup/complete",
	"/api/pairing/status", "/api/pairing/claim-code", "/api/pairing/unpair", "/api/pairing/fleet-key",
	"/api/deployment/preflight",
	// The administrative trail. audit.csv is listed separately because it is a separate SEGMENT —
	// "/api/audit" does not govern it, and a catalog that covered the listing but not the export
	// would leave the whole trail downloadable by a rule nobody wrote.
	"/api/audit", "/api/audit.csv",
}

// Signing in has to WORK for every role, which means more than /api/auth/login answering 200.
//
// The SPA's first call after sign-in is the session probe, and the matrix governs it like any other
// route. It was absent from the catalog, so it was denied by default: a viewer and an operator
// authenticated fine and were then handed the sign-in card again, with no error, permanently. The
// whole non-admin half of the product was unreachable through its own UI while every endpoint this
// catalog grants them answered 200 to a direct request.
func TestPolicy_EveryRoleCanCompleteSignIn(t *testing.T) {
	perms, viewer, operator := buildMatrix(t)
	ctx := context.Background()

	for _, role := range []struct {
		name string
		id   int64
	}{{"viewer", viewer}, {"operator", operator}} {
		for _, path := range []string{"/api/auth/session", "/api/auth/capabilities"} {
			if ok, _ := perms.Authorize(ctx, role.id, path, "GET"); !ok {
				t.Errorf("a %s cannot GET %s, so the app can never get past its own sign-in screen", role.name, path)
			}
		}
	}
}

// A viewer watches the estate and touches nothing.
func TestPolicy_ViewerWatchesAndTouchesNothing(t *testing.T) {
	perms, viewer, _ := buildMatrix(t)
	ctx := context.Background()

	for _, c := range []struct{ method, path string }{
		{"GET", "/api/doors"},
		{"GET", "/api/doors/7"},
		{"GET", "/api/readers"},
		{"GET", "/api/events"},
		{"GET", "/api/notifications"},
		{"GET", "/api/holders"},
		// Seeing that the site is sealed is not the same power as sealing it, and the Doors screen
		// loads this alongside the door list — so denying the read did not hide a pill, it blanked
		// the whole screen.
		{"GET", "/api/lockdown"},
		{"POST", "/api/auth/change-password"},
	} {
		if ok, _ := perms.Authorize(ctx, viewer, c.path, c.method); !ok {
			t.Errorf("viewer should be able to %s %s", c.method, c.path)
		}
	}

	for _, c := range []struct{ method, path string }{
		{"POST", "/api/doors/7/unlock"},
		{"POST", "/api/doors"},
		{"POST", "/api/holders"},
		{"POST", "/api/holders/3/credentials"},
		{"POST", "/api/holders/3/credentials/4/revoke"},
		{"POST", "/api/lockdown"},
		{"GET", "/api/grants"},
		{"GET", "/api/schedules"},
		{"PUT", "/api/settings/access"},
		{"GET", "/api/settings/users"},
	} {
		if ok, _ := perms.Authorize(ctx, viewer, c.path, c.method); ok {
			t.Errorf("viewer must NOT be able to %s %s", c.method, c.path)
		}
	}
}

// An operator runs the building: open a door, issue and revoke a badge, read the rules they have
// to work within. They do not EDIT those rules, and they do not seal the site.
func TestPolicy_OperatorRunsTheBuilding(t *testing.T) {
	perms, _, operator := buildMatrix(t)
	ctx := context.Background()

	for _, c := range []struct{ method, path string }{
		{"GET", "/api/doors"},
		{"POST", "/api/doors/7/unlock"},
		{"GET", "/api/holders"},
		{"POST", "/api/holders"},
		{"POST", "/api/holders/3/credentials"},
		{"POST", "/api/holders/3/credentials/4/revoke"},
		{"GET", "/api/events"},
		{"POST", "/api/notifications/9/read"},
		{"GET", "/api/groups"},
		{"GET", "/api/grants"},
		{"GET", "/api/schedules"},
		{"GET", "/api/lockdown"},
	} {
		if ok, _ := perms.Authorize(ctx, operator, c.path, c.method); !ok {
			t.Errorf("operator should be able to %s %s", c.method, c.path)
		}
	}

	// The two lines this app draws, restated as assertions: who may change the rules about who
	// gets in, and who may stop the building working.
	for _, c := range []struct{ method, path string }{
		{"POST", "/api/grants"},
		{"DELETE", "/api/grants/1"},
		{"POST", "/api/groups"},
		{"POST", "/api/groups/2/members"},
		{"DELETE", "/api/groups/2/members/5"},
		{"POST", "/api/schedules"},
		{"DELETE", "/api/schedules/1"},
		{"POST", "/api/schedules/holidays"},
		{"DELETE", "/api/schedules/holidays/1"},
		{"POST", "/api/lockdown"},
		{"POST", "/api/doors"},
		{"PUT", "/api/settings/access"},
		{"POST", "/api/settings/users"},
		{"GET", "/api/settings/users"},
		{"PUT", "/api/pairing/fleet-key"},
		{"POST", "/api/setup/complete"},
	} {
		if ok, _ := perms.Authorize(ctx, operator, c.path, c.method); ok {
			t.Errorf("operator must NOT be able to %s %s", c.method, c.path)
		}
	}
}
