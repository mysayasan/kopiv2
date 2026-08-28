// Package apis is mypintusan's HTTP surface.
//
// It is deliberately small at this stage: doors and their live state, people and their badges, the
// access log, and lockdown. Everything here is authorised by the shared accessrbac matrix declared
// in services.Policy(), which is deny-by-default — a route that is not in that catalog is a route
// nobody can see they are not granting.
package apis

import (
	"context"
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

// Unlocker is the runtime seam the API uses to open a door. It is an interface so the HTTP layer
// cannot reach the actuator directly — every unlock has to pass through the controller's audited
// chokepoint.
type Unlocker interface {
	Unlock(ctx context.Context, door entities.Door, actor int64, actorName string) error
	SetLockdown(ctx context.Context, on bool)
	Lockdown() bool
}

type doorApi struct {
	store  *services.SQLStore
	rt     Unlocker
	doors  dbsql.IGenericRepo[entities.Door]
	reader dbsql.IGenericRepo[entities.Reader]
}

// NewDoorApi registers the door surface.
func NewDoorApi(router *mux.Router, store *services.SQLStore, rt Unlocker, db dbsql.IDbCrud) {
	a := &doorApi{
		store:  store,
		rt:     rt,
		doors:  dbsql.NewGenericRepo[entities.Door](db),
		reader: dbsql.NewGenericRepo[entities.Reader](db),
	}

	g := router.PathPrefix("/doors").Subrouter()
	g.HandleFunc("", a.list).Methods("GET")
	g.HandleFunc("", a.create).Methods("POST")
	g.HandleFunc("/{id:[0-9]+}", a.get).Methods("GET")
	// The unlock path is its OWN matrix entry, separate from /api/doors, because seeing a door and
	// opening it are different powers held by different people.
	g.HandleFunc("/{id:[0-9]+}/unlock", a.unlock).Methods("POST")

	r := router.PathPrefix("/readers").Subrouter()
	r.HandleFunc("", a.listReaders).Methods("GET")
}

func (a *doorApi) list(w http.ResponseWriter, r *http.Request) {
	rows, total, err := a.doors.Get(r.Context(), "", 500, 0, nil,
		[]sqldataenums.Sorter{{FieldName: "Name", Sort: sqldataenums.ASC}})
	if err != nil {
		controllers.SendError(w, controllers.ErrNotFound, err.Error())
		return
	}
	controllers.SendPagingResult(w, rows, uint64(len(rows)), 0, total)
}

func (a *doorApi) get(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, "bad door id")
		return
	}
	door, err := a.store.Door(r.Context(), id)
	if err != nil {
		controllers.SendError(w, controllers.ErrNotFound, err.Error())
		return
	}
	if door == nil {
		controllers.SendError(w, controllers.ErrNotFound, "no such door")
		return
	}
	controllers.SendResult(w, door)
}

// createDoorRequest is a door and the reader that serves it, created together.
//
// They are ONE call on purpose. A door with no reader is inert — nothing can badge at it — and a
// reader with no door drives nothing. Creating them separately invites a half-configured state that
// looks fine in two list screens and does nothing at the wall, which is exactly the confusion this
// product is meant to spare a non-technical installer.
type createDoorRequest struct {
	Name          string `json:"name"`
	Class         string `json:"class"`
	UnlockSeconds int    `json:"unlockSeconds"`
	// SiteId places the door on myseliasan's site tree, and it is accepted here because the HOLIDAY
	// CALENDAR reads it. `Holiday.SiteId` scopes a calendar to one site — the entity's own comment
	// explains why ("Malaysian public holidays vary BY STATE … a site with offices in two states
	// needs two"), `HolidayOn` implements the precedence and `store_sql_test.go` tests it with
	// SiteId 5. But no request shape ever carried a site onto a door, so every door on every
	// install was at site 0, `HolidayOn`'s site branch could not match, and a site-scoped holiday
	// closed nothing anywhere. Measured live: a deny holiday scoped to the door's own site let the
	// badge straight through. Same shape as the offline policy before it, and it matters for the
	// same reason — there is no PUT /api/doors, so a door is born with its placement for good.
	SiteId int64 `json:"siteId"`
	// BusPort and OsdpAddress place the entry reader on a cable.
	BusPort     string `json:"busPort"`
	OsdpAddress int    `json:"osdpAddress"`
	ReaderName  string `json:"readerName"`
	// RequireSecureChannel is the door's policy, not the reader's capability. A POINTER so that
	// "the caller said false" is distinguishable from "the caller said nothing" — the difference
	// between an explicit escape hatch and an omission that must inherit the class default.
	RequireSecureChannel *bool `json:"requireSecureChannel"`
	// OfflinePolicy and OfflineTTLSeconds are the door's behaviour on a controller running from a
	// cached replica — `Decide()`'s GATE 10, and the table in docs/MYPINTUSAN_DATA_MODEL.md §2.
	//
	// They are accepted HERE because there is no PUT /api/doors: a door is created once and keeps
	// whatever policy it was born with. Before this, neither field appeared on any request shape at
	// all, so `OfflinePolicy` was hardcoded to `cached` on every door on every install — the `deny`
	// policy was a constant nothing could ever store — and `OfflineTTLSeconds` was always 0, which
	// means the class default of 8, 24 or 72 HOURS. Measured on a running appliance: a door 20
	// seconds past a 2-second TTL still granted, because no shipped door had a TTL to exceed.
	//
	// OfflinePolicy is cached | deny. Anything else is REFUSED rather than coerced: the value an
	// installer is most likely to invent is some spelling of "allow", and silently storing `cached`
	// for it would leave them believing they had configured a door that fails open. There is no
	// fail-open policy in this product, and the API should say so out loud.
	OfflinePolicy     string `json:"offlinePolicy"`
	OfflineTTLSeconds int    `json:"offlineTtlSeconds"`
	// RelayChannel is the output on the reader that fires the strike.
	RelayChannel int `json:"relayChannel"`
	// ContactDeviceKey binds a door-position contact. Empty means forced-open and held-open
	// cannot be detected — a capability gap the UI surfaces rather than hiding.
	ContactDeviceKey string `json:"contactDeviceKey"`
	HeldOpenSeconds  int    `json:"heldOpenSeconds"`
}

// create adds a door and its entry reader.
//
// Admin-only on top of the matrix: door hardware bindings decide which relay fires and which
// contact is believed. A wrong value here does not produce a bad reading, it produces a door that
// opens for the wrong person or an alarm that never comes.
func (a *doorApi) create(w http.ResponseWriter, r *http.Request) {
	user, ok := sharedapis.LocalUserFromContext(r.Context())
	if !ok || !user.IsAdmin {
		controllers.SendError(w, controllers.ErrLimitedAccess, "administrators only")
		return
	}

	var body createDoorRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	body.BusPort = strings.TrimSpace(body.BusPort)
	if body.Name == "" {
		controllers.SendError(w, controllers.ErrBadRequest, "the door needs a name")
		return
	}
	if body.BusPort == "" {
		controllers.SendError(w, controllers.ErrBadRequest, "the door needs a reader cable")
		return
	}
	if body.OsdpAddress < 0 || body.OsdpAddress > 0x7F {
		controllers.SendError(w, controllers.ErrBadRequest, "the reader address must be between 0 and 127")
		return
	}
	switch body.Class {
	case entities.ClassInterior, entities.ClassPerimeter, entities.ClassCritical:
	default:
		body.Class = entities.ClassInterior
	}

	ctx := r.Context()

	// Refuse a reader address already in use on that cable. Two readers at one address is the
	// out-of-box collision — they ship set to 0 — and catching it here is far kinder than letting
	// the installer discover it as a door that never responds.
	if existing, err := a.store.ReaderByBus(ctx, body.BusPort, body.OsdpAddress); err != nil {
		controllers.SendError(w, controllers.ErrConflict, err.Error())
		return
	} else if existing != nil {
		controllers.SendError(w, controllers.ErrConflict,
			"another reader is already at that address on this cable; give this one its own address")
		return
	}

	requireSC := entities.SecureChannelDefault(body.Class)
	if body.RequireSecureChannel != nil {
		requireSC = *body.RequireSecureChannel
	}

	offlinePolicy := strings.TrimSpace(body.OfflinePolicy)
	if offlinePolicy == "" {
		offlinePolicy = entities.OfflineCached
	}
	if offlinePolicy != entities.OfflineCached && offlinePolicy != entities.OfflineDeny {
		controllers.SendError(w, controllers.ErrBadRequest,
			"the offline policy must be \"cached\" or \"deny\"; there is no fail-open policy — "+
				"a door that opens because the controller lost its source of truth is the attack, "+
				"not the feature")
		return
	}
	if body.OfflineTTLSeconds < 0 {
		controllers.SendError(w, controllers.ErrBadRequest,
			"the offline cache TTL cannot be negative")
		return
	}

	now := time.Now().Unix()
	if body.SiteId < 0 {
		controllers.SendError(w, controllers.ErrBadRequest, "the site id cannot be negative")
		return
	}

	door := entities.Door{
		Name: body.Name, Class: body.Class,
		// 0 is "no site", and it is the right default for the single-site appliance this product
		// mostly ships as: such a door follows the global holiday calendar.
		SiteId:        body.SiteId,
		LockKind:      entities.LockFailSecure,
		UnlockSeconds: orDefault(body.UnlockSeconds, 5),
		// The accessibility extension defaults to roughly triple the normal time; an operator can
		// tune it per door later.
		ExtendedUnlockSeconds: orDefault(body.UnlockSeconds, 5) * 3,
		HeldOpenSeconds:       orDefault(body.HeldOpenSeconds, 30),
		RelayChannel:          body.RelayChannel,
		ContactDeviceKey:      strings.TrimSpace(body.ContactDeviceKey),
		// Omitted means the class decides — see entities.SecureChannelDefault. Its neighbours
		// here have always defaulted (UnlockSeconds, HeldOpenSeconds, OfflinePolicy); this is the
		// one SECURITY-relevant field that did not, and there is no PUT /api/doors, so a door
		// created with the wrong policy kept it for good.
		RequireSecureChannel: requireSC,
		OfflinePolicy:        offlinePolicy,
		// 0 means "use the class default" — see entities.Door.DefaultOfflineTTLSeconds.
		OfflineTTLSeconds: body.OfflineTTLSeconds,
		AntiPassback:      entities.APBOff,
		Enabled:           true,
		CreatedBy:         user.Id, CreatedAt: now, UpdatedBy: user.Id, UpdatedAt: now,
	}
	doorId, err := a.doors.Create(ctx, "", door)
	if err != nil {
		controllers.SendError(w, controllers.ErrConflict, err.Error())
		return
	}
	door.Id = int64(doorId)

	readerName := strings.TrimSpace(body.ReaderName)
	if readerName == "" {
		readerName = body.Name
	}
	readerId, err := a.reader.Create(ctx, "", entities.Reader{
		Name: readerName, DoorId: door.Id, Direction: entities.DirectionIn,
		BusPort: body.BusPort, OsdpAddress: body.OsdpAddress,
		ScbkState: entities.ScbkDefault, TamperState: entities.TamperOK, Enabled: true,
		CreatedBy: user.Id, CreatedAt: now, UpdatedBy: user.Id, UpdatedAt: now,
	})
	if err != nil {
		// Roll the door back rather than leaving one that can never be opened. A half-created door
		// would sit in the list looking real and refuse every badge, with nothing explaining why.
		_, _ = a.doors.DeleteById(ctx, "", uint64(door.Id))
		controllers.SendError(w, controllers.ErrConflict, "could not create the reader: "+err.Error())
		return
	}

	// Point the door at its reader. This is what StrikeFor resolves to find the PD address, so a
	// door whose ReaderInId is 0 would grant and then fail to open.
	door.ReaderInId = int64(readerId)
	if _, err := a.doors.UpdateById(ctx, "", door); err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError,
			"the door and reader were created but could not be linked: "+err.Error())
		return
	}

	controllers.SendResult(w, door)
}

func orDefault(v, def int) int {
	if v > 0 {
		return v
	}
	return def
}

func (a *doorApi) listReaders(w http.ResponseWriter, r *http.Request) {
	rows, total, err := a.reader.Get(r.Context(), "", 500, 0, nil,
		[]sqldataenums.Sorter{{FieldName: "Name", Sort: sqldataenums.ASC}})
	if err != nil {
		controllers.SendError(w, controllers.ErrNotFound, err.Error())
		return
	}
	controllers.SendPagingResult(w, rows, uint64(len(rows)), 0, total)
}

// unlock opens a door remotely.
//
// The actor is taken from the SESSION, never from the request body. A remote unlock with an
// attacker-supplied actor name would put a chosen name in the audit log next to a door opening,
// which is worse than no name at all — it is a forged record.
func (a *doorApi) unlock(w http.ResponseWriter, r *http.Request) {
	user, ok := sharedapis.LocalUserFromContext(r.Context())
	if !ok {
		controllers.SendError(w, controllers.ErrLimitedAccess, "sign in first")
		return
	}
	id, err := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, "bad door id")
		return
	}

	door, err := a.store.Door(r.Context(), id)
	if err != nil {
		controllers.SendError(w, controllers.ErrNotFound, err.Error())
		return
	}
	if door == nil {
		controllers.SendError(w, controllers.ErrNotFound, "no such door")
		return
	}

	if err := a.rt.Unlock(r.Context(), *door, user.Id, user.Username); err != nil {
		// The refusal is already in the access log by the time this returns — the controller
		// records it either way, so a denied remote unlock is as auditable as a granted one.
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{"doorId": door.Id, "unlocked": true})
}

type lockdownApi struct {
	rt Unlocker
}

// NewLockdownApi registers the site-seal control.
func NewLockdownApi(router *mux.Router, rt Unlocker) {
	a := &lockdownApi{rt: rt}
	g := router.PathPrefix("/lockdown").Subrouter()
	g.HandleFunc("", a.state).Methods("GET")
	g.HandleFunc("", a.set).Methods("POST")
}

func (a *lockdownApi) state(w http.ResponseWriter, r *http.Request) {
	controllers.SendResult(w, map[string]any{"lockdown": a.rt.Lockdown()})
}

// set seals or releases the site.
//
// Admin-only on top of the matrix. Lockdown is the one control that stops a building working: it
// cannot trap anybody, because egress is hardware and no software here can override it, but an
// operator who triggers it during a fire drill has turned a nuisance into an incident.
func (a *lockdownApi) set(w http.ResponseWriter, r *http.Request) {
	user, ok := sharedapis.LocalUserFromContext(r.Context())
	if !ok || !user.IsAdmin {
		controllers.SendError(w, controllers.ErrLimitedAccess, "administrators only")
		return
	}
	var body struct {
		Lockdown bool `json:"lockdown"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, "expected {\"lockdown\": true|false}")
		return
	}
	a.rt.SetLockdown(r.Context(), body.Lockdown)
	controllers.SendResult(w, map[string]any{"lockdown": a.rt.Lockdown()})
}
