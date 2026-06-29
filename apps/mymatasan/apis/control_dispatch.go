package apis

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/mysayasan/kopiv2/apps/mymatasan/services"
	"github.com/mysayasan/kopiv2/infra/control"
)

// NewControlDispatcher builds the node-side handler for tunneled parent→node
// commands. It re-injects the request into the node's own router (in-process, no
// network hop) with a synthetic authenticated principal derived from the asserted
// role, so the node's existing authorization — NewLocalBasicAuth honors the
// pre-injected principal, and NewRequireAdminForWrites gates writes by IsAdmin —
// enforces viewer/admin without any new auth code.
//
// router is the app's /api subrouter; the parent sends paths including the /api
// prefix (e.g. /api/settings/users), which gorilla/mux matches against the full
// request path.
func NewControlDispatcher(router http.Handler) services.ControlDispatcher {
	return func(ctx context.Context, req control.Request) control.Response {
		principal := &services.AuthenticatedUser{
			Username: controlActorUsername(req.Actor),
			IsAdmin:  strings.EqualFold(strings.TrimSpace(req.Role), roleAdmin),
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

		headers := map[string]string{}
		if ct := rec.Header().Get("Content-Type"); ct != "" {
			headers["Content-Type"] = ct
		}
		return control.Response{Status: rec.Code, Headers: headers, Body: rec.Body.Bytes()}
	}
}

const roleAdmin = "admin"

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
