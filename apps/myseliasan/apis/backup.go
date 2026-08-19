package apis

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/myseliasan/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
)

// backupMinPassphraseLen mirrors mymatasan's: the passphrase is the only thing standing
// between this file and the fleet CA private key, so a trivially short one is refused.
const backupMinPassphraseLen = 8

// backupMaxUploadBytes bounds a restore upload. Floor plan images ride inside the bundle,
// so the ceiling is well above mymatasan's — a site with a dozen scanned plans is normal.
const backupMaxUploadBytes = 64 * 1024 * 1024

type backupApi struct {
	session *middlewares.AccessSessionMidware
	serv    services.IBackupService
	audit   services.IAuditService
}

// NewBackupApi mounts the control-plane backup endpoints under /backup.
//
//	GET  /api/backup/sections — section list with row counts, for the export UI
//	POST /api/backup/export   — download a passphrase-encrypted .selbackup
//	POST /api/backup/preview  — decrypt only the manifest, to show what a file holds
//	POST /api/backup/restore  — apply selected sections
//
// Every route is SUPERADMIN-gated, not merely admin. An export is a complete copy of the
// fleet's trust root in one file; a restore can replace the node registry and every role
// in the system. Neither is an operator action, and the audit trail below is what makes
// "who took a copy of the CA" answerable afterwards.
func NewBackupApi(router *mux.Router, auth middlewares.AuthMidware, session *middlewares.AccessSessionMidware, serv services.IBackupService, audit services.IAuditService) {
	h := &backupApi{session: session, serv: serv, audit: audit}
	g := router.PathPrefix("/backup").Subrouter()
	g.Use(auth.Middleware)
	g.Use(session.Middleware)
	g.HandleFunc("/sections", h.requireSuper(h.sections)).Methods("GET")
	g.HandleFunc("/export", h.requireSuper(h.export)).Methods("POST")
	g.HandleFunc("/preview", h.requireSuper(h.preview)).Methods("POST")
	g.HandleFunc("/restore", h.requireSuper(h.restore)).Methods("POST")
}

func (a *backupApi) requireSuper(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a.session.IsSuperadmin(r) {
			controllers.SendError(w, controllers.ErrLimitedAccess, "superadmin access required")
			return
		}
		next(w, r)
	}
}

// sections lists the available sections and their current row counts so the export UI can
// show what will be included and disable the empty ones.
func (a *backupApi) sections(w http.ResponseWriter, r *http.Request) {
	info, err := a.serv.AvailableSections(r.Context())
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, info, "succeed")
}

func (a *backupApi) export(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	var body struct {
		Sections   []string `json:"sections"`
		Passphrase string   `json:"passphrase"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, "invalid request body")
		return
	}
	if len(strings.TrimSpace(body.Passphrase)) < backupMinPassphraseLen {
		controllers.SendError(w, controllers.ErrBadRequest, fmt.Sprintf("passphrase must be at least %d characters", backupMinPassphraseLen))
		return
	}
	blob, err := a.serv.Export(r.Context(), services.BackupRequest{Sections: body.Sections, Passphrase: body.Passphrase})
	if err != nil {
		a.record(r, "backup.export", "error", err.Error(), body.Sections)
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	a.record(r, "backup.export", "success", fmt.Sprintf("exported %d bytes", len(blob)), body.Sections)
	controllers.SendResult(w, map[string]any{
		"filename":   fmt.Sprintf("myseliasan-backup-%s.selbackup", time.Now().Format("20060102-150405")),
		"dataBase64": base64.StdEncoding.EncodeToString(blob),
	}, "succeed")
}

// preview decrypts only the manifest so the UI can show what a file contains (app
// version, sections, counts) before the operator commits to a restore. It is not audited:
// nothing changed and nothing left the building.
func (a *backupApi) preview(w http.ResponseWriter, r *http.Request) {
	data, passphrase, ok := a.readFileBody(w, r)
	if !ok {
		return
	}
	manifest, err := a.serv.Preview(r.Context(), data, passphrase)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, manifest, "succeed")
}

func (a *backupApi) restore(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, backupMaxUploadBytes)
	var body struct {
		DataBase64 string   `json:"dataBase64"`
		Passphrase string   `json:"passphrase"`
		Sections   []string `json:"sections"`
		Mode       string   `json:"mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, "invalid request body")
		return
	}
	data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(body.DataBase64))
	if err != nil || len(data) == 0 {
		controllers.SendError(w, controllers.ErrBadRequest, "backup file is not valid base64")
		return
	}
	result, err := a.serv.Restore(r.Context(), data, services.RestoreRequest{
		Sections:   body.Sections,
		Passphrase: body.Passphrase,
		Mode:       body.Mode,
	})
	if err != nil {
		a.record(r, "backup.restore", "error", err.Error(), body.Sections)
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	// Audited BEFORE the response, and deliberately not conditional on the restore having
	// changed anything: a restore that replaced the roles this actor was authorised under
	// must still be attributable to them.
	a.record(r, "backup.restore", "success",
		fmt.Sprintf("restored %v in %s mode", result.Sections, strings.ToLower(strings.TrimSpace(body.Mode))), result.Sections)
	controllers.SendResult(w, result, "succeed")
}

// readFileBody decodes the shared {dataBase64, passphrase} upload envelope.
func (a *backupApi) readFileBody(w http.ResponseWriter, r *http.Request) ([]byte, string, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, backupMaxUploadBytes)
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
		controllers.SendError(w, controllers.ErrBadRequest, "backup file is not valid base64")
		return nil, "", false
	}
	return data, body.Passphrase, true
}

// record writes the audit entry. The passphrase is NEVER part of it — the metadata blob is
// readable by every superadmin, and the passphrase is the only protection on the exported
// CA key.
func (a *backupApi) record(r *http.Request, action, outcome, detail string, sections []string) {
	if a.audit == nil {
		return
	}
	actorID, actorLabel, roleID := auditActor(r)
	a.audit.Record(r.Context(), services.AuditEntry{
		Action:     action,
		ActorId:    actorID,
		ActorEmail: actorLabel,
		ActorRole:  roleID,
		TargetType: "backup",
		Outcome:    outcome,
		Detail:     detail,
		Metadata:   map[string]any{"sections": sections},
		ClientIp:   clientIP(r),
	})
}
