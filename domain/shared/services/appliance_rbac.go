package services

import (
	"context"
	"fmt"

	"github.com/mysayasan/kopiv2/domain/entities"
)

// The appliance role model, shared by every appliance app (mymatasan, myiotsan).
//
// Three roles, and the line between them is: CAN THIS PERSON DESTROY EVIDENCE?
//
//	viewer    watch the live signal and see that an alert fired. No access to the record.
//	operator  + review the record, acknowledge alerts, operate the device.
//	          CANNOT delete or purge anything, cannot change rules or settings.
//	admin     everything (a superadmin role — it bypasses the matrix).
//
// The middle role is the point. An operator who was present at an incident must not be
// able to delete the record of it. What each role may actually reach is the app's own
// business — mymatasan governs cameras and recordings, myiotsan governs sensors and
// telemetry — so the CATALOG is per-app (see PolicyRule) while the mechanics that turn a
// catalog into permission rows live here, once.
const (
	// RoleAdmin is the shared superadmin builtin. It is named "superadmin" in the database
	// and usually shown as "Administrator" in the UI.
	RoleAdmin = RoleSuperadmin
	// RoleOperator is the appliance's day-to-day operator. Unlike superadmin and viewer it
	// is NOT a shared builtin — the shared stack does not seed it, EnsureApplianceRoles does.
	RoleOperator = "operator"
)

// Verbs is the four-verb grant on one path.
type Verbs struct {
	Get    bool
	Post   bool
	Put    bool
	Delete bool
}

// The three grants a catalog rule needs in practice. A rule that needs something else
// (say, delete-but-not-post) can still write a Verbs literal.
var (
	// VerbsRead grants GET.
	VerbsRead = Verbs{Get: true}
	// VerbsWrite grants POST.
	VerbsWrite = Verbs{Post: true}
	// VerbsNone grants nothing. It is NOT the same as omitting the rule — see RolePermissions.
	VerbsNone = Verbs{}
)

// PolicyRule is one governed area of an app's API and what each built-in role may do in it.
//
// An app's policy is meant to be Go data rather than rows an installer clicks into a grid:
// a policy you can review in a diff is a policy somebody actually reviews.
//
// Paths are matched segment-wise, and "*" matches one segment — which is what lets an
// action be granted without granting its whole collection. "/api/cameras/*/ptz" says an
// operator may move a camera; it does not say they may create one.
//
// The MOST SPECIFIC matching rule decides (rules do not union), so a broad grant can be
// carved out by a narrower one.
type PolicyRule struct {
	Path        string
	Description string
	Viewer      Verbs
	Operator    Verbs
}

// RolePermissions renders a policy catalog into the permission rows for one role.
//
// EVERY rule gets a row, including the ones the role is granted nothing on. That is not
// clutter — it is load-bearing.
//
// The matrix is decided by the MOST SPECIFIC matching row, and rows do not union. So an
// all-false row under a granted prefix is how a carve-out is expressed: "/api/settings" is
// readable, and "/api/settings/users" — deeper, therefore more specific — is not. Skipping
// the empty rows means the broader grant governs, and a viewer can enumerate every user
// account on the appliance. (That was a real bug, caught by a live test, not a hypothetical.)
//
// A row that is all-false and sits OUTSIDE any granted prefix is redundant — deny-by-default
// already covers it — but writing it anyway keeps the matrix a complete, honest picture of
// the API surface, which is what the admin UI renders.
//
// A role name that is neither viewer nor operator gets no rows: admin is a superadmin and
// bypasses the matrix entirely, so it needs none.
func RolePermissions(roleId int64, roleName string, policy []PolicyRule) []entities.AccessRolePermission {
	rows := []entities.AccessRolePermission{}
	for _, rule := range policy {
		var v Verbs
		switch roleName {
		case RoleViewer:
			v = rule.Viewer
		case RoleOperator:
			v = rule.Operator
		default:
			continue
		}
		rows = append(rows, entities.AccessRolePermission{
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

// EnsureApplianceRoles creates the built-in roles and seeds their permissions from the
// app's catalog. operatorDescription describes the app's operator role to a human.
//
// A role with NO permissions is seeded in full — so a role stripped to zero gets its defaults
// back, which is the right outcome: a role with no permissions cannot sign anyone in to
// anything.
//
// A role that already has permissions is BACKFILLED, not reset: rows are added only for
// catalog paths the role has no row for at all, and an existing row's verbs are never
// touched. That distinction is the whole point — a site that has retuned its operator keeps
// that tuning across upgrades, while a rule ADDED to the catalog still reaches the install.
//
// Without the backfill, a new catalog entry only ever reached brand-new installs. That is not
// a hypothetical: the appliance shipped without a rule for the SPA's session probe, and the
// fix would have been invisible to every existing install — the roles already had rows, so
// the corrected catalog would have been skipped and viewers would have stayed locked out.
//
// Adding a row can only ever match the catalog's stated intent, including the deliberately
// all-false rows that carve a narrower denial out of a broader grant (settings readable,
// settings/users not). It cannot revoke a verb a site granted, because it never edits a row
// that exists.
func EnsureApplianceRoles(
	ctx context.Context,
	roles IAccessRoleService,
	perms IAccessPermissionService,
	policy []PolicyRule,
	operatorDescription string,
) error {
	// Seeds superadmin + viewer.
	if err := roles.EnsureBuiltins(ctx); err != nil {
		return fmt.Errorf("seed builtin roles: %w", err)
	}

	// The operator role is the appliance's own — the shared stack does not seed it.
	operator, err := roles.GetByName(ctx, RoleOperator)
	if err != nil {
		return fmt.Errorf("look up operator role: %w", err)
	}
	if operator == nil {
		operator, err = roles.Create(ctx, RoleOperator, operatorDescription)
		if err != nil {
			return fmt.Errorf("create operator role: %w", err)
		}
	}

	viewer, err := roles.GetByName(ctx, RoleViewer)
	if err != nil {
		return fmt.Errorf("look up viewer role: %w", err)
	}

	for _, role := range []*entities.AccessRole{viewer, operator} {
		if role == nil {
			continue
		}
		existing, err := perms.ListForRole(ctx, role.Id)
		if err != nil {
			return fmt.Errorf("list permissions for %s: %w", role.Name, err)
		}
		havePath := make(map[string]bool, len(existing))
		for _, row := range existing {
			if row != nil {
				havePath[row.Path] = true
			}
		}
		for _, row := range RolePermissions(role.Id, role.Name, policy) {
			// Configured role: add only what is missing, and never touch what is there.
			if len(existing) > 0 && havePath[row.Path] {
				continue
			}
			if _, err := perms.Set(ctx, row); err != nil {
				return fmt.Errorf("seed permission %s for %s: %w", row.Path, role.Name, err)
			}
		}
	}
	return nil
}
