package apis

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/mymatasan/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
)

type teachApi struct {
	serv services.ITeachService
}

// NewTeachApi registers the Teach wizard's skill routes. Preflight capability
// checks reuse GET /api/training/capability (same trainer, same answer).
func NewTeachApi(router *mux.Router, serv services.ITeachService) {
	handler := &teachApi{serv: serv}
	group := router.PathPrefix("/teach").Subrouter()

	group.HandleFunc("/skills", handler.listSkills).Methods("GET")
	group.HandleFunc("/skills", handler.createSkill).Methods("POST")
	group.HandleFunc("/skills/{id}", handler.getSkill).Methods("GET")
	group.HandleFunc("/skills/{id}", handler.updateSkill).Methods("PUT")
	group.HandleFunc("/skills/{id}", handler.deleteSkill).Methods("DELETE")
	group.HandleFunc("/skills/{id}/session", handler.sessionInfo).Methods("GET")
	group.HandleFunc("/skills/{id}/session/start", handler.startSession).Methods("POST")
	group.HandleFunc("/skills/{id}/session/stop", handler.stopSession).Methods("POST")
	group.HandleFunc("/skills/{id}/evaluate", handler.startEvaluation).Methods("POST")
	group.HandleFunc("/skills/{id}/evaluation", handler.evaluationState).Methods("GET")
	group.HandleFunc("/skills/{id}/testdrive/start", handler.startTestDrive).Methods("POST")
	group.HandleFunc("/skills/{id}/testdrive/stop", handler.stopTestDrive).Methods("POST")
	group.HandleFunc("/skills/{id}/testdrive", handler.testDriveFrame).Methods("GET")
	group.HandleFunc("/skills/{id}/activate", handler.activateSkill).Methods("POST")
	group.HandleFunc("/skills/{id}/deactivate", handler.deactivateSkill).Methods("POST")
	group.HandleFunc("/skills/{id}/export", handler.exportSkill).Methods("POST")
	group.HandleFunc("/import/preview", handler.previewImport).Methods("POST")
	group.HandleFunc("/import", handler.importSkill).Methods("POST")
	group.HandleFunc("/skills/{id}/feedback", handler.listFeedback).Methods("GET")
	group.HandleFunc("/skills/{id}/feedback", handler.addFeedback).Methods("POST")
	group.HandleFunc("/active", handler.activeSessions).Methods("GET")
}

func (a *teachApi) listFeedback(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	items, err := a.serv.ListSkillFeedback(r.Context(), id)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{"items": items}, "succeed")
}

func (a *teachApi) addFeedback(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	var body struct {
		AlertId int64  `json:"alertId"`
		Verdict string `json:"verdict"`
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	if err := a.serv.AddSkillFeedback(r.Context(), id, body.AlertId, body.Verdict, localUserID(r)); err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, map[string]bool{"filed": true}, "succeed")
}

func (a *teachApi) exportSkill(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	var body struct {
		Passphrase    string `json:"passphrase"`
		IncludeImages bool   `json:"includeImages"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, "invalid request body")
		return
	}
	filename, blob, err := a.serv.ExportSkill(r.Context(), id, body.Passphrase, body.IncludeImages)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{
		"filename":   filename,
		"dataBase64": base64.StdEncoding.EncodeToString(blob),
	}, "succeed")
}

// readPackageBody decodes the shared {dataBase64, passphrase} upload envelope.
func (a *teachApi) readPackageBody(w http.ResponseWriter, r *http.Request) ([]byte, string, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, 160*1024*1024)
	var body struct {
		DataBase64 string `json:"dataBase64"`
		Passphrase string `json:"passphrase"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, "invalid request body")
		return nil, "", false
	}
	data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(body.DataBase64))
	if err != nil || len(data) == 0 {
		controllers.SendError(w, controllers.ErrBadRequest, "package file is not valid base64")
		return nil, "", false
	}
	return data, body.Passphrase, true
}

func (a *teachApi) previewImport(w http.ResponseWriter, r *http.Request) {
	data, passphrase, ok := a.readPackageBody(w, r)
	if !ok {
		return
	}
	manifest, err := a.serv.PreviewSkillPackage(r.Context(), data, passphrase)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, manifest, "succeed")
}

func (a *teachApi) importSkill(w http.ResponseWriter, r *http.Request) {
	data, passphrase, ok := a.readPackageBody(w, r)
	if !ok {
		return
	}
	skill, err := a.serv.ImportSkill(r.Context(), data, passphrase, localUserID(r))
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, skill, "succeed")
}

func (a *teachApi) startTestDrive(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := a.serv.StartTestDrive(r.Context(), id, localUserID(r)); err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, map[string]bool{"started": true}, "succeed")
}

func (a *teachApi) stopTestDrive(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	a.serv.StopTestDrive(r.Context(), id)
	controllers.SendResult(w, map[string]bool{"stopped": true}, "succeed")
}

func (a *teachApi) testDriveFrame(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	frame, err := a.serv.TestDriveFrame(r.Context(), id)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, frame, "succeed")
}

func (a *teachApi) activateSkill(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	result, err := a.serv.ActivateSkill(r.Context(), id, localUserID(r))
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, result, "succeed")
}

func (a *teachApi) deactivateSkill(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	skill, err := a.serv.DeactivateSkill(r.Context(), id, localUserID(r))
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, skill, "succeed")
}

func (a *teachApi) startEvaluation(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	state, err := a.serv.StartEvaluation(r.Context(), id, localUserID(r))
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, state, "succeed")
}

func (a *teachApi) evaluationState(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	state, err := a.serv.EvaluationState(r.Context(), id)
	if err != nil {
		controllers.SendError(w, controllers.ErrNotFound, err.Error())
		return
	}
	controllers.SendResult(w, state, "succeed")
}

func (a *teachApi) sessionInfo(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	info, err := a.serv.SessionInfo(r.Context(), id)
	if err != nil {
		controllers.SendError(w, controllers.ErrNotFound, err.Error())
		return
	}
	controllers.SendResult(w, info, "succeed")
}

func (a *teachApi) startSession(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	var body struct {
		ClassLabel string `json:"classLabel"`
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	info, err := a.serv.StartSession(r.Context(), id, body.ClassLabel, localUserID(r))
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, info, "succeed")
}

func (a *teachApi) stopSession(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	info, err := a.serv.StopSession(r.Context(), id, localUserID(r))
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, info, "succeed")
}

func (a *teachApi) activeSessions(w http.ResponseWriter, r *http.Request) {
	controllers.SendResult(w, map[string]any{"items": a.serv.ActiveSessions(r.Context())}, "succeed")
}

func (a *teachApi) listSkills(w http.ResponseWriter, r *http.Request) {
	items, err := a.serv.ListSkills(r.Context())
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{"items": items}, "succeed")
}

func (a *teachApi) createSkill(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1*1024*1024)
	var body services.TeachSkillRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	skill, err := a.serv.CreateSkill(r.Context(), body, localUserID(r))
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, skill, "succeed")
}

func (a *teachApi) getSkill(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	skill, err := a.serv.GetSkill(r.Context(), id)
	if err != nil {
		controllers.SendError(w, controllers.ErrNotFound, err.Error())
		return
	}
	controllers.SendResult(w, skill, "succeed")
}

func (a *teachApi) updateSkill(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1*1024*1024)
	var body services.TeachSkillRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	skill, err := a.serv.UpdateSkill(r.Context(), id, body, localUserID(r))
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, skill, "succeed")
}

func (a *teachApi) deleteSkill(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	count, err := a.serv.DeleteSkill(r.Context(), id)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, map[string]uint64{"deleted": count}, "succeed")
}
