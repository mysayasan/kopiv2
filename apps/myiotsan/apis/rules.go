package apis

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/myiotsan/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
)

type rulesApi struct {
	rules *services.RuleService
}

// NewRulesApi registers the rules and the alert log they produce.
func NewRulesApi(router *mux.Router, rules *services.RuleService) {
	h := &rulesApi{rules: rules}

	g := router.PathPrefix("/rules").Subrouter()
	g.HandleFunc("", h.list).Methods("GET")
	g.HandleFunc("", h.create).Methods("POST")
	g.HandleFunc("/{id}", h.update).Methods("PUT")
	g.HandleFunc("/{id}", h.remove).Methods("DELETE")

	a := router.PathPrefix("/alerts").Subrouter()
	a.HandleFunc("", h.listAlerts).Methods("GET")
	// Acknowledging is an operator power. There is deliberately NO delete: an operator who was
	// present at an incident must not be able to erase the record of it.
	a.HandleFunc("/{id}/ack", h.ack).Methods("POST")
}

func (a *rulesApi) list(w http.ResponseWriter, r *http.Request) {
	rows, err := a.rules.List(r.Context())
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{"items": rows}, "succeed")
}

func (a *rulesApi) create(w http.ResponseWriter, r *http.Request) {
	var body services.SaveRuleRequest
	if !decode(w, r, &body) {
		return
	}
	rule, err := a.rules.Create(r.Context(), body, actorId(r))
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, rule, "succeed")
}

func (a *rulesApi) update(w http.ResponseWriter, r *http.Request) {
	id, ok := readID(w, r)
	if !ok {
		return
	}
	var body services.SaveRuleRequest
	if !decode(w, r, &body) {
		return
	}
	rule, err := a.rules.Update(r.Context(), id, body, actorId(r))
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, rule, "succeed")
}

func (a *rulesApi) remove(w http.ResponseWriter, r *http.Request) {
	id, ok := readID(w, r)
	if !ok {
		return
	}
	if err := a.rules.Delete(r.Context(), id); err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{"deleted": id}, "succeed")
}

func (a *rulesApi) listAlerts(w http.ResponseWriter, r *http.Request) {
	limit, offset := readPaging(r)
	var deviceId int64
	if v := r.URL.Query().Get("deviceId"); v != "" {
		if id, err := parseInt(v); err == nil {
			deviceId = id
		}
	}
	rows, total, err := a.rules.ListAlerts(r.Context(), deviceId, limit, offset)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{"items": rows, "total": total}, "succeed")
}

func (a *rulesApi) ack(w http.ResponseWriter, r *http.Request) {
	id, ok := readID(w, r)
	if !ok {
		return
	}
	if err := a.rules.AckAlert(r.Context(), id, actorId(r), actorName(r)); err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{"acked": id}, "succeed")
}
