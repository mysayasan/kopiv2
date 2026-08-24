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
	"github.com/mysayasan/kopiv2/apps/mymatasan/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
)

// Evidence export. See services/evidence_export.go for what a bundle contains and why.

type evidenceApi struct {
	serv  services.IEvidenceExportService
	audit *Auditor
}

// NewEvidenceApi mounts the export routes under /evidence.
//
//	POST /api/evidence/preview          — what an export WOULD contain, gaps included
//	POST /api/evidence/exports          — start building a bundle
//	GET  /api/evidence/exports/{id}      — job status + manifest once ready
//	GET  /api/evidence/exports/{id}/download — the .zip
//
// Building is asynchronous because a long range takes minutes to decrypt and join, and a
// request-scoped job would be abandoned the moment the operator's browser navigated away.
func NewEvidenceApi(router *mux.Router, serv services.IEvidenceExportService, audit *Auditor) {
	h := &evidenceApi{serv: serv, audit: audit}
	g := router.PathPrefix("/evidence").Subrouter()
	g.HandleFunc("/preview", h.preview).Methods("POST")
	g.HandleFunc("/exports", h.create).Methods("POST")
	g.HandleFunc("/exports/{id}", h.status).Methods("GET")
	g.HandleFunc("/exports/{id}/download", h.download).Methods("GET")
}

// preview is what backs the gap warning in the export dialog. It is deliberately a
// separate call from create: an operator must be able to see that the range they are
// about to hand over has holes in it BEFORE they produce the bundle, not afterwards.
func (a *evidenceApi) preview(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
	var body struct {
		CameraId int64 `json:"cameraId"`
		From     int64 `json:"from"`
		To       int64 `json:"to"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, "invalid request body")
		return
	}
	man, err := a.serv.Preview(r.Context(), body.CameraId, body.From, body.To)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, man, "succeed")
}

func (a *evidenceApi) create(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
	var body struct {
		CameraId int64  `json:"cameraId"`
		From     int64  `json:"from"`
		To       int64  `json:"to"`
		Reason   string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, "invalid request body")
		return
	}
	if strings.TrimSpace(body.Reason) == "" {
		controllers.SendError(w, controllers.ErrBadRequest, "a reason is required for an evidence export")
		return
	}

	actorID, actorName, _ := auditActor(r)
	job, err := a.serv.Create(r.Context(), services.ExportRequest{
		CameraId: body.CameraId, From: body.From, To: body.To,
		Reason: body.Reason, ExporterId: actorID, Exporter: actorName,
	})
	target := strconv.FormatInt(body.CameraId, 10)
	if err != nil {
		a.audit.Failure(r, services.ActionRecordingExport, services.TargetRecording, target, err.Error(),
			map[string]any{"from": body.From, "to": body.To})
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	// Audited at REQUEST time, not on completion: the decision to take a copy of footage
	// out of the system is the auditable act, and a build that later fails must still
	// leave a record that somebody asked for it.
	a.audit.Success(r, services.ActionRecordingExport, services.TargetRecording, target,
		fmt.Sprintf("requested an evidence export of camera %d — %s", body.CameraId, body.Reason),
		map[string]any{
			"exportId": job.Id, "from": body.From, "to": body.To,
			"reason": body.Reason, "hasGaps": job.GapWarning,
		})
	controllers.SendResult(w, job, "succeed")
}

// A CASE bundle is not collectable here. The two routes are governed by different page
// grants — this one by Recordings, the case routes by Cases — so serving a case bundle
// from under /api/evidence hands it to a role that was never granted cases. Found by the
// W3-3a bench, which asked for a case export id through this route and got the zip.
func (a *evidenceApi) notThisRoute(w http.ResponseWriter, job *services.ExportJob, ok bool) bool {
	if !ok || job == nil || job.CaseId > 0 {
		controllers.SendError(w, controllers.ErrBadRequest, "that export is not available (it may have expired)")
		return true
	}
	return false
}

func (a *evidenceApi) status(w http.ResponseWriter, r *http.Request) {
	job, ok := a.serv.Get(mux.Vars(r)["id"])
	if a.notThisRoute(w, job, ok) {
		return
	}
	controllers.SendResult(w, job, "succeed")
}

func (a *evidenceApi) download(w http.ResponseWriter, r *http.Request) {
	job, ok := a.serv.Get(mux.Vars(r)["id"])
	if a.notThisRoute(w, job, ok) {
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
	// The digest of the file being served, so a careful recipient can check it without
	// unzipping first. The manifest inside carries the media's own digest.
	if job.Manifest != nil {
		w.Header().Set("X-Evidence-Output-Sha256", job.Manifest.Output.Sha256)
		w.Header().Set("X-Evidence-Gaps", strconv.Itoa(len(job.Manifest.Gaps)))
	}
	if fi, statErr := f.Stat(); statErr == nil {
		w.Header().Set("Content-Length", strconv.FormatInt(fi.Size(), 10))
	}

	// A second audit entry, because downloading is when the footage actually leaves the
	// building — requesting a bundle and collecting it can be minutes and one shift apart.
	a.audit.Success(r, services.ActionRecordingExport, services.TargetRecording,
		strconv.FormatInt(job.CameraId, 10),
		fmt.Sprintf("downloaded evidence bundle %s", job.Id),
		map[string]any{"exportId": job.Id, "file": name})

	http.ServeContent(w, r, name, fileModTime(f), f)
}

func fileModTime(f *os.File) time.Time {
	if fi, err := f.Stat(); err == nil {
		return fi.ModTime()
	}
	return time.Time{}
}
