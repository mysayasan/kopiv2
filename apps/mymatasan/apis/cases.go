package apis

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	"github.com/mysayasan/kopiv2/apps/mymatasan/services"
	sharedaudit "github.com/mysayasan/kopiv2/domain/shared/audit"
	sharedservices "github.com/mysayasan/kopiv2/domain/shared/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
)

// Case files. See services/case_file.go for what a case is and why it holds its footage.
//
// EVERY MUTATION IS A POST, including the ones REST would spell PUT or DELETE. That is
// not sloppiness: the appliance role model grants verbs, and its grantable "use" rung is
// GET+POST precisely so that DELETE stays an administrator's verb everywhere. Spelling
// "remove this item from the case" as DELETE would have made it ungrantable to the
// operator who does the work, and widening the rung to include DELETE would have handed
// out the verb that destroys footage. The one genuine DELETE here is deleting the case
// itself, which is exactly the act that should need an administrator.

type caseApi struct {
	serv   services.ICaseService
	export services.IEvidenceExportService
	audit  *Auditor
	trail  services.IAuditService
	users  sharedservices.ILocalUserService
}

// NewCaseApi mounts the case routes under /cases.
//
//	GET    /api/cases                              the list
//	POST   /api/cases                              open a case
//	GET    /api/cases/{id}                         one case, its evidence and what it holds
//	POST   /api/cases/{id}                         retitle / re-summarize / reassign
//	POST   /api/cases/{id}/close                   close it, with a stated outcome
//	POST   /api/cases/{id}/reopen                  reopen it
//	DELETE /api/cases/{id}                         remove it entirely (administrator)
//	POST   /api/cases/{id}/items                   add evidence
//	POST   /api/cases/{id}/items/{itemId}          annotate evidence
//	POST   /api/cases/{id}/items/{itemId}/remove   take evidence back out
//	POST   /api/cases/{id}/export                  build the case bundle
//	GET    /api/cases/exports/{exportId}           bundle status
//	GET    /api/cases/exports/{exportId}/download  the .zip
//
// The export routes live UNDER /api/cases rather than under /api/evidence so that the
// Cases page grant alone is enough to work a case end to end. A role granted Cases but
// not Recordings would otherwise be able to assemble a bundle and not collect it.
func NewCaseApi(
	router *mux.Router,
	serv services.ICaseService,
	export services.IEvidenceExportService,
	trail services.IAuditService,
	users sharedservices.ILocalUserService,
	audit *Auditor,
) {
	h := &caseApi{serv: serv, export: export, trail: trail, users: users, audit: audit}
	g := router.PathPrefix("/cases").Subrouter()

	// Registered before "/{id}" so an export id is never read as a case id.
	// Who a case can be handed to. It is here rather than reusing /api/settings/users
	// because assignment is part of working a case and that route is administrator-only:
	// an operator who cannot read it could open a case and never hand it to anybody. It
	// answers with ids and display names ONLY — none of the account surface that makes
	// the settings route administrative.
	g.HandleFunc("/assignees", h.assignees).Methods("GET")
	g.HandleFunc("/exports/{exportId}", h.exportStatus).Methods("GET")
	g.HandleFunc("/exports/{exportId}/download", h.exportDownload).Methods("GET")

	g.HandleFunc("", h.list).Methods("GET")
	g.HandleFunc("", h.create).Methods("POST")
	g.HandleFunc("/{id}", h.detail).Methods("GET")
	g.HandleFunc("/{id}", h.update).Methods("POST")
	g.HandleFunc("/{id}", h.remove).Methods("DELETE")
	g.HandleFunc("/{id}/close", h.close).Methods("POST")
	g.HandleFunc("/{id}/reopen", h.reopen).Methods("POST")
	g.HandleFunc("/{id}/items", h.addItem).Methods("POST")
	g.HandleFunc("/{id}/items/{itemId}", h.updateItem).Methods("POST")
	g.HandleFunc("/{id}/items/{itemId}/remove", h.removeItem).Methods("POST")
	g.HandleFunc("/{id}/export", h.exportCase).Methods("POST")
}

func (a *caseApi) actor(r *http.Request) services.CaseActor {
	id, name, _ := auditActor(r)
	return services.CaseActor{Id: id, Name: name}
}

func caseIdVar(r *http.Request, key string) int64 {
	id, _ := strconv.ParseInt(mux.Vars(r)[key], 10, 64)
	return id
}

// resolveAssignee turns a user id into a display name, and refuses an id that names
// nobody. A case assigned to a user that does not exist is a case nobody is working.
func (a *caseApi) resolveAssignee(r *http.Request, userId int64) (string, error) {
	if userId <= 0 {
		return "", nil
	}
	if a.users == nil {
		return "", nil
	}
	users, _, err := a.users.Get(r.Context(), 500, 0)
	if err != nil {
		return "", err
	}
	for _, u := range users {
		if u == nil || u.Id != userId {
			continue
		}
		name := strings.TrimSpace(u.Username)
		if name == "" {
			name = strings.TrimSpace(u.DisplayName)
		}
		if name == "" {
			name = strconv.FormatInt(u.Id, 10)
		}
		return name, nil
	}
	return "", fmt.Errorf("there is no user with id %d to assign this case to", userId)
}

func (a *caseApi) assignees(w http.ResponseWriter, r *http.Request) {
	type assignee struct {
		Id   int64  `json:"id"`
		Name string `json:"name"`
	}
	out := []assignee{}
	if a.users != nil {
		users, _, err := a.users.Get(r.Context(), 500, 0)
		if err != nil {
			controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
			return
		}
		for _, u := range users {
			if u == nil {
				continue
			}
			name := strings.TrimSpace(u.Username)
			if name == "" {
				name = strings.TrimSpace(u.DisplayName)
			}
			if name == "" {
				name = strconv.FormatInt(u.Id, 10)
			}
			out = append(out, assignee{Id: u.Id, Name: name})
		}
	}
	controllers.SendResult(w, map[string]any{"assignees": out}, "succeed")
}

func (a *caseApi) list(w http.ResponseWriter, r *http.Request) {
	limit := uint64(parseInt64Query(r, "limit"))
	offset := uint64(parseInt64Query(r, "offset"))
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	assigned := parseInt64Query(r, "assignedTo")
	rows, total, err := a.serv.List(r.Context(), limit, offset, status, assigned)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{"cases": rows, "total": total}, "succeed")
}

func (a *caseApi) detail(w http.ResponseWriter, r *http.Request) {
	detail, err := a.serv.Get(r.Context(), caseIdVar(r, "id"))
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, detail, "succeed")
}

func (a *caseApi) create(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	var body struct {
		Title      string `json:"title"`
		Summary    string `json:"summary"`
		AssignedTo int64  `json:"assignedTo"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, "invalid request body")
		return
	}
	name, err := a.resolveAssignee(r, body.AssignedTo)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	row, err := a.serv.Create(r.Context(), services.CaseCreate{
		Title: body.Title, Summary: body.Summary,
		AssignedTo: body.AssignedTo, AssignedName: name,
		Actor: a.actor(r),
	})
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	a.audit.Success(r, services.ActionCaseCreate, services.TargetCase, strconv.FormatInt(row.Id, 10),
		fmt.Sprintf("opened case %d — %s", row.Id, row.Title),
		map[string]any{"title": row.Title, "assignedTo": row.AssignedName})
	controllers.SendResult(w, row, "succeed")
}

func (a *caseApi) update(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	var body struct {
		Title      *string `json:"title"`
		Summary    *string `json:"summary"`
		AssignedTo *int64  `json:"assignedTo"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, "invalid request body")
		return
	}
	id := caseIdVar(r, "id")
	req := services.CaseUpdate{Title: body.Title, Summary: body.Summary, AssignedTo: body.AssignedTo}
	if body.AssignedTo != nil {
		name, err := a.resolveAssignee(r, *body.AssignedTo)
		if err != nil {
			controllers.SendError(w, controllers.ErrBadRequest, err.Error())
			return
		}
		req.AssignedName = name
	}
	row, err := a.serv.Update(r.Context(), id, req)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	// Reassignment is its own action rather than a field inside case.update: "who was
	// this handed to, and when" is a question asked on its own, and an audit trail that
	// buries it in a generic update is one that cannot answer it.
	action := services.ActionCaseUpdate
	detail := fmt.Sprintf("edited case %d", id)
	if body.AssignedTo != nil {
		action = services.ActionCaseAssign
		if row.AssignedTo > 0 {
			detail = fmt.Sprintf("assigned case %d to %s", id, row.AssignedName)
		} else {
			detail = fmt.Sprintf("removed the assignee from case %d", id)
		}
	}
	a.audit.Success(r, action, services.TargetCase, strconv.FormatInt(id, 10), detail,
		map[string]any{"title": row.Title, "assignedTo": row.AssignedName})
	controllers.SendResult(w, row, "succeed")
}

func (a *caseApi) close(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	var body struct {
		Outcome string `json:"outcome"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, "invalid request body")
		return
	}
	id := caseIdVar(r, "id")
	// Read the hold BEFORE closing, so the audit entry records what closing released.
	// Afterwards the case is closed and the answer is zero, which is the least useful
	// possible thing to write down about this exact decision.
	released := 0
	if before, err := a.serv.Get(r.Context(), id); err == nil {
		released = before.Hold.Segments
	}
	row, err := a.serv.Close(r.Context(), id, body.Outcome, a.actor(r))
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	a.audit.Success(r, services.ActionCaseClose, services.TargetCase, strconv.FormatInt(id, 10),
		fmt.Sprintf("closed case %d — %s", id, row.Outcome),
		map[string]any{"outcome": row.Outcome, "releasedSegments": released})
	controllers.SendResult(w, row, "succeed")
}

func (a *caseApi) reopen(w http.ResponseWriter, r *http.Request) {
	id := caseIdVar(r, "id")
	row, err := a.serv.Reopen(r.Context(), id, a.actor(r))
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	a.audit.Success(r, services.ActionCaseReopen, services.TargetCase, strconv.FormatInt(id, 10),
		fmt.Sprintf("reopened case %d", id), nil)
	controllers.SendResult(w, row, "succeed")
}

func (a *caseApi) remove(w http.ResponseWriter, r *http.Request) {
	id := caseIdVar(r, "id")
	detail, derr := a.serv.Get(r.Context(), id)
	if err := a.serv.Delete(r.Context(), id); err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	meta := map[string]any{}
	if derr == nil && detail != nil {
		meta["title"] = detail.Case.Title
		meta["items"] = len(detail.Items)
		meta["releasedSegments"] = detail.Hold.Segments
	}
	a.audit.Success(r, services.ActionCaseDelete, services.TargetCase, strconv.FormatInt(id, 10),
		fmt.Sprintf("deleted case %d", id), meta)
	controllers.SendResult(w, map[string]any{"deleted": true}, "succeed")
}

func (a *caseApi) addItem(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	var body struct {
		Kind         string `json:"kind"`
		CameraId     int64  `json:"cameraId"`
		StartedAt    int64  `json:"startedAt"`
		EndedAt      int64  `json:"endedAt"`
		Label        string `json:"label"`
		Note         string `json:"note"`
		SourceId     int64  `json:"sourceId"`
		SnapshotPath string `json:"snapshotPath"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, "invalid request body")
		return
	}
	id := caseIdVar(r, "id")
	item, err := a.serv.AddItem(r.Context(), id, services.CaseItemInput{
		Kind: body.Kind, CameraId: body.CameraId,
		StartedAt: body.StartedAt, EndedAt: body.EndedAt,
		Label: body.Label, Note: body.Note,
		SourceId: body.SourceId, SnapshotPath: body.SnapshotPath,
		Actor: a.actor(r),
	})
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	a.audit.Success(r, services.ActionCaseItemAdd, services.TargetCase, strconv.FormatInt(id, 10),
		describeItem("added", item), itemMeta(item))
	controllers.SendResult(w, item, "succeed")
}

func (a *caseApi) updateItem(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	var body struct {
		Label *string `json:"label"`
		Note  *string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, "invalid request body")
		return
	}
	id := caseIdVar(r, "id")
	item, err := a.serv.UpdateItem(r.Context(), id, caseIdVar(r, "itemId"),
		services.CaseItemUpdate{Label: body.Label, Note: body.Note})
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	a.audit.Success(r, services.ActionCaseItemUpdate, services.TargetCase, strconv.FormatInt(id, 10),
		describeItem("annotated", item), itemMeta(item))
	controllers.SendResult(w, item, "succeed")
}

func (a *caseApi) removeItem(w http.ResponseWriter, r *http.Request) {
	id := caseIdVar(r, "id")
	item, err := a.serv.RemoveItem(r.Context(), id, caseIdVar(r, "itemId"))
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	// Recorded with the span it releases. Removing evidence never deletes footage — it
	// only stops the case extending its life past retention — but the day that matters is
	// the day before the sweep, and this entry is what makes that reconstructible.
	a.audit.Success(r, services.ActionCaseItemRemove, services.TargetCase, strconv.FormatInt(id, 10),
		describeItem("removed", item), itemMeta(item))
	controllers.SendResult(w, map[string]any{"removed": true, "item": item}, "succeed")
}

func describeItem(verb string, item *entities.CaseItem) string {
	if item == nil {
		return verb + " evidence"
	}
	if item.CameraId <= 0 {
		return fmt.Sprintf("%s a note", verb)
	}
	return fmt.Sprintf("%s %s evidence from camera %d, %s to %s", verb, item.Kind, item.CameraId,
		time.Unix(item.StartedAt, 0).UTC().Format(time.RFC3339),
		time.Unix(item.EndedAt, 0).UTC().Format(time.RFC3339))
}

func itemMeta(item *entities.CaseItem) map[string]any {
	if item == nil {
		return nil
	}
	return map[string]any{
		"itemId": item.Id, "kind": item.Kind, "cameraId": item.CameraId,
		"from": item.StartedAt, "to": item.EndedAt, "label": item.Label,
	}
}

// --- exporting ---------------------------------------------------------------

func (a *caseApi) exportCase(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
	var body struct {
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, "invalid request body")
		return
	}
	if strings.TrimSpace(body.Reason) == "" {
		controllers.SendError(w, controllers.ErrBadRequest, "a reason is required for an evidence export")
		return
	}
	id := caseIdVar(r, "id")
	detail, err := a.serv.Get(r.Context(), id)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	actorId, actorName, _ := auditActor(r)
	custody, note := a.custodyFor(r, id)
	job, err := a.export.CreateCase(r.Context(), services.CaseExportRequest{
		Case: detail.Case, Items: detail.Items,
		Custody: custody, CustodyNote: note,
		Reason: strings.TrimSpace(body.Reason), ExporterId: actorId, Exporter: actorName,
	})
	if err != nil {
		a.audit.Failure(r, services.ActionRecordingExport, services.TargetCase, strconv.FormatInt(id, 10),
			err.Error(), map[string]any{"reason": body.Reason})
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	// Audited at REQUEST time for the same reason a single-clip export is: deciding to
	// take a copy of the footage out of the system is the auditable act, and a build that
	// later fails must still leave a record that somebody asked for it.
	//
	// The action is recording.export, not a case-specific one, so that "what footage left
	// this appliance" is one filter and not two.
	a.audit.Success(r, services.ActionRecordingExport, services.TargetCase, strconv.FormatInt(id, 10),
		fmt.Sprintf("requested an evidence export of case %d — %s", id, strings.TrimSpace(body.Reason)),
		map[string]any{
			"exportId": job.Id, "reason": strings.TrimSpace(body.Reason),
			"items": len(detail.Items), "title": detail.Case.Title,
		})
	controllers.SendResult(w, job, "succeed")
}

// custodyFor reads the case's own audit entries, oldest first — the chain of custody the
// bundle ships. The note is what goes in the bundle when the trail could not be read: an
// empty custody list must never be presented as "nothing happened".
func (a *caseApi) custodyFor(r *http.Request, caseId int64) ([]services.CaseCustodyEntry, string) {
	if a.trail == nil {
		return nil, "This appliance has no audit trail, so no chain of custody could be included."
	}
	rows, _, err := a.trail.List(r.Context(), 500, 0, sharedaudit.Filter{
		TargetType: services.TargetCase,
		TargetId:   strconv.FormatInt(caseId, 10),
	})
	if err != nil {
		return nil, "The audit trail could not be read when this bundle was built, so the chain of custody below may be incomplete."
	}
	out := make([]services.CaseCustodyEntry, 0, len(rows))
	// List returns newest-first; a chain of custody reads forwards.
	for i := len(rows) - 1; i >= 0; i-- {
		row := rows[i]
		if row == nil {
			continue
		}
		out = append(out, services.CaseCustodyEntry{
			At:      time.Unix(row.CreatedAt, 0).UTC().Format(time.RFC3339),
			Actor:   row.ActorEmail,
			Action:  row.Action,
			Outcome: row.Outcome,
			Detail:  row.Detail,
		})
	}
	if len(rows) >= 500 {
		return out, "This case has more recorded actions than one bundle carries; the 500 most recent are included."
	}
	return out, ""
}

func (a *caseApi) exportStatus(w http.ResponseWriter, r *http.Request) {
	job, ok := a.export.Get(mux.Vars(r)["exportId"])
	if !ok || job.CaseId <= 0 {
		controllers.SendError(w, controllers.ErrBadRequest, "that export is not available (it may have expired)")
		return
	}
	controllers.SendResult(w, job, "succeed")
}

func (a *caseApi) exportDownload(w http.ResponseWriter, r *http.Request) {
	job, ok := a.export.Get(mux.Vars(r)["exportId"])
	// The CaseId check is authorization, not tidiness: without it this route — reachable
	// with only the Cases grant — would serve any single-clip bundle whose id was known,
	// including one built by a role that may export footage this caller may not.
	if !ok || job.CaseId <= 0 {
		controllers.SendError(w, controllers.ErrBadRequest, "that export is not available (it may have expired)")
		return
	}
	if job.Status != services.ExportReady || job.BundlePath == "" {
		controllers.SendError(w, controllers.ErrBadRequest, "that export is not ready")
		return
	}
	f, err := os.Open(job.BundlePath)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, "the bundle is no longer available")
		return
	}
	defer f.Close()

	name := filepath.Base(job.BundlePath)
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	if job.CaseManifest != nil {
		w.Header().Set("X-Evidence-Clips", strconv.Itoa(job.CaseManifest.Totals.ClipsWritten))
		w.Header().Set("X-Evidence-Clips-Missing", strconv.Itoa(job.CaseManifest.Totals.ClipsMissing))
	}
	if fi, statErr := f.Stat(); statErr == nil {
		w.Header().Set("Content-Length", strconv.FormatInt(fi.Size(), 10))
	}
	// A second entry, because collecting the bundle is when the footage actually leaves.
	a.audit.Success(r, services.ActionRecordingExport, services.TargetCase,
		strconv.FormatInt(job.CaseId, 10),
		fmt.Sprintf("downloaded case bundle %s", job.Id),
		map[string]any{"exportId": job.Id, "file": name})

	http.ServeContent(w, r, name, fileModTime(f), f)
}
