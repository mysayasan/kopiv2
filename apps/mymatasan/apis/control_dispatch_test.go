package apis

import (
	"context"
	"net/http"
	"testing"

	"github.com/gorilla/mux"
	sharedentities "github.com/mysayasan/kopiv2/domain/entities"
	sharedservices "github.com/mysayasan/kopiv2/domain/shared/services"
	"github.com/mysayasan/kopiv2/infra/control"
)

// tunnelRoles is the NODE's role table. The point of the test is that the parent asserts a
// role NAME and the node resolves it here, against its own roles, and then evaluates its own
// matrix. The parent never gets to say what a role may do.
type tunnelRoles struct {
	sharedservices.IAccessRoleService
}

func (tunnelRoles) GetByName(_ context.Context, name string) (*sharedentities.AccessRole, error) {
	switch name {
	case "superadmin":
		return &sharedentities.AccessRole{Id: 1, Name: "superadmin", IsSuperadmin: true}, nil
	case "operator":
		return &sharedentities.AccessRole{Id: 2, Name: "operator"}, nil
	case "viewer":
		return &sharedentities.AccessRole{Id: 3, Name: "viewer"}, nil
	}
	return nil, nil // an unknown role resolves to nothing, and nothing is denied everything
}

// tunnelPerms is the NODE's matrix: reads are allowed, writes are not — except for the
// superadmin, which never reaches here.
type tunnelPerms struct {
	sharedservices.IAccessPermissionService
}

func (tunnelPerms) Authorize(_ context.Context, roleId int64, _ string, method string) (bool, error) {
	if roleId <= 0 {
		return false, nil
	}
	return method == http.MethodGet, nil
}

// buildTunnelRouter mirrors the real node wiring: an /api subrouter protected by
// NewLocalBasicAuth + NewRequireRolePermission, with a read and a write endpoint.
// userService/guard/notifier are nil because a tunneled request takes the
// pre-injected-principal path and never touches them.
func buildTunnelRouter() http.Handler {
	root := mux.NewRouter()
	api := root.PathPrefix("/api").Subrouter()
	protected := api.PathPrefix("").Subrouter()
	protected.Use(NewLocalBasicAuth(nil, nil, nil))
	protected.Use(NewRequireRolePermission(tunnelRoles{}, tunnelPerms{}))
	protected.HandleFunc("/widgets", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("listed"))
	}).Methods("GET")
	protected.HandleFunc("/widgets", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("created"))
	}).Methods("POST")
	return api
}

func newTunnelDispatcher() func(context.Context, control.Request) control.Response {
	return NewControlDispatcher(buildTunnelRouter(), tunnelRoles{})
}

// The parent asserts a ROLE NAME; the NODE decides what that role may do.
//
// This is the security posture of the whole fleet. If the parent asserted a permission SET,
// the node would be trusting the control plane to say who may delete its footage — and a
// compromised parent could assert anything. All the parent gets to say is "on behalf of an
// operator".
func TestControlDispatcher_NodeEvaluatesItsOwnMatrix(t *testing.T) {
	disp := newTunnelDispatcher()
	ctx := context.Background()

	// A viewer may read.
	if res := disp(ctx, control.Request{Method: "GET", Path: "/api/widgets", Role: "viewer"}); res.Status != http.StatusOK {
		t.Fatalf("viewer GET: got %d, want 200", res.Status)
	}
	// A viewer may not write — the NODE's matrix said so, not the parent.
	if res := disp(ctx, control.Request{Method: "POST", Path: "/api/widgets", Role: "viewer"}); res.Status == http.StatusCreated {
		t.Fatalf("viewer POST should be denied, got %d", res.Status)
	}
	// "admin" is the legacy wire word for full access and must keep working — an already
	// deployed control plane sends it, and a mixed-version fleet has to keep functioning.
	if res := disp(ctx, control.Request{Method: "POST", Path: "/api/widgets", Role: "admin"}); res.Status != http.StatusCreated {
		t.Fatalf("legacy admin POST: got %d, want 201", res.Status)
	}
	if res := disp(ctx, control.Request{Method: "GET", Path: "/api/widgets", Role: "admin"}); res.Status != http.StatusOK {
		t.Fatalf("legacy admin GET: got %d, want 200", res.Status)
	}
}

// The vocabulary widened from {admin, viewer} to {admin, operator, viewer}. A NEW control
// plane can send "operator", and the node evaluates it against its own matrix.
func TestControlDispatcher_OperatorIsUnderstood(t *testing.T) {
	disp := newTunnelDispatcher()
	ctx := context.Background()

	if res := disp(ctx, control.Request{Method: "GET", Path: "/api/widgets", Role: "operator"}); res.Status != http.StatusOK {
		t.Fatalf("operator GET: got %d, want 200", res.Status)
	}
	if res := disp(ctx, control.Request{Method: "POST", Path: "/api/widgets", Role: "operator"}); res.Status == http.StatusCreated {
		t.Fatalf("operator POST should be denied by the node's matrix, got %d", res.Status)
	}
}

// A role the node does not know — a newer control plane inventing one, or a malformed
// request — resolves to no role, and a principal with no role is denied everything.
// FAIL CLOSED: an unrecognised assertion must never widen access.
func TestControlDispatcher_UnknownRoleFailsClosed(t *testing.T) {
	disp := newTunnelDispatcher()
	ctx := context.Background()

	for _, role := range []string{"", "root", "superuser", "OPERATOR-X"} {
		res := disp(ctx, control.Request{Method: "GET", Path: "/api/widgets", Role: role})
		if res.Status == http.StatusOK {
			t.Fatalf("unknown role %q was granted access (status %d) — it must fail closed", role, res.Status)
		}
	}
}

func TestControlDispatcherUnknownPath(t *testing.T) {
	disp := newTunnelDispatcher()
	res := disp(context.Background(), control.Request{Method: "GET", Path: "/api/nope", Role: "admin"})
	if res.Status != http.StatusNotFound {
		t.Fatalf("unknown path: got %d, want 404", res.Status)
	}
}
