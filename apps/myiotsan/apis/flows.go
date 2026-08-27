package apis

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/myiotsan/entities"
	"github.com/mysayasan/kopiv2/apps/myiotsan/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
)

type flowsApi struct {
	flows   *services.FlowService
	runtime *services.FlowRuntime
}

// NewFlowsApi registers the flow engine's endpoints.
//
// Reading and authoring a flow are ordinary CRUD; TEST-RUNNING one exercises the runtime (and, via
// an output node, can actuate through the guarded command path), so the whole area is admin-only —
// the RBAC matrix (services.Policy) denies /api/flows to everyone below admin, and the settings-side
// requireAdmin gate is defence in depth. A flow's debug snapshot is the live inspector's data.
func NewFlowsApi(router *mux.Router, flows *services.FlowService, runtime *services.FlowRuntime) {
	h := &flowsApi{flows: flows, runtime: runtime}

	g := router.PathPrefix("/flows").Subrouter()
	g.HandleFunc("", h.list).Methods("GET")
	g.HandleFunc("", h.create).Methods("POST")
	// Literal paths before /{id} so they win the match.
	g.HandleFunc("/import", h.importFlow).Methods("POST")
	g.HandleFunc("/stats", h.stats).Methods("GET")
	g.HandleFunc("/{id}", h.detail).Methods("GET")
	g.HandleFunc("/{id}/export", h.export).Methods("GET")
	g.HandleFunc("/{id}/slots", h.slots).Methods("GET")
	g.HandleFunc("/{id}/instantiate", h.instantiate).Methods("POST")
	g.HandleFunc("/{id}/run", h.run).Methods("POST")
	g.HandleFunc("/{id}/debug", h.debug).Methods("GET")
	g.HandleFunc("/{id}", h.update).Methods("PUT")
	g.HandleFunc("/{id}", h.remove).Methods("DELETE")
}

// withRuntimeState renders a flow row with what the RUNTIME says about it alongside its columns.
//
// An `enabled` column answers "was this flow meant to run", which is not the same question as "is
// it running" — and until the runtime's answer was rendered anywhere, the two were
// indistinguishable on screen. A flow whose script would not compile, or one stopped for running
// away, showed as enabled and healthy.
//
// The row is FLATTENED rather than wrapped: the list has always been a list of flow rows, and a
// wrapper would break every caller for the sake of one extra field.
func withRuntimeState(flow *entities.IotFlow, states map[int64]services.FlowState) map[string]any {
	item := map[string]any{}
	if raw, err := json.Marshal(flow); err == nil {
		_ = json.Unmarshal(raw, &item)
	}
	state, known := states[flow.Id]
	switch {
	case !flow.Enabled:
		item["runtimeState"] = "disabled"
	case known:
		item["runtimeState"] = state.State
		if state.Detail != "" {
			item["runtimeDetail"] = state.Detail
		}
	default:
		item["runtimeState"] = "starting" // enabled, not yet reconciled
	}
	return item
}

func (a *flowsApi) list(w http.ResponseWriter, r *http.Request) {
	rows, err := a.flows.List(r.Context())
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	states := a.runtime.States()
	items := make([]map[string]any, 0, len(rows))
	for _, f := range rows {
		items = append(items, withRuntimeState(f, states))
	}
	controllers.SendResult(w, map[string]any{"items": items}, "succeed")
}

func (a *flowsApi) detail(w http.ResponseWriter, r *http.Request) {
	id, ok := readID(w, r)
	if !ok {
		return
	}
	flow, err := a.flows.Detail(r.Context(), id)
	if err != nil {
		controllers.SendError(w, controllers.ErrNotFound, err.Error())
		return
	}
	controllers.SendResult(w, withRuntimeState(flow, a.runtime.States()), "succeed")
}

// stats exposes what the flow runtime is doing — above all, how many telemetry events it has SHED
// because the flows could not keep up. That number has no other symptom: readings keep landing and
// charts keep drawing while the automation on top of them quietly stops firing.
func (a *flowsApi) stats(w http.ResponseWriter, r *http.Request) {
	controllers.SendResult(w, a.runtime.Stats(), "succeed")
}

func (a *flowsApi) create(w http.ResponseWriter, r *http.Request) {
	var body services.SaveFlowRequest
	if !decode(w, r, &body) {
		return
	}
	flow, err := a.flows.Create(r.Context(), body, actorId(r))
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, flow, "succeed")
}

func (a *flowsApi) update(w http.ResponseWriter, r *http.Request) {
	id, ok := readID(w, r)
	if !ok {
		return
	}
	var body services.SaveFlowRequest
	if !decode(w, r, &body) {
		return
	}
	flow, err := a.flows.Update(r.Context(), id, body, actorId(r))
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, flow, "succeed")
}

func (a *flowsApi) remove(w http.ResponseWriter, r *http.Request) {
	id, ok := readID(w, r)
	if !ok {
		return
	}
	if err := a.flows.Delete(r.Context(), id); err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{"deleted": id}, "succeed")
}

func (a *flowsApi) export(w http.ResponseWriter, r *http.Request) {
	id, ok := readID(w, r)
	if !ok {
		return
	}
	doc, err := a.flows.Export(r.Context(), id)
	if err != nil {
		controllers.SendError(w, controllers.ErrNotFound, err.Error())
		return
	}
	controllers.SendResult(w, doc, "succeed")
}

// slots lists the device-role slots a flow declares — empty means it is a concrete flow, non-empty
// means it is a template that can be instantiated.
func (a *flowsApi) slots(w http.ResponseWriter, r *http.Request) {
	id, ok := readID(w, r)
	if !ok {
		return
	}
	names, err := a.flows.Slots(r.Context(), id)
	if err != nil {
		controllers.SendError(w, controllers.ErrNotFound, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{"slots": names}, "succeed")
}

// instantiate stamps a concrete flow out of a template, binding each slot to a real device.
func (a *flowsApi) instantiate(w http.ResponseWriter, r *http.Request) {
	id, ok := readID(w, r)
	if !ok {
		return
	}
	var body struct {
		Name     string            `json:"name"`
		Bindings map[string]string `json:"bindings"`
	}
	if !decode(w, r, &body) {
		return
	}
	flow, err := a.flows.Instantiate(r.Context(), id, body.Name, body.Bindings, actorId(r))
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, flow, "succeed")
}

func (a *flowsApi) importFlow(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 256*1024)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	res, err := a.flows.Import(r.Context(), raw, actorId(r))
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, res, "succeed")
}

// run test-fires a flow: it injects a synthetic value at every input node and returns the resulting
// per-node snapshot, so an author can see the graph light up without waiting for a real reading.
// It runs off the live runtime (its own throwaway sandbox), but an OUTPUT node still acts for real —
// a notify really publishes, a command really routes through the guarded path — which is the point
// of a test-fire.
func (a *flowsApi) run(w http.ResponseWriter, r *http.Request) {
	id, ok := readID(w, r)
	if !ok {
		return
	}
	body := struct {
		Seed float64 `json:"seed"`
	}{}
	// The body is optional — an empty POST seeds 0. Read leniently: only a present, non-empty body
	// is parsed, and a malformed one is reported.
	if raw, _ := io.ReadAll(http.MaxBytesReader(w, r.Body, 64*1024)); len(raw) > 0 {
		if err := json.Unmarshal(raw, &body); err != nil {
			controllers.SendError(w, controllers.ErrParseFailed, err.Error())
			return
		}
	}
	flow, err := a.flows.Detail(r.Context(), id)
	if err != nil {
		controllers.SendError(w, controllers.ErrNotFound, err.Error())
		return
	}
	snap, err := a.runtime.TestRun(r.Context(), flow, body.Seed)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{"nodes": snap}, "succeed")
}

func (a *flowsApi) debug(w http.ResponseWriter, r *http.Request) {
	id, ok := readID(w, r)
	if !ok {
		return
	}
	controllers.SendResult(w, map[string]any{"nodes": a.runtime.DebugSnapshot(id)}, "succeed")
}
