package apis

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/domain/shared/fleetnode"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/infra/pairing"
)

type pairingApi struct {
	svc       fleetnode.IPairingService
	onAdopted func()
}

// NewPairingPublicApi registers the crypto-authenticated routes the control plane
// calls over the network. They are mounted on the unauthenticated /api router —
// the parent has no local session, so these carry their own cryptographic auth
// (fleet-key assertion for adopt, pairing token for release). Register this BEFORE
// the local-session catch-all subrouter so the requests match here first.
//
//	POST /pairing/adopt    — fleet-key assertion + claim code bind the node
//	POST /pairing/release  — pairing token releases the node
//
// onAdopted (optional) is invoked after a successful adoption so the caller can
// kick certificate enrollment for the mTLS channel.
func NewPairingPublicApi(public *mux.Router, svc fleetnode.IPairingService, onAdopted func()) {
	h := &pairingApi{svc: svc, onAdopted: onAdopted}
	pub := public.PathPrefix("/pairing").Subrouter()
	pub.HandleFunc("/adopt", h.adopt).Methods("POST")
	pub.HandleFunc("/release", h.release).Methods("POST")
}

// NewPairingApi registers the node-UI routes behind the local session +
// admin-for-writes middleware:
//
//	GET  /pairing/status
//	POST /pairing/claim-code  (admin)
//	POST /pairing/unpair      (admin — self-drop)
//	PUT  /pairing/fleet-key   (admin)
func NewPairingApi(protected *mux.Router, svc fleetnode.IPairingService) {
	h := &pairingApi{svc: svc}
	prot := protected.PathPrefix("/pairing").Subrouter()
	prot.HandleFunc("/status", h.status).Methods("GET")
	prot.HandleFunc("/claim-code", h.claimCode).Methods("POST")
	prot.HandleFunc("/unpair", h.unpair).Methods("POST")
	prot.HandleFunc("/fleet-key", h.setFleetKey).Methods("PUT")
}

func (a *pairingApi) status(w http.ResponseWriter, r *http.Request) {
	st, err := a.svc.Status(r.Context())
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, st, "succeed")
}

func (a *pairingApi) claimCode(w http.ResponseWriter, r *http.Request) {
	code, expiresAt, err := a.svc.GenerateClaimCode(r.Context())
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{"code": code, "expiresAt": expiresAt}, "succeed")
}

func (a *pairingApi) setFleetKey(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Key string `json:"key"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	if err := a.svc.SetFleetKey(r.Context(), body.Key); err != nil {
		if errors.Is(err, fleetnode.ErrPairingFleetKeyShort) {
			controllers.SendError(w, controllers.ErrBadRequest, err.Error())
			return
		}
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{"fleetKeySet": true}, "succeed")
}

func (a *pairingApi) unpair(w http.ResponseWriter, r *http.Request) {
	parentURL, err := a.svc.Unpair(r.Context())
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	// Best-effort courtesy notice so the ex-parent can drop us from its registry.
	// The node unpairs regardless of whether this succeeds; the cleared pairing
	// token already makes the parent's next call fail and reconcile us as lost.
	// The notice is signed with the fleet key (which survives unpair) so the parent
	// can trust it. Fired in the background — never block the unpair on the parent.
	if strings.TrimSpace(parentURL) != "" {
		st, serr := a.svc.Status(r.Context())
		key, kerr := a.svc.FleetKey(r.Context())
		if serr == nil && kerr == nil && len(key) > 0 {
			go notifyParentSelfDrop(parentURL, st.NodeID, key)
		}
	}
	controllers.SendResult(w, map[string]any{"paired": false}, "succeed")
}

func (a *pairingApi) adopt(w http.ResponseWriter, r *http.Request) {
	var req fleetnode.AdoptRequest
	if err := decodeJSON(w, r, &req); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	res, err := a.svc.Adopt(r.Context(), req)
	if err != nil {
		switch {
		case errors.Is(err, fleetnode.ErrPairingAlreadyPaired):
			controllers.SendError(w, controllers.ErrConflict, err.Error())
		case errors.Is(err, fleetnode.ErrPairingBadAssertion),
			errors.Is(err, fleetnode.ErrPairingFleetKeyUnset):
			controllers.SendError(w, controllers.ErrPermission, err.Error())
		case errors.Is(err, fleetnode.ErrPairingBadClaimCode):
			controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		default:
			controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		}
		return
	}
	if a.onAdopted != nil {
		a.onAdopted() // kick mTLS certificate enrollment
	}
	controllers.SendResult(w, res, "succeed")
}

func (a *pairingApi) release(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Token string `json:"token"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	if err := a.svc.Release(r.Context(), body.Token); err != nil {
		if errors.Is(err, fleetnode.ErrPairingBadToken) {
			controllers.SendError(w, controllers.ErrPermission, err.Error())
			return
		}
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{"paired": false}, "succeed")
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}

// notifyParentSelfDrop POSTs a best-effort, fleet-key-signed self-drop notice to
// the ex-parent. Short timeout; tolerates the parent's self-signed cert because
// this is only a courtesy — security does not depend on it being delivered.
func notifyParentSelfDrop(parentBaseURL, nodeID string, fleetKey []byte) {
	base := strings.TrimRight(strings.TrimSpace(parentBaseURL), "/")
	if base == "" {
		return
	}
	nonce := strconv.FormatInt(time.Now().UnixNano(), 16)
	ts := time.Now().Unix()
	assertion := pairing.SignAssertion(fleetKey, nodeID, nonce, strconv.FormatInt(ts, 10))
	payload, _ := json.Marshal(map[string]any{
		"nodeId":    nodeID,
		"nonce":     nonce,
		"ts":        ts,
		"assertion": assertion,
	})
	client := &http.Client{
		Timeout:   5 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
	}
	req, err := http.NewRequest(http.MethodPost, base+"/api/nodes/self-dropped", bytes.NewReader(payload))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if resp, err := client.Do(req); err == nil {
		resp.Body.Close()
	}
}
