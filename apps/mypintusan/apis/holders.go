package apis

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/mypintusan/entities"
	"github.com/mysayasan/kopiv2/apps/mypintusan/services"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	sharedapis "github.com/mysayasan/kopiv2/domain/shared/apis"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

type holderApi struct {
	holders dbsql.IGenericRepo[entities.Holder]
	creds   dbsql.IGenericRepo[entities.Credential]
}

// NewHolderApi registers people and their badges — the operator's daily surface.
func NewHolderApi(router *mux.Router, db dbsql.IDbCrud) {
	a := &holderApi{
		holders: dbsql.NewGenericRepo[entities.Holder](db),
		creds:   dbsql.NewGenericRepo[entities.Credential](db),
	}
	g := router.PathPrefix("/holders").Subrouter()
	g.HandleFunc("", a.list).Methods("GET")
	g.HandleFunc("", a.create).Methods("POST")
	g.HandleFunc("/{id:[0-9]+}", a.get).Methods("GET")
	g.HandleFunc("/{id:[0-9]+}/credentials", a.listCredentials).Methods("GET")
	g.HandleFunc("/{id:[0-9]+}/credentials", a.issueCredential).Methods("POST")
	g.HandleFunc("/{id:[0-9]+}/credentials/{credId:[0-9]+}/revoke", a.revokeCredential).Methods("POST")
}

func (a *holderApi) list(w http.ResponseWriter, r *http.Request) {
	rows, total, err := a.holders.Get(r.Context(), "", 500, 0, nil,
		[]sqldataenums.Sorter{{FieldName: "Name", Sort: sqldataenums.ASC}})
	if err != nil {
		controllers.SendError(w, controllers.ErrNotFound, err.Error())
		return
	}
	controllers.SendPagingResult(w, rows, uint64(len(rows)), 0, total)
}

func (a *holderApi) get(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, "bad holder id")
		return
	}
	holder, err := a.holders.GetById(r.Context(), "", uint64(id))
	if err != nil || holder == nil {
		controllers.SendError(w, controllers.ErrNotFound, "no such holder")
		return
	}
	controllers.SendResult(w, holder)
}

func (a *holderApi) create(w http.ResponseWriter, r *http.Request) {
	user, ok := sharedapis.LocalUserFromContext(r.Context())
	if !ok {
		controllers.SendError(w, controllers.ErrLimitedAccess, "sign in first")
		return
	}
	var body entities.Holder
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	if strings.TrimSpace(body.Ref) == "" || strings.TrimSpace(body.Name) == "" {
		controllers.SendError(w, controllers.ErrBadRequest, "ref and name are required")
		return
	}
	if body.Kind == "" {
		body.Kind = entities.HolderStaff
	}
	// A new holder starts ACTIVE but reaches nothing: access comes from group membership, and this
	// creates none. That is the fail-closed default — a person exists before they are permitted.
	body.Status = entities.HolderActive
	// SsoUserId is deliberately NOT taken from the request body here. Linking a badge to an
	// identity is a privileged operation, and accepting an id from whoever is creating a visitor
	// record would let a badge be bound to somebody else's account.
	body.SsoUserId = 0
	now := time.Now().Unix()
	body.CreatedBy, body.CreatedAt = user.Id, now
	body.UpdatedBy, body.UpdatedAt = user.Id, now

	id, err := a.holders.Create(r.Context(), "", body)
	if err != nil {
		controllers.SendError(w, controllers.ErrConflict, err.Error())
		return
	}
	body.Id = int64(id)
	controllers.SendResult(w, body)
}

func (a *holderApi) listCredentials(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, "bad holder id")
		return
	}
	// Get with an explicit filter, never GetByForeign — that path hardcodes limit=1 and would show
	// a holder only their FIRST badge, which is exactly the kind of quiet truncation that makes an
	// operator think a card was never issued.
	rows, total, err := a.creds.Get(r.Context(), "", 200, 0,
		[]sqldataenums.Filter{{FieldName: "HolderId", Compare: sqldataenums.Equal, Value: id}}, nil)
	if err != nil {
		controllers.SendError(w, controllers.ErrNotFound, err.Error())
		return
	}
	controllers.SendPagingResult(w, rows, uint64(len(rows)), 0, total)
}

// issueCredential enrols a badge or a PIN for a holder.
func (a *holderApi) issueCredential(w http.ResponseWriter, r *http.Request) {
	user, ok := sharedapis.LocalUserFromContext(r.Context())
	if !ok {
		controllers.SendError(w, controllers.ErrLimitedAccess, "sign in first")
		return
	}
	holderId, err := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, "bad holder id")
		return
	}
	holder, err := a.holders.GetById(r.Context(), "", uint64(holderId))
	if err != nil || holder == nil {
		controllers.SendError(w, controllers.ErrNotFound, "no such holder")
		return
	}

	var body struct {
		Kind         string `json:"kind"`
		Format       string `json:"format"`
		FacilityCode int    `json:"facilityCode"`
		CardNumber   string `json:"cardNumber"`
		PIN          string `json:"pin"`
		DuressPIN    string `json:"duressPin"`
		ValidFrom    int64  `json:"validFrom"`
		ValidUntil   int64  `json:"validUntil"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}

	cred := entities.Credential{
		HolderId: holderId, Kind: body.Kind, Format: body.Format,
		FacilityCode: body.FacilityCode, CardNumber: strings.TrimSpace(body.CardNumber),
		Status: entities.CredActive, ValidFrom: body.ValidFrom, ValidUntil: body.ValidUntil,
		IssuedBy: user.Id, IssuedAt: time.Now().Unix(),
	}
	if cred.Kind == "" {
		cred.Kind = entities.CredCard
	}

	switch cred.Kind {
	case entities.CredCard:
		if cred.Format == "" || cred.CardNumber == "" {
			controllers.SendError(w, controllers.ErrBadRequest,
				"a card needs a format and a number; the facility code is part of the match key")
			return
		}
	case entities.CredPin:
		if body.PIN == "" {
			controllers.SendError(w, controllers.ErrBadRequest, "a PIN credential needs a pin")
			return
		}
	}

	// PINs are hashed here and the plaintext never leaves this function. The entity's PinHash and
	// DuressPinHash carry `json:"-"`, so they cannot travel back out in a response either.
	if body.PIN != "" {
		h, err := services.HashPIN(body.PIN)
		if err != nil {
			controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
			return
		}
		cred.PinHash = h
	}
	if body.DuressPIN != "" {
		if body.DuressPIN == body.PIN {
			// A duress PIN identical to the normal one silently disables duress: every entry would
			// raise a silent alarm, so the site would turn it off within a day.
			controllers.SendError(w, controllers.ErrBadRequest,
				"the duress PIN must differ from the normal PIN")
			return
		}
		h, err := services.HashPIN(body.DuressPIN)
		if err != nil {
			controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
			return
		}
		cred.DuressPinHash = h
	}

	id, err := a.creds.Create(r.Context(), "", cred)
	if err != nil {
		controllers.SendError(w, controllers.ErrConflict, err.Error())
		return
	}
	cred.Id = int64(id)
	controllers.SendResult(w, cred)
}

// revokeCredential kills a badge.
//
// It UPDATES rather than deletes, on purpose: the access log references the credential id, and
// deleting the row would orphan every historical decision that mentions it. A revoked badge must
// still be explicable years later.
func (a *holderApi) revokeCredential(w http.ResponseWriter, r *http.Request) {
	user, ok := sharedapis.LocalUserFromContext(r.Context())
	if !ok {
		controllers.SendError(w, controllers.ErrLimitedAccess, "sign in first")
		return
	}
	credId, err := strconv.ParseInt(mux.Vars(r)["credId"], 10, 64)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, "bad credential id")
		return
	}
	cred, err := a.creds.GetById(r.Context(), "", uint64(credId))
	if err != nil || cred == nil {
		controllers.SendError(w, controllers.ErrNotFound, "no such credential")
		return
	}

	var body struct {
		Status string `json:"status"`
		Reason string `json:"reason"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	// lost and stolen are kept distinct from a plain revocation: a card presented after being
	// reported stolen is an incident worth surfacing, not merely a denial.
	switch body.Status {
	case entities.CredLost, entities.CredStolen, entities.CredSuspended:
		cred.Status = body.Status
	default:
		cred.Status = entities.CredRevoked
	}
	cred.RevokedBy = user.Id
	cred.RevokedAt = time.Now().Unix()
	cred.RevokeReason = strings.TrimSpace(body.Reason)

	if _, err := a.creds.UpdateById(r.Context(), "", *cred); err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, cred)
}
