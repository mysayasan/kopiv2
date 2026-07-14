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
// This is the P0 surface, and it is deliberately small: P0 has no device, telemetry or
// notification API yet, so none is listed. A catalog that names routes the app does not
// serve is a lie an operator would rely on.
//
// Every phase that adds an API area MUST add it here, INCLUDING the admin-only areas: a
// route missing from this catalog is a route nobody can see they are not granting.
func Policy() []PolicyRule {
	return []PolicyRule{
		// --- Everyone signed in ---------------------------------------------------------
		{Path: "/api/auth/change-password", Description: "Change your own password", Viewer: write, Operator: write},

		// --- Admin only -------------------------------------------------------------------
		// Listed with no grants so the catalog stays a COMPLETE description of the API
		// surface even where nothing below admin is granted.
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
