package apis

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/infra/atrest"
)

// recoveryGateApi serves the PUBLIC endpoints used while the app is in recovery mode:
// encryption-at-rest is on, the master key is missing, but a key existed here before (so a
// new one must not be silently generated). These are the only app routes mounted in that
// mode — normal services and login are not started, so the browser can reach nothing but
// the gate until the key is restored.
type recoveryGateApi struct {
	keyPath   string
	cfg       atrest.ProtectorConfig
	restarter restarter
	keyId     string

	mu      sync.Mutex
	lastTry time.Time
}

// NewRecoveryGateApi mounts GET /system/recovery/gate and POST /system/recovery/unlock.
// keyId is the non-secret install identifier shown on the gate so an operator can match
// the correct recovery file.
func NewRecoveryGateApi(router *mux.Router, keyPath string, cfg atrest.ProtectorConfig, restart restarter, keyId string) {
	h := &recoveryGateApi{keyPath: keyPath, cfg: cfg, restarter: restart, keyId: keyId}
	g := router.PathPrefix("/system/recovery").Subrouter()
	g.HandleFunc("/gate", h.gate).Methods("GET")
	g.HandleFunc("/unlock", h.unlock).Methods("POST")
}

// gate tells the SPA it should show the recovery screen instead of the login form.
func (a *recoveryGateApi) gate(w http.ResponseWriter, r *http.Request) {
	controllers.SendResult(w, map[string]any{"pending": true, "keyId": a.keyId}, "succeed")
}

// unlock restores the master key from an uploaded recovery escrow + passphrase, then
// restarts the process so it reboots normally with the key in place. It is a no-op without
// both the file and the correct passphrase (together the secret), and is rate-limited since
// recovery is a rare, deliberate action.
func (a *recoveryGateApi) unlock(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	if time.Since(a.lastTry) < 2*time.Second {
		a.mu.Unlock()
		controllers.SendError(w, controllers.ErrRateLimited, "please wait a moment and try again")
		return
	}
	a.lastTry = time.Now()
	a.mu.Unlock()

	var body struct {
		Passphrase string `json:"passphrase"`
		KeyBase64  string `json:"keyBase64"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, "invalid request body")
		return
	}
	data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(body.KeyBase64))
	if err != nil || len(data) == 0 {
		controllers.SendError(w, controllers.ErrBadRequest, "recovery file is not valid")
		return
	}
	if err := atrest.RestoreFromEscrow(a.keyPath, data, body.Passphrase, a.cfg); err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{"restarting": true}, "succeed")
	go func() {
		time.Sleep(500 * time.Millisecond)
		if a.restarter != nil {
			a.restarter.Restart("recovery key restored")
		}
	}()
}
