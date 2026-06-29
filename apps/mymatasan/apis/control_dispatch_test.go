package apis

import (
	"context"
	"net/http"
	"testing"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/infra/control"
)

// buildTunnelRouter mirrors the real node wiring: an /api subrouter protected by
// NewLocalBasicAuth + NewRequireAdminForWrites, with a read and a write endpoint.
// userService/guard/notifier are nil because tunneled requests take the
// pre-injected-principal path and never touch them.
func buildTunnelRouter() http.Handler {
	root := mux.NewRouter()
	api := root.PathPrefix("/api").Subrouter()
	protected := api.PathPrefix("").Subrouter()
	protected.Use(NewLocalBasicAuth(nil, nil, nil))
	protected.Use(NewRequireAdminForWrites())
	protected.HandleFunc("/widgets", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("listed"))
	}).Methods("GET")
	protected.HandleFunc("/widgets", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("created"))
	}).Methods("POST")
	// Serve against the /api subrouter, matching how the node passes its api router.
	return api
}

func TestControlDispatcherRoleGating(t *testing.T) {
	disp := NewControlDispatcher(buildTunnelRouter())
	ctx := context.Background()

	// A viewer may read.
	if res := disp(ctx, control.Request{Method: "GET", Path: "/api/widgets", Role: "viewer"}); res.Status != http.StatusOK {
		t.Fatalf("viewer GET: got %d, want 200", res.Status)
	}
	// A viewer may NOT write (admin-for-writes gate).
	if res := disp(ctx, control.Request{Method: "POST", Path: "/api/widgets", Role: "viewer"}); res.Status == http.StatusCreated {
		t.Fatalf("viewer POST should be denied, got %d", res.Status)
	}
	// An admin may write.
	if res := disp(ctx, control.Request{Method: "POST", Path: "/api/widgets", Role: "admin"}); res.Status != http.StatusCreated {
		t.Fatalf("admin POST: got %d, want 201", res.Status)
	}
	// An admin may read.
	if res := disp(ctx, control.Request{Method: "GET", Path: "/api/widgets", Role: "admin"}); res.Status != http.StatusOK {
		t.Fatalf("admin GET: got %d, want 200", res.Status)
	}
}

func TestControlDispatcherUnknownPath(t *testing.T) {
	disp := NewControlDispatcher(buildTunnelRouter())
	res := disp(context.Background(), control.Request{Method: "GET", Path: "/api/nope", Role: "admin"})
	if res.Status != http.StatusNotFound {
		t.Fatalf("unknown path: got %d, want 404", res.Status)
	}
}
