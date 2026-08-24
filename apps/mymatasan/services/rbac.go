package services

import (
	"context"

	sharedservices "github.com/mysayasan/kopiv2/domain/shared/services"
)

// mymatasan's role model.
//
// The three roles and the mechanics that seed them are shared with every other appliance
// app (domain/shared/services.EnsureApplianceRoles). What lives HERE is the part that is
// genuinely mymatasan's: the catalog below, which says what a camera NVR's viewer and
// operator may actually reach.
//
//	viewer    watch live and see that an alert fired. No access to recorded footage.
//	operator  + review and download footage, acknowledge alerts, PTZ, talk-back.
//	          CANNOT delete or purge anything, cannot change AI rules or settings,
//	          cannot add or remove cameras.
//	admin     everything (a superadmin role — it bypasses the matrix).
//
// That middle role is the whole point. An operator who was present at an incident must not
// be able to delete the footage of it.
//
// Three and no more. Every extra role is a support burden and a matrix the customer will
// misconfigure. The matrix stays editable for the site that genuinely needs a fourth.
const (
	// RoleAdmin is the shared superadmin builtin, shown as "Administrator" in the UI.
	RoleAdmin = sharedservices.RoleAdmin
	// RoleOperator is the day-to-day security operator.
	RoleOperator = sharedservices.RoleOperator
	// RoleViewer is the shared viewer builtin: read-only.
	RoleViewer = sharedservices.RoleViewer
)

// PolicyRule is the shared catalog rule type, re-exported so the catalog below reads as
// mymatasan's own.
type PolicyRule = sharedservices.PolicyRule

var (
	read  = sharedservices.VerbsRead
	write = sharedservices.VerbsWrite
	none  = sharedservices.VerbsNone
	// readWrite is for an area whose write action is asynchronous or whose reads are part
	// of doing the write — an export you must poll, a case you must read back to annotate.
	// It grants no DELETE: destruction stays an administrator's verb.
	readWrite = sharedservices.Verbs{Get: true, Post: true}
)

// operatorDescription is what a human sees next to the operator role.
const operatorDescription = "Day-to-day security operator: watch, review, acknowledge, PTZ. Cannot delete footage or change settings."

// Policy returns mymatasan's authorization catalog.
//
// admin is absent by design: it is a superadmin role and bypasses the matrix entirely, so
// it needs no rules. Anything NOT listed here is denied to viewer and operator — the matrix
// is deny-by-default, which is the opposite of the old rule (every GET allowed to everybody).
//
// The ladder is drawn so the upgrade is not a regression. Today's non-admin can watch live,
// review footage, and acknowledge alerts, and is backfilled to OPERATOR, which keeps all of
// it. VIEWER is the genuinely stricter role the old model could not express: watch live and
// see that an alert happened, without access to the recorded footage.
func Policy() []PolicyRule {
	return []PolicyRule{
		// --- Everyone signed in ---------------------------------------------------------
		// The session probe is what the SPA calls FIRST, before it renders anything: it reads
		// who you are and whether you must change your password. Leaving it out of this catalog
		// meant deny-by-default refused it, and neither viewer nor operator could sign in AT
		// ALL — the sign-in form accepted the password and then showed "you do not have
		// permission for this action". Only superadmin worked, because superadmin bypasses the
		// matrix entirely and so never exercised this path.
		{Path: "/api/auth/session", Description: "Read your own session (who you are, must-change flag)", Viewer: read, Operator: read},
		{Path: "/api/auth/change-password", Description: "Change your own password", Viewer: write, Operator: write},

		// --- Watching live ----------------------------------------------------------------
		{Path: "/api/cameras", Description: "See the camera list and camera details", Viewer: read, Operator: read},
		{Path: "/api/cameras/health/refresh", Description: "Re-probe camera reachability (the sign-in health check)", Viewer: write, Operator: write},
		{Path: "/api/cameras/*/live-view", Description: "Open a live view", Viewer: write, Operator: write},
		{Path: "/api/cameras/*/webrtc/offer", Description: "Negotiate the live video stream", Viewer: write, Operator: write},

		// --- Seeing that something happened -----------------------------------------------
		{Path: "/api/vision", Description: "See AI detection rules and the alert log", Viewer: read, Operator: read},
		{Path: "/api/notifications", Description: "See the notification feed", Viewer: read, Operator: read},
		{Path: "/api/capacity", Description: "See how much the host can handle", Viewer: read, Operator: read},
		// Settings is readable so the app can render — decoder settings drive live view. The
		// user-management routes beneath it keep their own admin self-gate, so a viewer who
		// can read /api/settings still cannot read /api/settings/users.
		{Path: "/api/settings", Description: "Read runtime settings", Viewer: read, Operator: read},
		{Path: "/api/setup", Description: "Read first-run setup state", Viewer: read, Operator: read},

		// --- Reviewing recorded footage (operator and up) ----------------------------------
		// THIS is the line viewer/operator draws: a viewer watches live and sees that an alert
		// fired; only an operator can go back and watch what actually happened.
		//
		// Read only. Every mutating verb beneath these is denied because it is never granted:
		// deleting a segment, purging a camera's footage. THAT denial is the evidentiary line
		// — an operator who was present at an incident cannot delete the footage of it.
		{Path: "/api/recording", Description: "Play back and download recorded footage", Viewer: none, Operator: read},
		// Exporting is an OPERATOR capability and deleting is not, which is the same line
		// drawn twice: an operator who was present at an incident must be able to hand the
		// footage of it to somebody, and must not be able to destroy it. Every export is
		// audited with the operator's stated reason.
		//
		// GET as well as POST, and the GET is not a widening: building a bundle is
		// asynchronous, so an exporter must poll the job and then download the result.
		// Granted POST alone, an operator could START an export and was then refused both
		// the status and the file — the capability the role model says it has, unusable.
		// Found while tracing this path for the case bundle, not by a test.
		{Path: "/api/evidence", Description: "Export footage as a verifiable evidence bundle", Viewer: none, Operator: readWrite},
		{Path: "/api/observations", Description: "Search what objects a camera saw", Viewer: none, Operator: read},
		// Case files: the investigation container. An operator opens cases, adds evidence,
		// annotates and closes them, and exports the bundle — that is the role's job.
		// DELETE is absent from readWrite, so removing a case entirely stays with an
		// administrator: a case is the record that an investigation happened, and the
		// person who was present at the incident must not be able to erase it.
		{Path: "/api/cases", Description: "Open, annotate and export case files", Viewer: none, Operator: readWrite},
		// Video walls. A viewer READS them — the wall is what they are here to watch, and a
		// viewer who cannot load it sees a blank grid. Arranging them is an operator's job.
		// No level grants DELETE; removing a wall is a POST, see apis/walls.go.
		{Path: "/api/walls", Description: "Named video walls: which cameras, in what grid", Viewer: read, Operator: readWrite},

		// --- Operating (operator only) ----------------------------------------------------
		{Path: "/api/vision/alerts/*/ack", Description: "Acknowledge an alert", Viewer: none, Operator: write},
		{Path: "/api/notifications/*/read", Description: "Mark a notification read", Viewer: none, Operator: write},
		{Path: "/api/cameras/*/ptz", Description: "Pan, tilt and zoom a camera", Viewer: none, Operator: write},
		{Path: "/api/cameras/*/talk/offer", Description: "Talk through a camera's speaker", Viewer: none, Operator: write},

		// --- Admin only -------------------------------------------------------------------
		// Listed with no grants so the catalog is a COMPLETE description of the API surface.
		// The admin UI renders this list, so an area missing from it is an area nobody can see
		// they are not granting.
		{Path: "/api/onvif", Description: "Discover cameras on the network", Viewer: none, Operator: none},
		// Face recognition was governed by nothing at all: absent from this catalog, it was
		// denied by default (correct) but invisible in the admin UI that renders this list — an
		// area nobody could see they were not granting, which is the exact failure this
		// catalog's completeness rule exists to prevent. Found by the page-catalog test that
		// asserts every page grant names a path Policy() governs.
		{Path: "/api/faces", Description: "Enroll people for face recognition (biometric data)", Viewer: none, Operator: none},
		{Path: "/api/training", Description: "Train custom detection models", Viewer: none, Operator: none},
		{Path: "/api/teach", Description: "Teach a camera a new skill", Viewer: none, Operator: none},
		{Path: "/api/anomaly", Description: "Configure the anomaly monitor", Viewer: none, Operator: none},
		{Path: "/api/pairing", Description: "Pair this node with a control plane", Viewer: none, Operator: none},
		{Path: "/api/system", Description: "Restart, update, factory reset, secure wipe", Viewer: none, Operator: none},
		{Path: "/api/settings/users", Description: "Manage users and their roles", Viewer: none, Operator: none},
		{Path: "/api/settings/roles", Description: "See the roles that can be assigned", Viewer: none, Operator: none},
		// The audit trail names who watched, downloaded and deleted which footage, so it is
		// itself sensitive: it is a record of people's viewing, not just of configuration.
		// Admin-only, and read-only for everyone — there is no delete route at all, because a
		// trail the recorded party can trim is not a trail.
		{Path: "/api/audit", Description: "Review the audit trail (who viewed, downloaded or deleted footage)", Viewer: none, Operator: none},
	}
}

// EnsureRoles creates the built-in roles and seeds them from mymatasan's catalog.
func EnsureRoles(
	ctx context.Context,
	roles sharedservices.IAccessRoleService,
	perms sharedservices.IAccessPermissionService,
) error {
	return sharedservices.EnsureApplianceRoles(ctx, roles, perms, Policy(), operatorDescription)
}
