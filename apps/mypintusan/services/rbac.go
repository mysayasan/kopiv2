package services

import (
	"context"

	sharedservices "github.com/mysayasan/kopiv2/domain/shared/services"
)

// mypintusan's role model.
//
// The three roles and the mechanics that seed them are shared with the other appliances
// (domain/shared/services.EnsureApplianceRoles). What lives here is the catalog: what a door
// system's viewer and operator may actually reach.
//
//	viewer    see doors, readers and their live state, and see the access log.
//	          Cannot open anything, cannot change who may enter.
//	operator  + open a door remotely, enrol and revoke credentials, manage holders.
//	          CANNOT change grants, schedules, door hardware bindings, or lockdown.
//	admin     everything (a superadmin role — it bypasses the matrix).
//
// The line the other apps draw is "can this person destroy the record?". This app keeps that,
// because the access log is evidentiary — "the fire door opened at 02:14 and nobody badged" is a
// fact somebody may want gone — and adds a second, sharper one: WHO MAY CHANGE THE RULES ABOUT WHO
// GETS IN.
//
// Those are deliberately different powers. Handing someone a temporary badge is a daily,
// reversible, fully logged act, and a receptionist needs it. Editing a GRANT silently changes who
// may enter every door in that group, at every hour, until somebody notices — and nothing about it
// looks unusual in a log full of ordinary badge events. So credentials are operator-level and
// grants are admin-only, even though the UI puts them two clicks apart.
//
// LOCKDOWN is admin-only for the opposite reason: it is the one control that stops a building
// working. It cannot trap anyone — egress is hardware — but an operator who triggers it during a
// fire drill has still made an incident out of a nuisance.
const (
	// RoleAdmin is the shared superadmin builtin, shown as "Administrator" in the UI.
	RoleAdmin = sharedservices.RoleAdmin
	// RoleOperator is the day-to-day operator: a receptionist, a security desk.
	RoleOperator = sharedservices.RoleOperator
	// RoleViewer is the shared viewer builtin: read-only.
	RoleViewer = sharedservices.RoleViewer
)

// PolicyRule is the shared catalog rule type, re-exported so the catalog reads as this app's own.
type PolicyRule = sharedservices.PolicyRule

var (
	read  = sharedservices.VerbsRead
	write = sharedservices.VerbsWrite
	none  = sharedservices.VerbsNone
)

// operatorDescription is what a human sees next to the operator role.
const operatorDescription = "Day-to-day operator: watch doors, open one remotely, issue and revoke badges, manage holders. Cannot change access rules, schedules, door hardware or lockdown."

// Policy returns mypintusan's authorization catalog.
//
// admin is absent by design: it is a superadmin role and bypasses the matrix entirely. Anything NOT
// listed here is denied to viewer and operator — the matrix is deny-by-default.
//
// Every area that grows an API MUST be added here, INCLUDING the admin-only ones: a route missing
// from this catalog is a route nobody can see they are not granting.
func Policy() []PolicyRule {
	return []PolicyRule{
		// --- Everyone signed in -----------------------------------------------------------
		{Path: "/api/auth/change-password", Description: "Change your own password", Viewer: write, Operator: write},

		// --- Watching the estate ----------------------------------------------------------
		{Path: "/api/doors", Description: "See doors and their live state", Viewer: read, Operator: read},
		{Path: "/api/readers", Description: "See readers, their health and Secure Channel state", Viewer: read, Operator: read},
		{Path: "/api/events", Description: "See the access log", Viewer: read, Operator: read},
		{Path: "/api/notifications", Description: "See the unified event feed", Viewer: read, Operator: read},
		{Path: "/api/notifications/*/read", Description: "Mark a feed entry read", Viewer: none, Operator: write},

		// --- Running the building ---------------------------------------------------------
		// Opening a door remotely is an operator power: it is what a receptionist does all day,
		// it is instantaneous, and every use of it lands in the same log as a badge.
		{Path: "/api/doors/unlock", Description: "Open a door remotely", Viewer: none, Operator: write},
		// Holders and their credentials are also operator-level: issuing and revoking a badge is
		// the routine, reversible half of access control.
		{Path: "/api/holders", Description: "Manage people and their badges", Viewer: read, Operator: write},

		// --- Changing the rules -----------------------------------------------------------
		// Admin-only, and the reason is in the file header: editing a grant or a schedule changes
		// who may enter, everywhere in that group, silently and indefinitely.
		{Path: "/api/groups", Description: "Access groups and their members", Viewer: none, Operator: read},
		{Path: "/api/grants", Description: "Which groups reach which doors, on what schedule", Viewer: none, Operator: read},
		{Path: "/api/schedules", Description: "Time policies and the holiday calendar", Viewer: none, Operator: read},

		// --- The building's safety posture ------------------------------------------------
		{Path: "/api/lockdown", Description: "Seal the site", Viewer: none, Operator: none},
		// Door hardware bindings decide which relay fires and which contact is believed. Wrong
		// values here do not produce a bad reading, they produce a door that opens for the wrong
		// person or an alarm that never comes.
		{Path: "/api/settings", Description: "Users, roles, door hardware and system settings", Viewer: none, Operator: none},
		{Path: "/api/setup", Description: "First-run setup wizard", Viewer: none, Operator: none},
		// Joining or leaving a fleet changes who can manage this building's doors remotely.
		{Path: "/api/pairing", Description: "Fleet pairing with a control plane", Viewer: none, Operator: none},
	}
}

// EnsureRoles seeds the three builtin roles and their permission matrix.
func EnsureRoles(ctx context.Context, roles sharedservices.IAccessRoleService, perms sharedservices.IAccessPermissionService) error {
	return sharedservices.EnsureApplianceRoles(ctx, roles, perms, Policy(), operatorDescription)
}
