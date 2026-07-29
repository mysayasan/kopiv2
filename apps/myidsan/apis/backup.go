package apis

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/myidsan/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
)

// backupMinPassphraseLen mirrors the at-rest recovery escrow: the passphrase is the only
// thing standing between this file and every password hash, TOTP secret, LDAP bind
// password and the SSO CA private key, so a trivially short one is refused.
const backupMinPassphraseLen = 12

// backupMaxUploadBytes caps a restore upload. The archive is configuration, not media —
// a directory of 100k users still lands far inside this.
const backupMaxUploadBytes = 64 * 1024 * 1024

type backupApi struct {
	serv services.IBackupService
	auditRecorder
}

// NewBackupApi wires the backup/restore surface. It is SUPERADMIN-ONLY and deliberately
// not delegatable through the permission matrix: an export hands the caller the entire
// identity store in one file, and a restore rewrites every account and role in it. That
// is strictly more power than any individual admin endpoint grants, so it sits with the
// other privilege-affecting surfaces (user-credential, mfa-admin, password-reset).
func NewBackupApi(
	router *mux.Router,
	auth middlewares.AuthMidware,
	access *middlewares.AccessSessionMidware,
	serv services.IBackupService,
	audit services.IAuditService,
	stepUp services.IStepUpService,
	trustedProxies []string,
) {
	h := &backupApi{serv: serv, auditRecorder: newAuditRecorder(audit, trustedProxies)}

	group := router.PathPrefix("/backup").Subrouter()
	group.Use(auth.Middleware)
	group.Use(access.Middleware)
	group.Use(access.RequireSuperadmin)

	group.HandleFunc("/sections", h.sections).Methods("GET")
	// Export hands over the whole identity store; restore rewrites it. Preview is
	// deliberately NOT gated — it only reads a manifest from a file the operator
	// already holds, and making them re-authenticate to look before they leap would
	// push them to skip the looking.
	group.HandleFunc("/export", requireStepUp(stepUp, h.export)).Methods("POST")
	group.HandleFunc("/preview", h.preview).Methods("POST")
	group.HandleFunc("/restore", requireStepUp(stepUp, h.restore)).Methods("POST")
}

// sections lists each section and its current row count, so the export screen can show
// what is about to be included and grey out the empty ones.
func (a *backupApi) sections(w http.ResponseWriter, r *http.Request) {
	info, err := a.serv.AvailableSections(r.Context())
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, info)
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
		controllers.SendError(w, controllers.ErrBadRequest,
			fmt.Sprintf("passphrase must be at least %d characters", backupMinPassphraseLen))
		return
	}
	blob, err := a.serv.Export(r.Context(), services.BackupRequest{
		Sections:   body.Sections,
		Passphrase: body.Passphrase,
	})
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	// An export removes the entire identity store from this server in one file. It is
	// among the most sensitive actions available, and it leaves no other trace.
	a.record(r, services.AuditEntry{
		Action:     services.ActionBackupExport,
		TargetType: "backup",
		Detail:     "exported an encrypted copy of the identity store",
		Metadata:   map[string]any{"sections": body.Sections, "bytes": len(blob)},
	})

	controllers.SendResult(w, map[string]any{
		"filename":   fmt.Sprintf("myidsan-backup-%s.idbackup", time.Now().Format("20060102-150405")),
		"dataBase64": base64.StdEncoding.EncodeToString(blob),
	})
}

// preview decrypts only the manifest, so the operator can confirm the app version,
// sections and row counts before committing to a restore that replaces live accounts.
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
	controllers.SendResult(w, manifest)
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
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	// Recorded even though the restore has just wiped and rewritten the accounts table:
	// the audit table is NOT part of a backup and is never cleared by a restore, so this
	// entry survives and is the only record that the rewrite happened at all.
	//
	// The actor is captured from the request's claims, which were read before the restore
	// invalidated every session — including the caller's own.
	a.record(r, services.AuditEntry{
		Action:     services.ActionBackupRestore,
		TargetType: "backup",
		Detail:     "restored this server from a backup",
		Metadata: map[string]any{
			"mode":       body.Mode,
			"sections":   result.Sections,
			"restored":   result.Restored,
			"skipped":    result.Skipped,
			"appVersion": result.AppVersion,
		},
	})

	// The caller's own session was just invalidated along with everyone else's, so the
	// SPA must send them back to the login screen rather than keep polling with a cookie
	// that no longer resolves.
	controllers.SendResult(w, result)
}

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
