package apis

import (
	"bytes"
	"context"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/mysayasan/kopiv2/apps/mymatasan/services"
	sharedservices "github.com/mysayasan/kopiv2/domain/shared/services"
	"github.com/mysayasan/kopiv2/infra/control"
)

// NewControlDispatcher builds the node-side handler for tunneled parent→node commands. It
// re-injects the request into the node's OWN router (in-process, no network hop) with a
// synthetic principal, so the node's normal authorization decides it.
//
// The parent asserts a ROLE NAME. The node looks that name up in its OWN role table and
// evaluates its OWN permission matrix.
//
// That direction matters, and it is the reason this does not simply carry a permission set
// across the wire. If the parent asserted permissions, the node would be trusting the
// control plane to say who may delete its footage — and a compromised or buggy parent could
// assert anything. The node owns the data; the node's policy governs. All the parent gets to
// say is "this request is on behalf of an operator", and the node decides what an operator
// may do here.
//
// An unknown role name resolves to nothing, and a principal with no role is denied
// everything by the matrix. Fail closed.
//
// router is the app's /api subrouter; the parent sends paths including the /api prefix
// (e.g. /api/settings/users), which gorilla/mux matches against the full request path.
func NewControlDispatcher(router http.Handler, roles sharedservices.IAccessRoleService) services.ControlDispatcher {
	return func(ctx context.Context, req control.Request) control.Response {
		principal := &services.AuthenticatedUser{
			Username: controlActorUsername(req.Actor),
		}

		// Resolve the asserted role against the NODE's roles. "admin" is accepted as an alias
		// for the superadmin role: it is what every already-deployed control plane sends, and
		// the tunnel has to keep working across a mixed-version fleet.
		if name := normalizeControlRole(req.Role); name != "" {
			role, err := roles.GetByName(ctx, name)
			if err != nil {
				log.Printf("control dispatch: could not resolve role %q: %v", name, err)
				return control.Response{Status: http.StatusServiceUnavailable, Body: []byte("authorization is unavailable")}
			}
			if role != nil {
				principal.RoleId = role.Id
				principal.IsAdmin = role.IsSuperadmin
			}
		}

		method := strings.TrimSpace(req.Method)
		if method == "" {
			method = http.MethodGet
		}
		hr, err := http.NewRequestWithContext(
			context.WithValue(ctx, localAuthContextKey{}, principal),
			method, req.Path, bytes.NewReader(req.Body))
		if err != nil {
			return control.Response{Status: http.StatusBadRequest, Body: []byte(err.Error())}
		}
		for k, v := range req.Headers {
			hr.Header.Set(k, v)
		}

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, hr)

		// Forward all response headers so range/streaming metadata (Content-Range,
		// Accept-Ranges, Content-Length, Content-Type) reaches the control plane intact —
		// required for tunneled recording playback to seek.
		headers := map[string]string{}
		for k := range rec.Header() {
			if v := rec.Header().Get(k); v != "" {
				headers[k] = v
			}
		}
		return control.Response{Status: rec.Code, Headers: headers, Body: rec.Body.Bytes()}
	}
}

// controlRoleAdminAlias is what every already-deployed control plane sends for "full
// access". The node's superadmin role is named "superadmin", so the alias is mapped rather
// than the wire being broken.
const controlRoleAdminAlias = "admin"

// normalizeControlRole maps the wire vocabulary onto the node's own role names.
//
// The vocabulary widened from {admin, viewer} to {admin, operator, viewer} — an old control
// plane sends only the first two, and a new one may send "operator". A name the node does
// not know resolves to no role, and a principal with no role is denied everything.
func normalizeControlRole(role string) string {
	name := strings.ToLower(strings.TrimSpace(role))
	if name == controlRoleAdminAlias {
		return services.RoleAdmin
	}
	return name
}

// controlActorUsername labels the synthetic principal so node-side audit fields
// (createdBy/updatedBy style attribution) point back to the parent operator rather
// than to a real local account.
func controlActorUsername(actor string) string {
	actor = strings.TrimSpace(actor)
	if actor == "" {
		return "control-plane"
	}
	return "cp:" + actor
}
