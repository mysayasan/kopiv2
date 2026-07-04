package apis

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/mymatasan/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
)

// backupMinPassphraseLen mirrors the recovery-escrow endpoint: the passphrase is the
// only thing protecting the exported secrets, so a trivially short one is refused.
const backupMinPassphraseLen = 8

type backupApi struct {
	serv services.IBackupService
}

// NewBackupApi registers the Settings → Backup & Restore endpoints under
// /settings/backup. Export/preview/restore are POSTs, so the router's
// require-admin-for-writes middleware gates them; the file carries plaintext
// secrets and never leaves without a passphrase.
func NewBackupApi(router *mux.Router, serv services.IBackupService) {
	h := &backupApi{serv: serv}
	g := router.PathPrefix("/settings/backup").Subrouter()
	g.HandleFunc("/sections", h.sections).Methods("GET")
	g.HandleFunc("/export", h.export).Methods("POST")
	g.HandleFunc("/preview", h.preview).Methods("POST")
	g.HandleFunc("/restore", h.restore).Methods("POST")
}

// sections lists the available sections and their current row counts so the export
// UI can show what will be included and disable empty ones.
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
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{
		"filename":   fmt.Sprintf("mymatasan-backup-%s.mmbackup", time.Now().Format("20060102-150405")),
		"dataBase64": base64.StdEncoding.EncodeToString(blob),
	}, "succeed")
}

// preview decrypts only the manifest so the UI/wizard can show what a file contains
// (app version, sections, counts) before the operator commits to a restore.
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
	r.Body = http.MaxBytesReader(w, r.Body, 16*1024*1024)
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
	controllers.SendResult(w, result, "succeed")
}

// readFileBody decodes the shared {dataBase64, passphrase} upload envelope.
func (a *backupApi) readFileBody(w http.ResponseWriter, r *http.Request) ([]byte, string, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, 16*1024*1024)
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
