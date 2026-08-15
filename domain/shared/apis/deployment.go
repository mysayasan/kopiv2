package apis

import (
	"encoding/json"
	"net/http"
	"strings"

	sharedservices "github.com/mysayasan/kopiv2/domain/shared/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
)

// DeploymentHandlers serve the deployment-mode question and its readiness
// checklist. As with the setup and factory-reset handlers, route registration
// stays with each app — they differ in how an admin write is gated — but the
// payloads are identical so one shared SPA component works everywhere.
//
// The env is supplied as a FUNCTION rather than a captured value because two of
// its fields are only knowable per request: the ping callbacks have to run
// against the live cache and lock, and an app whose settings editor just changed
// the cache provider must not keep reporting the value it booted with.
type DeploymentHandlers struct {
	mode sharedservices.IDeploymentModeService
	env  func() sharedservices.DeploymentEnv
}

// NewDeploymentHandlers builds the handler set for the deployment surface. mode
// may be nil for an appliance app that has no runtime-setting repo wired: the
// declared mode is then always standalone, which is the truth for those anyway.
func NewDeploymentHandlers(mode sharedservices.IDeploymentModeService, env func() sharedservices.DeploymentEnv) *DeploymentHandlers {
	return &DeploymentHandlers{mode: mode, env: env}
}

// Preflight reports whether this app may be clustered and, if so, how ready this
// instance is. Readable by any signed-in user: it exposes provider names, a
// connection count and the at-rest KEY ID — an identifier for comparison, never
// the key itself — and the operator who most needs it is often looking before
// they have been granted anything else.
func (h *DeploymentHandlers) Preflight(w http.ResponseWriter, r *http.Request) {
	env := sharedservices.DeploymentEnv{}
	if h.env != nil {
		env = h.env()
	}
	controllers.SendResult(w, sharedservices.Preflight(r.Context(), env, h.state(r)), "succeed")
}

// Mode reports the declared deployment mode.
func (h *DeploymentHandlers) Mode(w http.ResponseWriter, r *http.Request) {
	controllers.SendResult(w, h.state(r), "succeed")
}

// SetMode records the declared deployment mode. This is a write, so callers must
// register it behind whatever admin gate the app uses.
//
// An appliance app refuses the write outright rather than storing an answer it
// would then ignore: a stored "clustered" on an NVR would be a claim the product
// cannot honour, and the next reader could not tell it from a supported one.
func (h *DeploymentHandlers) SetMode(w http.ResponseWriter, r *http.Request) {
	if h.env != nil {
		if env := h.env(); env.Appliance {
			controllers.SendError(w, controllers.ErrBadRequest, env.ApplianceReason)
			return
		}
	}
	if h.mode == nil {
		controllers.SendError(w, controllers.ErrBadRequest, "deployment mode is not configurable on this app")
		return
	}
	var body struct {
		Mode         string `json:"mode"`
		Acknowledged bool   `json:"acknowledged"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	state, err := h.mode.Set(r.Context(), strings.TrimSpace(body.Mode), body.Acknowledged)
	if err != nil {
		// The only failure the service raises for a bad value is an unknown mode,
		// which is the caller's fault rather than the server's.
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, state, "succeed")
}

// state reads the declared mode, falling back to standalone. A read failure is
// deliberately not surfaced as an error: standalone is both the safe default and
// the truth for every install that never answered, so a missing row must not stop
// the checklist rendering.
func (h *DeploymentHandlers) state(r *http.Request) sharedservices.DeploymentState {
	if h.mode == nil {
		return sharedservices.DeploymentState{Mode: sharedservices.ModeStandalone}
	}
	state, err := h.mode.Get(r.Context())
	if err != nil || state.Mode == "" {
		return sharedservices.DeploymentState{Mode: sharedservices.ModeStandalone}
	}
	return state
}
