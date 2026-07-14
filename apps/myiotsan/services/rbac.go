package services

import (
	"context"

	sharedservices "github.com/mysayasan/kopiv2/domain/shared/services"
)

// myiotsan's role model.
//
// The three roles and the mechanics that seed them are shared with mymatasan
// (domain/shared/services.EnsureApplianceRoles). What lives HERE is the part that is
// genuinely myiotsan's: the catalog below, which says what a sensor hub's viewer and
// operator may actually reach.
//
//	viewer    see devices and their current readings, and see that an alert fired.
//	          No access to the historical record.
//	operator  + review telemetry history, acknowledge alerts.
//	          CANNOT actuate a device, delete readings, or change rules and settings.
//	admin     everything (a superadmin role — it bypasses the matrix).
//
// The line between viewer and operator is the same one mymatasan draws — CAN THIS PERSON
// DESTROY THE RECORD? — because a sensor hub is an evidentiary device too: "the door
// contact opened at 02:14" is a fact somebody may want to erase.
//
// myiotsan adds a second line that mymatasan does not have: ACTUATION. A camera is
// read-mostly, but an IoT device gets written to, and a bad relay write is physically
// dangerous in a way a bad PTZ move is not. So actuation is admin-only here, on top of the
// per-device capability toggle and the confirm-to-execute the plan calls for. When P4 lands
// the command path, this is the rule that must NOT be loosened without a deliberate decision.
const (
	// RoleAdmin is the shared superadmin builtin, shown as "Administrator" in the UI.
	RoleAdmin = sharedservices.RoleAdmin
	// RoleOperator is the day-to-day operator.
	RoleOperator = sharedservices.RoleOperator
	// RoleViewer is the shared viewer builtin: read-only.
	RoleViewer = sharedservices.RoleViewer
)

// PolicyRule is the shared catalog rule type, re-exported so the catalog below reads as
// myiotsan's own.
type PolicyRule = sharedservices.PolicyRule

var (
	read  = sharedservices.VerbsRead
	write = sharedservices.VerbsWrite
	none  = sharedservices.VerbsNone
)

// operatorDescription is what a human sees next to the operator role.
const operatorDescription = "Day-to-day operator: monitor devices, review telemetry, acknowledge alerts. Cannot actuate devices, delete readings, or change settings."

// Policy returns myiotsan's authorization catalog.
//
// admin is absent by design: it is a superadmin role and bypasses the matrix entirely, so
// it needs no rules. Anything NOT listed here is denied to viewer and operator — the matrix
// is deny-by-default.
//
// Every phase that adds an API area MUST add it here, INCLUDING the admin-only areas: a
// route missing from this catalog is a route nobody can see they are not granting.
func Policy() []PolicyRule {
	return []PolicyRule{
		// --- Everyone signed in ---------------------------------------------------------
		{Path: "/api/auth/change-password", Description: "Change your own password", Viewer: write, Operator: write},

		// --- Watching the estate ----------------------------------------------------------
		// A viewer sees the devices, their CURRENT readings, and that an alert fired. That is
		// the live picture, and it is all a viewer gets.
		{Path: "/api/devices", Description: "See devices and their current readings", Viewer: read, Operator: read},
		{Path: "/api/profiles", Description: "See device types and their datapoints", Viewer: read, Operator: read},
		{Path: "/api/rules", Description: "See the alert rules", Viewer: read, Operator: read},
		{Path: "/api/alerts", Description: "See the alert log", Viewer: read, Operator: read},
		{Path: "/api/notifications", Description: "See the notification feed", Viewer: read, Operator: read},

		// --- Reviewing the record (operator and up) ---------------------------------------
		// THIS is the viewer/operator line, and it is mymatasan's line exactly: a viewer sees
		// what is happening NOW; only an operator can go back through the history. A sensor hub
		// is an evidentiary device too — "the door contact opened at 02:14" is a fact somebody
		// may want to erase, and the history is where it lives.
		{Path: "/api/devices/*/readings", Description: "Review a device's telemetry history", Viewer: none, Operator: read},

		// --- Operating (operator only) ----------------------------------------------------
		{Path: "/api/alerts/*/ack", Description: "Acknowledge an alert", Viewer: none, Operator: write},
		{Path: "/api/notifications/*/read", Description: "Mark a notification read", Viewer: none, Operator: write},

		// --- Admin only -------------------------------------------------------------------
		// Listed with no grants so the catalog stays a COMPLETE description of the API surface
		// even where nothing below admin is granted.
		//
		// Note what is NOT here and therefore denied to everyone below admin: creating and
		// deleting devices, editing profiles (which would let somebody widen a deadband until a
		// sensor effectively stops recording), writing rules, and — when P4 lands it — ACTUATION.
		// A bad relay write is physically dangerous in a way a bad PTZ move is not.
		{Path: "/api/devices/*/password", Description: "Rotate a device's broker credential", Viewer: none, Operator: none},
		{Path: "/api/settings/users", Description: "Manage users and their roles", Viewer: none, Operator: none},
		{Path: "/api/settings/roles", Description: "See the roles that can be assigned", Viewer: none, Operator: none},
	}
}

// EnsureRoles creates the built-in roles and seeds them from myiotsan's catalog.
func EnsureRoles(
	ctx context.Context,
	roles sharedservices.IAccessRoleService,
	perms sharedservices.IAccessPermissionService,
) error {
	return sharedservices.EnsureApplianceRoles(ctx, roles, perms, Policy(), operatorDescription)
}
