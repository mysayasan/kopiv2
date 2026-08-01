package apis

import (
	"net/http"

	sharedservices "github.com/mysayasan/kopiv2/domain/shared/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
)

// SetupHandlers serves the first-run setup wizard's completion flag. The
// handlers are shared across the suite; route registration is deliberately left
// to each app because they differ in how reads and writes are gated (a global
// require-admin-for-writes wrapper in mymatasan, explicit auth + RBAC-matrix
// middleware in myidsan and myseliasan).
type SetupHandlers struct {
	setup sharedservices.ISetupStateService
}

// NewSetupHandlers builds the handler pair for GET state / POST complete.
func NewSetupHandlers(setup sharedservices.ISetupStateService) *SetupHandlers {
	return &SetupHandlers{setup: setup}
}

// State reports whether first-run setup has been completed. Readable by any
// signed-in user — the SPA checks it immediately after login.
func (h *SetupHandlers) State(w http.ResponseWriter, r *http.Request) {
	state, err := h.setup.Get(r.Context())
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, state, "succeed")
}

// Complete records setup as done. This is a write, so callers must register it
// behind whatever admin gate the app uses.
func (h *SetupHandlers) Complete(w http.ResponseWriter, r *http.Request) {
	state, err := h.setup.Complete(r.Context())
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, state, "succeed")
}
