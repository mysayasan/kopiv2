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
	read = sharedservices.VerbsRead
	// write grants POST **and nothing else**. It does NOT include GET — a rule whose only grant is
	// `write` gives a role the ability to create a thing it cannot list. That reading cost this app
	// its operator: `/api/holders` was `Operator: write`, so the receptionist could enrol a person
	// and then got 403 on the people list, which is the first thing the People screen loads.
	// `readWrite` is the grant that shape of rule almost always means.
	write     = sharedservices.VerbsWrite
	readWrite = sharedservices.Verbs{Get: true, Post: true}
	none      = sharedservices.VerbsNone
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
		//
		// THE SESSION PROBE IS THE FIRST CALL THE SPA MAKES, and it was not in this catalog. So a
		// viewer or an operator signed in successfully — /api/auth/login is public and answered 200
		// — and then GET /api/auth/session was refused by the matrix, `user` stayed null, and the
		// app rendered the sign-in card again. Correct credentials, no error message, straight back
		// to the login screen, forever. Every API this catalog carefully grants them was reachable
		// the whole time; there was simply no way to get past the front door of the UI.
		{Path: "/api/auth/session", Description: "Read your own session", Viewer: read, Operator: read},
		{Path: "/api/auth/change-password", Description: "Change your own password", Viewer: write, Operator: write},
		// What THIS role may do, answered by asking this very matrix. The screens gate their
		// controls on it instead of on a client-side `isAdmin`, so an offer on screen and a
		// decision on the server cannot drift apart. Readable by everyone signed in: a role that
		// cannot ask what it may do gets a UI that guesses.
		{Path: "/api/auth/capabilities", Description: "What the signed-in role may do", Viewer: read, Operator: read},

		// --- Watching the estate ----------------------------------------------------------
		{Path: "/api/doors", Description: "See doors and their live state", Viewer: read, Operator: read},
		{Path: "/api/readers", Description: "See readers, their health and Secure Channel state", Viewer: read, Operator: read},
		{Path: "/api/events", Description: "See the access log", Viewer: read, Operator: read},
		{Path: "/api/notifications", Description: "See the unified event feed", Viewer: read, Operator: read},
		{Path: "/api/notifications/*/read", Description: "Mark a feed entry read", Viewer: none, Operator: write},

		// --- Running the building ---------------------------------------------------------
		// Opening a door remotely is an operator power: it is what a receptionist does all day,
		// it is instantaneous, and every use of it lands in the same log as a badge.
		//
		// THE WILDCARD IS THE WHOLE RULE. The route is /api/doors/{id}/unlock, so "/api/doors/unlock"
		// — three segments against four — matched no request an operator could ever make. The most
		// specific rule that DID match was "/api/doors", which is read-only, so every remote open by
		// an operator was refused and the one power the role exists for did not exist. Nothing
		// caught it because this app's catalog had no test at all; see rbac_test.go.
		{Path: "/api/doors/*/unlock", Description: "Open a door remotely", Viewer: none, Operator: write},
		// Holders and their credentials are also operator-level: issuing and revoking a badge is
		// the routine, reversible half of access control. readWrite, not write — an operator who
		// cannot LIST people cannot find the person whose badge they are about to revoke.
		{Path: "/api/holders", Description: "Manage people and their badges", Viewer: read, Operator: readWrite},

		// --- Changing the rules -----------------------------------------------------------
		// Admin-only, and the reason is in the file header: editing a grant or a schedule changes
		// who may enter, everywhere in that group, silently and indefinitely.
		{Path: "/api/groups", Description: "Access groups and their members", Viewer: none, Operator: read},
		{Path: "/api/grants", Description: "Which groups reach which doors, on what schedule", Viewer: none, Operator: read},
		{Path: "/api/schedules", Description: "Time policies and the holiday calendar", Viewer: none, Operator: read},

		// --- The building's safety posture ------------------------------------------------
		// SEALING the site is admin-only. SEEING that it is sealed is not, and the two were one rule.
		//
		// The Doors screen loads the door list and the lockdown state together, so a refused GET
		// here did not merely hide a pill — it rejected the whole load and rendered "you do not
		// have permission for this action" where the doors should be. The home screen of a door
		// controller was an error page for every viewer and every operator on every install.
		// Doors.js no longer lets one refused read blank the screen, and this makes the read a
		// grant: an operator who cannot see that the site is sealed cannot explain why nothing is
		// opening, which is the exact minute they most need the screen.
		{Path: "/api/lockdown", Description: "See whether the site is sealed; sealing it is admin-only", Viewer: read, Operator: read},
		// Door hardware bindings decide which relay fires and which contact is believed. Wrong
		// values here do not produce a bad reading, they produce a door that opens for the wrong
		// person or an alarm that never comes.
		{Path: "/api/settings", Description: "Door hardware and system settings", Viewer: none, Operator: none},
		// The administrative trail: who changed the rules about who gets in. Admin-only, and
		// deliberately NOT granted to the operator who can read the rules themselves.
		//
		// The reasoning is the mirror image of the access log's. The access log is granted to
		// everybody signed in because a viewer's job is to watch what happened at the doors. This
		// table is about the people with power over the appliance — which accounts exist, who was
		// given a role, who was refused, which administrator reset whose password. Handing a
		// receptionist the record of every administrator's actions is a different disclosure from
		// handing them the door log, and it is not one this app's roles ask for.
		//
		// A superadmin bypasses the matrix, so "admin-only" here means: absent from the two rows
		// this catalog fills in.
		{Path: "/api/audit", Description: "The administrative trail: who changed the rules about who gets in", Viewer: none, Operator: none},
		// SEGMENT-WISE MATCHING MEANS THIS IS NOT COVERED BY THE RULE ABOVE. "/api/audit" governs
		// "/api/audit/anything", but "audit.csv" is a different first segment — so without this row
		// the export would be a route in no catalog rule, which is the one thing rbac.go's header
		// says must never happen. #224 found the same shape as a rule with the wrong segment count.
		{Path: "/api/audit.csv", Description: "Download the administrative trail as CSV", Viewer: none, Operator: none},
		// Minting an account is its own row, deeper than /api/settings and therefore more specific,
		// so it stays denied even if somebody widens the settings grant one day. It is the surface
		// that can hand out every other power on this list.
		{Path: "/api/settings/users", Description: "Users and the role each one holds", Viewer: none, Operator: none},
		{Path: "/api/settings/roles", Description: "The roles this appliance offers", Viewer: none, Operator: none},
		{Path: "/api/setup", Description: "First-run setup wizard", Viewer: none, Operator: none},
		// Joining or leaving a fleet changes who can manage this building's doors remotely.
		{Path: "/api/pairing", Description: "Fleet pairing with a control plane", Viewer: none, Operator: none},
		// Single-instance by design, so there is nothing here to grant — but a route absent from
		// this catalog is a route nobody can see they are not granting, which is the one thing the
		// catalog's own header says must never happen.
		{Path: "/api/deployment", Description: "Deployment mode (single-instance on this appliance)", Viewer: none, Operator: none},
	}
}

// EnsureRoles seeds the three builtin roles and their permission matrix.
func EnsureRoles(ctx context.Context, roles sharedservices.IAccessRoleService, perms sharedservices.IAccessPermissionService) error {
	return sharedservices.EnsureApplianceRoles(ctx, roles, perms, Policy(), operatorDescription)
}
