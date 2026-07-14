package apis

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/myiotsan/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
)

type commandsApi struct {
	commands *services.CommandService
	devices  *services.DeviceService
}

// NewCommandsApi registers actuation.
//
// Every WRITE route here is admin-only (services.Policy). Reading the command history and the
// twin is not: an operator must be able to see what was done and whether the door is actually
// locked. Being able to SEE that somebody unlocked a door is a different thing from being able
// to unlock it, and collapsing the two would mean the audit trail is only visible to the people
// who could have written to it.
func NewCommandsApi(router *mux.Router, commands *services.CommandService, devices *services.DeviceService) {
	h := &commandsApi{commands: commands, devices: devices}

	g := router.PathPrefix("/devices/{id}").Subrouter()
	g.HandleFunc("/commands", h.available).Methods("GET")
	g.HandleFunc("/commands", h.issue).Methods("POST")
	g.HandleFunc("/commands/history", h.history).Methods("GET")
	g.HandleFunc("/twin", h.twin).Methods("GET")
}

// available lists what this device can be told to do — exactly what its profile declares.
func (a *commandsApi) available(w http.ResponseWriter, r *http.Request) {
	id, ok := readID(w, r)
	if !ok {
		return
	}
	dev, err := a.devices.GetById(r.Context(), id)
	if err != nil {
		controllers.SendError(w, controllers.ErrNotFound, err.Error())
		return
	}
	cmds, err := a.commands.CommandsFor(r.Context(), dev.ProfileId)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	// actuationEnabled rides along so the UI can explain WHY a device with declared commands
	// still cannot be commanded, instead of just greying a button out.
	controllers.SendResult(w, map[string]any{
		"items":            cmds,
		"actuationEnabled": dev.ActuationEnabled,
	}, "succeed")
}

func (a *commandsApi) issue(w http.ResponseWriter, r *http.Request) {
	id, ok := readID(w, r)
	if !ok {
		return
	}
	var body services.IssueRequest
	if !decode(w, r, &body) {
		return
	}
	cmd, err := a.commands.Issue(r.Context(), id, body, actorId(r))
	if err != nil {
		// A refusal is still recorded — cmd carries the audit row. Return the reason verbatim:
		// "outside the safe range 5..30" tells an operator what to do; "bad request" does not.
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	// "sent" — NOT "done". The command is confirmed only when the device reports the state
	// back, and the UI must not claim otherwise.
	controllers.SendResult(w, cmd, "succeed")
}

func (a *commandsApi) history(w http.ResponseWriter, r *http.Request) {
	id, ok := readID(w, r)
	if !ok {
		return
	}
	limit, offset := readPaging(r)
	rows, total, err := a.commands.History(r.Context(), id, limit, offset)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{"items": rows, "total": total}, "succeed")
}

// twin is desired vs reported: what was asked for, and what the device says is actually true.
// The gap between them is the only honest answer to "is the door locked?".
func (a *commandsApi) twin(w http.ResponseWriter, r *http.Request) {
	id, ok := readID(w, r)
	if !ok {
		return
	}
	rows, err := a.commands.Twin(r.Context(), id)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{"items": rows}, "succeed")
}
