package services

import (
	"context"
	"fmt"

	sharedentities "github.com/mysayasan/kopiv2/domain/entities"
	sharedservices "github.com/mysayasan/kopiv2/domain/shared/services"
)

// mymatasan's role model.
//
// Three roles, and the line between them is: CAN THIS PERSON DESTROY EVIDENCE?
//
//	viewer    watch live and see that an alert fired. No access to recorded footage.
//	operator  + review and download footage, acknowledge alerts, PTZ, talk-back.
//	          CANNOT delete or purge anything, cannot change AI rules or settings,
//	          cannot add or remove cameras.
//	admin     everything (a superadmin role — it bypasses the matrix).
//
// That middle role is the whole point of the phase. An operator who was present at an
// incident must not be able to delete the footage of it. Before this, authorization was one
// bool: admin got everything, and everyone else got every GET plus a hardcoded allow-list —
// so "may watch cameras but may not delete recordings" was not expressible.
//
// Three and no more. Every extra role is a support burden and a matrix the customer will
// misconfigure. The matrix stays editable for the site that genuinely needs a fourth.
const (
	// RoleAdmin is the shared superadmin builtin. It is named "superadmin" in the database
	// (the shared stack seeds it) and shown as "Administrator" in the UI.
	RoleAdmin = sharedservices.RoleSuperadmin
	// RoleOperator is mymatasan's own role: the day-to-day security operator.
	RoleOperator = "operator"
	// RoleViewer is the shared viewer builtin: read-only.
	RoleViewer = sharedservices.RoleViewer
)

// verbs is the four-verb grant on one path.
type verbs struct {
	Get    bool
	Post   bool
	Put    bool
	Delete bool
}

var (
	read  = verbs{Get: true}
	write = verbs{Post: true}
	none  = verbs{}
)

// PolicyRule is one governed area of the API and what each built-in role may do in it.
//
// This catalog is the ONLY place mymatasan's authorization surface is described. It is
// deliberately Go data rather than rows an installer clicks into a grid: a policy you can
// review in a diff is a policy somebody actually reviews.
//
// The paths are matched segment-wise, and "*" matches one segment — which is what lets an
// action be granted without granting its whole collection. "/api/cameras/*/ptz" says an
// operator may move a camera; it does not say they may create one.
//
// The MOST SPECIFIC matching rule decides (rules do not union), so a broad grant can be
// carved out by a narrower one. That is how "/api/recording" is readable while nothing
// beneath it is deletable.
type PolicyRule struct {
	Path        string
	Description string
	Viewer      verbs
	Operator    verbs
}

// Policy returns mymatasan's authorization catalog.
//
// admin is absent by design: it is a superadmin role and bypasses the matrix entirely, so it
// needs no rules. Anything NOT listed here is denied to viewer and operator — the matrix is
// deny-by-default, which is the opposite of the old rule (every GET allowed to everybody).
//
// The ladder is drawn so the upgrade is not a regression. Today's non-admin can watch live,
// review footage, and acknowledge alerts, and is backfilled to OPERATOR, which keeps all of
// it. VIEWER is the genuinely stricter role the old model could not express: watch live and
// see that an alert happened, without access to the recorded footage.
func Policy() []PolicyRule {
	return []PolicyRule{
		// --- Everyone signed in ---------------------------------------------------------
		{"/api/auth/change-password", "Change your own password", write, write},

		// --- Watching live ----------------------------------------------------------------
		{"/api/cameras", "See the camera list and camera details", read, read},
		{"/api/cameras/health/refresh", "Re-probe camera reachability (the sign-in health check)", write, write},
		{"/api/cameras/*/live-view", "Open a live view", write, write},
		{"/api/cameras/*/webrtc/offer", "Negotiate the live video stream", write, write},

		// --- Seeing that something happened -----------------------------------------------
		{"/api/vision", "See AI detection rules and the alert log", read, read},
		{"/api/notifications", "See the notification feed", read, read},
		{"/api/capacity", "See how much the host can handle", read, read},
		// Settings is readable so the app can render — decoder settings drive live view. The
		// user-management routes beneath it keep their own admin self-gate, so a viewer who
		// can read /api/settings still cannot read /api/settings/users.
		{"/api/settings", "Read runtime settings", read, read},
		{"/api/setup", "Read first-run setup state", read, read},

		// --- Reviewing recorded footage (operator and up) ----------------------------------
		// THIS is the line viewer/operator draws: a viewer watches live and sees that an alert
		// fired; only an operator can go back and watch what actually happened.
		//
		// Read only. Every mutating verb beneath these is denied because it is never granted:
		// deleting a segment, purging a camera's footage. THAT denial is the evidentiary line
		// — an operator who was present at an incident cannot delete the footage of it.
		{"/api/recording", "Play back and download recorded footage", none, read},
		{"/api/observations", "Search what objects a camera saw", none, read},

		// --- Operating (operator only) ----------------------------------------------------
		{"/api/vision/alerts/*/ack", "Acknowledge an alert", none, write},
		{"/api/notifications/*/read", "Mark a notification read", none, write},
		{"/api/cameras/*/ptz", "Pan, tilt and zoom a camera", none, write},
		{"/api/cameras/*/talk/offer", "Talk through a camera's speaker", none, write},

		// --- Admin only -------------------------------------------------------------------
		// Listed with no grants so the catalog is a COMPLETE description of the API surface.
		// The admin UI renders this list, so an area missing from it is an area nobody can see
		// they are not granting.
		{"/api/onvif", "Discover cameras on the network", none, none},
		{"/api/training", "Train custom detection models", none, none},
		{"/api/teach", "Teach a camera a new skill", none, none},
		{"/api/anomaly", "Configure the anomaly monitor", none, none},
		{"/api/pairing", "Pair this node with a control plane", none, none},
		{"/api/system", "Restart, update, factory reset, secure wipe", none, none},
		{"/api/settings/users", "Manage users and their roles", none, none},
		{"/api/settings/roles", "See the roles that can be assigned", none, none},
	}
}

// rolePermissions renders the catalog into the permission rows for one role.
//
// EVERY rule gets a row, including the ones the role is granted nothing on. That is not
// clutter — it is load-bearing.
//
// The matrix is decided by the MOST SPECIFIC matching row, and rows do not union. So an
// all-false row under a granted prefix is how a carve-out is expressed: "/api/settings" is
// readable, and "/api/settings/users" — deeper, therefore more specific — is not. Skipping
// the empty rows (as an earlier version of this did) meant the broader grant governed, and a
// viewer could enumerate every user account on the appliance. The test caught it; the
// reasoning that produced it was simply wrong.
//
// A row that is all-false and sits OUTSIDE any granted prefix ("/api/system") is redundant —
// deny-by-default already covers it — but writing it anyway keeps the matrix a complete,
// honest picture of the API surface, which is what the admin UI renders.
func rolePermissions(roleId int64, roleName string) []sharedentities.AccessRolePermission {
	rows := []sharedentities.AccessRolePermission{}
	for _, rule := range Policy() {
		var v verbs
		switch roleName {
		case RoleViewer:
			v = rule.Viewer
		case RoleOperator:
			v = rule.Operator
		default:
			continue
		}
		rows = append(rows, sharedentities.AccessRolePermission{
			RoleId:    roleId,
			Path:      rule.Path,
			CanGet:    v.Get,
			CanPost:   v.Post,
			CanPut:    v.Put,
			CanDelete: v.Delete,
		})
	}
	return rows
}

// EnsureRoles creates the built-in roles and seeds their permissions from the catalog.
//
// Defaults are applied ONLY to a role that has no permissions at all — so an operator who
// has been retuned by the site admin is never silently reset on the next boot. The
// consequence is that a role stripped to zero permissions gets its defaults back, which is
// the right outcome: a role with no permissions cannot sign anyone in to anything.
func EnsureRoles(
	ctx context.Context,
	roles sharedservices.IAccessRoleService,
	perms sharedservices.IAccessPermissionService,
) error {
	// Seeds superadmin + viewer.
	if err := roles.EnsureBuiltins(ctx); err != nil {
		return fmt.Errorf("seed builtin roles: %w", err)
	}

	// The operator role is mymatasan's own.
	operator, err := roles.GetByName(ctx, RoleOperator)
	if err != nil {
		return fmt.Errorf("look up operator role: %w", err)
	}
	if operator == nil {
		operator, err = roles.Create(ctx, RoleOperator, "Day-to-day security operator: watch, review, acknowledge, PTZ. Cannot delete footage or change settings.")
		if err != nil {
			return fmt.Errorf("create operator role: %w", err)
		}
	}

	viewer, err := roles.GetByName(ctx, RoleViewer)
	if err != nil {
		return fmt.Errorf("look up viewer role: %w", err)
	}

	for _, role := range []*sharedentities.AccessRole{viewer, operator} {
		if role == nil {
			continue
		}
		existing, err := perms.ListForRole(ctx, role.Id)
		if err != nil {
			return fmt.Errorf("list permissions for %s: %w", role.Name, err)
		}
		if len(existing) > 0 {
			continue // already configured — never clobber a site's tuning
		}
		for _, row := range rolePermissions(role.Id, role.Name) {
			if _, err := perms.Set(ctx, row); err != nil {
				return fmt.Errorf("seed permission %s for %s: %w", row.Path, role.Name, err)
			}
		}
	}
	return nil
}
