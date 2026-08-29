package apis

import (
	"context"
	"net/http"

	"github.com/gorilla/mux"
	sharedapis "github.com/mysayasan/kopiv2/domain/shared/apis"
	sharedservices "github.com/mysayasan/kopiv2/domain/shared/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
)

// NewCapabilitiesApi answers "what may the signed-in role actually do?" — by asking the same
// permission matrix the authorization middleware asks.
//
// WHY THIS EXISTS. This app decided authorization twice. The server used the deny-by-default
// matrix in services.Policy(); the SPA hid Access rules and Settings on a client-side
// `user.isAdmin` and offered everything else to everybody. Two mechanisms with one intent, and
// nothing kept them in step — the exact root cause this codebase already recorded once as "nav
// uses isAdmin, server uses the matrix — two sources of truth".
//
// Drift between them is not cosmetic, and it goes wrong in both directions:
//
//   - the screen OFFERS what the server refuses. A viewer was shown an Unlock button on every
//     door card and an "Add person" button on the People screen. Pressing one produced a bare
//     error, from which nobody can tell "I am not allowed" from "this is broken" — while standing
//     at a door.
//   - the screen HIDES what the server allows. An operator is granted read on groups, grants and
//     schedules precisely so they can see the rules they must work within; the rail hid the whole
//     section because it was not an admin's.
//
// The fix is not a longer list of `isAdmin` checks in the frontend — that is a second copy of the
// policy, which is the defect. It is this: the client asks the matrix. Every flag below is
// computed by calling Authorize on the REAL route the screen would call, so a capability cannot
// say yes to something the middleware will refuse. Change the catalog and the screen follows.
type capabilitiesApi struct {
	perms sharedservices.IAccessPermissionService
}

// NewCapabilitiesApi registers GET /api/auth/capabilities.
//
// It must be registered BEFORE the shared /auth subrouter. That subrouter matches the /auth prefix
// and only serves the two routes it declares, so ordering is the simple way to keep this one from
// depending on fallthrough behaviour.
func NewCapabilitiesApi(router *mux.Router, perms sharedservices.IAccessPermissionService) {
	h := &capabilitiesApi{perms: perms}
	router.HandleFunc("/auth/capabilities", h.get).Methods("GET")
}

// capability is one thing a screen can offer, and the request it would really send.
//
// The probe path carries a placeholder id. The matrix matches segment-wise, so any id decides the
// same way — what matters is that the SHAPE is the shape the browser sends. A probe written
// "/api/doors/unlock" would have agreed with the catalog rule that had the same mistake in it and
// reported a capability nobody had.
type capability struct {
	name   string
	path   string
	method string
}

var capabilities = []capability{
	// Watching the estate.
	{"viewDoors", "/api/doors", "GET"},
	{"viewReaders", "/api/readers", "GET"},
	{"viewActivity", "/api/events", "GET"},
	{"viewPeople", "/api/holders", "GET"},
	// Running the building.
	{"unlockDoor", "/api/doors/0/unlock", "POST"},
	{"managePeople", "/api/holders", "POST"},
	{"issueBadges", "/api/holders/0/credentials", "POST"},
	// Changing the rules. viewRules is separate from editRules on purpose: an operator may read
	// the grants and schedules they work within and may not touch one, and a rail that collapses
	// the two hides a grant somebody was deliberately allowed to see.
	{"viewRules", "/api/grants", "GET"},
	{"editRules", "/api/grants", "POST"},
	// The building's safety posture, and the appliance itself.
	{"lockdown", "/api/lockdown", "POST"},
	{"viewSettings", "/api/settings/access", "GET"},
	{"editSettings", "/api/settings/access", "PUT"},
	{"manageUsers", "/api/settings/users", "POST"},
	{"createDoors", "/api/doors", "POST"},
}

func (a *capabilitiesApi) get(w http.ResponseWriter, r *http.Request) {
	user, ok := sharedapis.LocalUserFromContext(r.Context())
	if !ok {
		controllers.SendError(w, controllers.ErrLimitedAccess, "not authenticated")
		return
	}

	out := make(map[string]bool, len(capabilities))
	for _, c := range capabilities {
		out[c.name] = a.allows(r.Context(), user, c)
	}
	controllers.SendResult(w, out, "succeed")
}

// allows mirrors NewRequireRolePermission exactly: a superadmin bypasses the matrix, a user with
// no role has nothing, and everything else is the matrix's answer. A lookup that fails answers NO
// — a capability that cannot be established is not a capability, and the middleware would fail
// closed on the same request anyway, so saying yes here would only produce a button that 403s.
func (a *capabilitiesApi) allows(ctx context.Context, user *sharedservices.AuthenticatedUser, c capability) bool {
	if user.IsAdmin {
		return true
	}
	if user.RoleId <= 0 || a.perms == nil {
		return false
	}
	allowed, err := a.perms.Authorize(ctx, user.RoleId, c.path, c.method)
	return err == nil && allowed
}
