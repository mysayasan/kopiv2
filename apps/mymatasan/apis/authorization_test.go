package apis

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mysayasan/kopiv2/apps/mymatasan/services"
)

func withUser(r *http.Request, user *services.AuthenticatedUser) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), localAuthContextKey{}, user))
}

func TestRequireAdminForWrites(t *testing.T) {
	handler := NewRequireAdminForWrites()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	admin := &services.AuthenticatedUser{Id: 1, Username: "admin", IsAdmin: true}
	viewer := &services.AuthenticatedUser{Id: 2, Username: "viewer", IsAdmin: false}

	cases := []struct {
		name   string
		user   *services.AuthenticatedUser
		method string
		path   string
		want   int
	}{
		{"admin write allowed", admin, http.MethodPut, "/api/settings/runtime", http.StatusNoContent},
		{"viewer read allowed", viewer, http.MethodGet, "/api/settings/runtime", http.StatusNoContent},
		{"viewer write blocked", viewer, http.MethodPut, "/api/settings/runtime", http.StatusForbidden},
		{"viewer delete blocked", viewer, http.MethodDelete, "/api/cameras/3", http.StatusForbidden},
		{"viewer ack allowed", viewer, http.MethodPost, "/api/vision/alerts/9/ack", http.StatusNoContent},
		{"viewer mark-read allowed", viewer, http.MethodPost, "/api/notifications/9/read", http.StatusNoContent},
		{"viewer change-password allowed", viewer, http.MethodPost, "/api/auth/change-password", http.StatusNoContent},
		{"viewer live-view allowed", viewer, http.MethodPost, "/api/cameras/3/live-view", http.StatusNoContent},
		{"viewer create-rule blocked", viewer, http.MethodPost, "/api/vision/rules", http.StatusForbidden},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := withUser(httptest.NewRequest(tc.method, "http://example.com"+tc.path, nil), tc.user)
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)
			if rr.Code != tc.want {
				t.Fatalf("%s %s as admin=%v: status = %d, want %d", tc.method, tc.path, tc.user.IsAdmin, rr.Code, tc.want)
			}
		})
	}
}

func TestRequireAdminForWritesFailsClosedWithoutUser(t *testing.T) {
	handler := NewRequireAdminForWrites()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodPost, "http://example.com/api/settings/runtime", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("missing user status = %d, want 403", rr.Code)
	}
}
