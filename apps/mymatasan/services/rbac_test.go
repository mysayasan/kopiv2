package services

import (
	"context"
	"testing"

	sharedentities "github.com/mysayasan/kopiv2/domain/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	sharedservices "github.com/mysayasan/kopiv2/domain/shared/services"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// memPermRepo is an in-memory permission store, so these tests exercise the REAL permission
// service and the REAL matcher against the REAL catalog — the only thing faked is the disk.
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

// buildMatrix seeds the REAL catalog through the REAL permission service, so these tests
// assert what a running install would actually decide — not what a hand-written fixture says.
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

// THE test. An operator who was present at an incident must not be able to delete the footage
// of it — that is the property that makes an NVR evidentiary rather than a camera viewer, and
// it was not expressible before this phase.
func TestPolicy_NobodyBelowAdminCanDestroyEvidence(t *testing.T) {
	perms, viewer, operator := buildMatrix(t)
	ctx := context.Background()

	destructive := []struct{ method, path string }{
		{"DELETE", "/api/recording/segments/9"},
		{"POST", "/api/recording/segments/purge"},
		{"POST", "/api/recording/purge-camera"},
		{"DELETE", "/api/cameras/3"},
		{"POST", "/api/system/reset"},
		{"POST", "/api/system/wipe"},
		{"DELETE", "/api/vision/rules/2"},
		{"DELETE", "/api/observations/4"},
	}

	for _, role := range []struct {
		name string
		id   int64
	}{{"viewer", viewer}, {"operator", operator}} {
		for _, c := range destructive {
			if ok, _ := perms.Authorize(ctx, role.id, c.path, c.method); ok {
				t.Errorf("%s must NOT be able to %s %s", role.name, c.method, c.path)
			}
		}
	}
}

// Nobody below admin may reconfigure the system either — a role that can rewrite the AI rules
// or repoint a camera can make evidence not exist in the first place.
func TestPolicy_NobodyBelowAdminCanReconfigure(t *testing.T) {
	perms, viewer, operator := buildMatrix(t)
	ctx := context.Background()

	forbidden := []struct{ method, path string }{
		{"POST", "/api/cameras"},           // add a camera
		{"PUT", "/api/cameras/3"},          // repoint one
		{"POST", "/api/vision/rules"},      // change what gets detected
		{"PUT", "/api/settings/runtime"},   // change how it runs
		{"POST", "/api/settings/users"},    // create an account
		{"GET", "/api/settings/users"},     // enumerate accounts
		{"GET", "/api/settings/roles"},     // enumerate the authorization model
		{"POST", "/api/onvif/discover"},    // scan the network
		{"POST", "/api/training/datasets"}, // train a model
		{"POST", "/api/pairing/adopt"},     // hand the node to a control plane
		{"PUT", "/api/recording/config"},   // change retention
		{"POST", "/api/teach/skills"},      // teach a skill
		{"PUT", "/api/anomaly/settings"},   // retune the anomaly monitor
	}

	for _, role := range []struct {
		name string
		id   int64
	}{{"viewer", viewer}, {"operator", operator}} {
		for _, c := range forbidden {
			if ok, _ := perms.Authorize(ctx, role.id, c.path, c.method); ok {
				t.Errorf("%s must NOT be able to %s %s", role.name, c.method, c.path)
			}
		}
	}
}

// A viewer watches live and sees that an alert fired. That is the whole role.
func TestPolicy_ViewerCanWatchLiveAndNothingElse(t *testing.T) {
	perms, viewer, _ := buildMatrix(t)
	ctx := context.Background()

	for _, c := range []struct{ method, path string }{
		{"GET", "/api/cameras"},
		{"GET", "/api/cameras/3"},
		{"POST", "/api/cameras/3/live-view"},
		{"POST", "/api/cameras/3/webrtc/offer"},
		{"GET", "/api/cameras/3/live.mjpeg"},
		{"POST", "/api/cameras/health/refresh"},
		{"GET", "/api/vision/alerts"},
		{"GET", "/api/notifications"},
		{"POST", "/api/auth/change-password"},
		{"GET", "/api/settings/runtime"},
	} {
		if ok, _ := perms.Authorize(ctx, viewer, c.path, c.method); !ok {
			t.Errorf("viewer should be able to %s %s", c.method, c.path)
		}
	}

	// The line that viewer/operator draws: a viewer cannot go back and watch what happened,
	// and cannot act on an alert.
	for _, c := range []struct{ method, path string }{
		{"GET", "/api/recording/segments"},
		{"GET", "/api/recording/segments/9/download"},
		{"GET", "/api/observations"},
		{"POST", "/api/vision/alerts/9/ack"},
		{"POST", "/api/notifications/9/read"},
		{"POST", "/api/cameras/3/ptz/move"},
		{"POST", "/api/cameras/3/talk/offer"},
	} {
		if ok, _ := perms.Authorize(ctx, viewer, c.path, c.method); ok {
			t.Errorf("viewer must NOT be able to %s %s", c.method, c.path)
		}
	}
}

// An operator does the day-to-day job: review the footage, acknowledge the alert, move the
// camera, talk through it. Everything today's non-admin could do, plus PTZ and talk — which is
// why an existing non-admin is backfilled to this role and not to viewer.
func TestPolicy_OperatorCanDoTheJob(t *testing.T) {
	perms, _, operator := buildMatrix(t)
	ctx := context.Background()

	for _, c := range []struct{ method, path string }{
		// Everything a viewer can do.
		{"GET", "/api/cameras"},
		{"POST", "/api/cameras/3/webrtc/offer"},
		{"GET", "/api/vision/alerts"},

		// Review what happened.
		{"GET", "/api/recording/segments"},
		{"GET", "/api/recording/segments/9/download"},
		{"GET", "/api/observations"},

		// Act on it.
		{"POST", "/api/vision/alerts/9/ack"},
		{"POST", "/api/notifications/9/read"},
		{"POST", "/api/cameras/3/ptz/move"},
		{"POST", "/api/cameras/3/ptz/stop"},
		{"POST", "/api/cameras/3/talk/offer"},
	} {
		if ok, _ := perms.Authorize(ctx, operator, c.path, c.method); !ok {
			t.Errorf("operator should be able to %s %s", c.method, c.path)
		}
	}
}

// Deny-by-default. A route nobody thought about is denied, rather than allowed because it
// happens to be a GET — which is what the old rule did for EVERY read in the system.
func TestPolicy_UnknownRouteIsDenied(t *testing.T) {
	perms, viewer, operator := buildMatrix(t)
	ctx := context.Background()

	for _, roleId := range []int64{viewer, operator} {
		for _, method := range []string{"GET", "POST", "PUT", "DELETE"} {
			if ok, _ := perms.Authorize(ctx, roleId, "/api/some-future-endpoint", method); ok {
				t.Errorf("role %d: an endpoint nobody granted must be denied (%s)", roleId, method)
			}
		}
	}
}

// A user with no role at all — a freshly created account an admin has not finished setting up
// — can do nothing.
func TestPolicy_RolelessUserIsDeniedEverything(t *testing.T) {
	perms, _, _ := buildMatrix(t)
	ctx := context.Background()

	for _, c := range []struct{ method, path string }{
		{"GET", "/api/cameras"},
		{"GET", "/api/settings/runtime"},
		{"POST", "/api/auth/change-password"},
	} {
		if ok, _ := perms.Authorize(ctx, 0, c.path, c.method); ok {
			t.Errorf("a user with no role must be denied %s %s", c.method, c.path)
		}
	}
}

// Every rule in the catalog must be a path the matcher can actually use. A typo here is a
// permission that silently governs nothing.
func TestPolicy_CatalogPathsAreWellFormed(t *testing.T) {
	seen := map[string]bool{}
	for _, rule := range Policy() {
		if rule.Path == "" || rule.Path[0] != '/' {
			t.Errorf("catalog path %q must be absolute", rule.Path)
		}
		if rule.Description == "" {
			t.Errorf("catalog path %q has no description — the admin UI renders these", rule.Path)
		}
		if seen[rule.Path] {
			t.Errorf("catalog path %q is listed twice", rule.Path)
		}
		seen[rule.Path] = true
	}
}

// THE regression test for the bug that made the whole three-role model a fiction: neither
// viewer nor operator could sign in AT ALL.
//
// The SPA walks a fixed path before it can render anything — probe the session, load runtime
// settings, list the cameras. Deny any step and sign-in fails with "you do not have permission
// for this action" AFTER the password was accepted, which reads like a broken account rather
// than a missing catalog rule. Only superadmin ever worked, because superadmin bypasses the
// matrix and so never exercised the path these tests now cover.
//
// Every other test in this file asserts what a role must NOT do. This one asserts the floor:
// that the role can get through the door at all.
func TestPolicy_NonAdminRolesCanCompleteSignIn(t *testing.T) {
	perms, viewer, operator := buildMatrix(t)
	ctx := context.Background()

	// In the order App.js calls them.
	signIn := []struct{ method, path, why string }{
		{"GET", "/api/auth/session", "the probe that reveals the role and the must-change flag"},
		{"GET", "/api/settings/runtime", "runtime settings, loaded before the shell renders"},
		{"GET", "/api/cameras", "the camera list the landing view is built from"},
		{"POST", "/api/auth/change-password", "the forced first-login change, if pending"},
	}

	for _, role := range []struct {
		name string
		id   int64
	}{{"viewer", viewer}, {"operator", operator}} {
		for _, c := range signIn {
			if ok, _ := perms.Authorize(ctx, role.id, c.path, c.method); !ok {
				t.Errorf("%s cannot sign in: %s %s is denied (%s)", role.name, c.method, c.path, c.why)
			}
		}
	}
}
