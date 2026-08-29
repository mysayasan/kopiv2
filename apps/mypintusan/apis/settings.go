package apis

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/mypintusan/services"
	sharedapis "github.com/mysayasan/kopiv2/domain/shared/apis"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
)

type settingsApi struct {
	settings services.IAccessSettingsService
	audit    *Auditor
}

// NewSettingsApi exposes the runtime settings — the screen that makes this app configurable by a
// facilities manager instead of by somebody editing JSON over SSH.
//
// config.json only ever SEEDS the first boot; from then on these endpoints are the way anything
// changes. That is mymatasan's established pattern, and it is what the "zero prior knowledge"
// product promise actually requires.
func NewSettingsApi(router *mux.Router, settings services.IAccessSettingsService, audit *Auditor) {
	a := &settingsApi{settings: settings, audit: audit}
	g := router.PathPrefix("/settings/access").Subrouter()
	g.HandleFunc("", a.get).Methods("GET")
	g.HandleFunc("", a.save).Methods("PUT")
	g.HandleFunc("/reset", a.reset).Methods("POST")
}

// get returns the live settings. Site keys are redacted by the entity's own marshaller, so this
// handler cannot leak one by forgetting to.
func (a *settingsApi) get(w http.ResponseWriter, r *http.Request) {
	out, err := a.settings.Get(r.Context())
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, out)
}

// save validates and persists an edit.
//
// Admin-only on top of the matrix: these values decide which readers are polled, which are
// encrypted, and how a door behaves. They are not operator settings — a wrong entry here does not
// produce a bad reading, it produces a door that opens for the wrong person or an alarm that never
// comes.
func (a *settingsApi) save(w http.ResponseWriter, r *http.Request) {
	user, ok := sharedapis.LocalUserFromContext(r.Context())
	if !ok || !user.IsAdmin {
		controllers.SendError(w, controllers.ErrLimitedAccess, "administrators only")
		return
	}

	var body services.AccessSettings
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}

	// Read the live values BEFORE the save, so the trail can say what actually changed. "Settings
	// changed" is the least useful entry an audit log can hold — the whole question an
	// investigation asks is which field moved, and a screen that posts the entire object every time
	// makes the request body useless as an answer.
	before, _ := a.settings.Get(r.Context())

	out, err := a.settings.Save(r.Context(), body, user.Id)
	if err != nil {
		// A validation failure is the USER'S problem to fix, and the message says what is wrong in
		// plain language ("two readers at address 1"), because the person reading it is the one who
		// has to go and change a DIP switch.
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	changes, meta := describeSettingsChange(before, out)
	a.audit.Success(r, services.ActionSettingsChange, services.TargetSettings, "access",
		"access settings changed: "+changes, meta)
	controllers.SendResult(w, out)
}

// describeSettingsChange renders what one save actually moved.
//
// Only the values that change how the controller DECIDES are named: the site timezone (every
// schedule and holiday is evaluated in it), the offline flag (whether the site is running from a
// cached replica), the timer cadences, and the shape of the bus. A reader's Secure Channel
// requirement and a rekey are called out individually because they are the two edits on this screen
// that weaken or strengthen the wire itself.
//
// NO KEY MATERIAL, ever. A rekey is recorded as the fact that a key was replaced — the value would
// be readable by every administrator and exported to CSV, which is the same as publishing it.
func describeSettingsChange(before, after services.AccessSettings) (string, map[string]any) {
	meta := map[string]any{}
	var parts []string
	note := func(field string, from, to any) {
		parts = append(parts, fmt.Sprintf("%s %v → %v", field, from, to))
		meta[field] = map[string]any{"from": from, "to": to}
	}
	if before.Timezone != after.Timezone {
		note("timezone", before.Timezone, after.Timezone)
	}
	if before.Offline != after.Offline {
		note("offline", before.Offline, after.Offline)
	}
	if before.TickSeconds != after.TickSeconds {
		note("tickSeconds", before.TickSeconds, after.TickSeconds)
	}
	if before.PINWindowSeconds != after.PINWindowSeconds {
		note("pinWindowSeconds", before.PINWindowSeconds, after.PINWindowSeconds)
	}
	if len(before.Buses) != len(after.Buses) {
		note("buses", len(before.Buses), len(after.Buses))
	}

	// Per-reader, keyed by bus port and PD address rather than by position: a reader removed from
	// the middle of a list would otherwise report every reader after it as changed.
	type readerKey struct {
		port    string
		address int
	}
	index := func(s services.AccessSettings) map[readerKey]services.ReaderSettings {
		out := map[readerKey]services.ReaderSettings{}
		for _, bus := range s.Buses {
			for _, rd := range bus.Readers {
				out[readerKey{bus.Port, rd.Address}] = rd
			}
		}
		return out
	}
	old, now := index(before), index(after)
	for k, rd := range now {
		prev, existed := old[k]
		label := fmt.Sprintf("reader %s@%d", k.port, k.address)
		if !existed {
			parts = append(parts, label+" added")
			continue
		}
		if prev.RequireSecureChannel != rd.RequireSecureChannel {
			note(label+" requireSecureChannel", prev.RequireSecureChannel, rd.RequireSecureChannel)
		}
		if prev.SCBK != rd.SCBK {
			parts = append(parts, label+" rekeyed")
			meta[label+" rekeyed"] = true
		}
	}
	for k := range old {
		if _, still := now[k]; !still {
			parts = append(parts, fmt.Sprintf("reader %s@%d removed", k.port, k.address))
		}
	}

	if len(parts) == 0 {
		// A save that moved nothing is still somebody pressing Save on this screen, and saying so
		// is more honest than an entry that implies a change nobody made.
		return "no values changed", meta
	}
	return strings.Join(parts, "; "), meta
}

// reset restores the config.json seed.
//
// This is the recovery path for an edit that stopped the controller working. Without it, a
// mistyped timezone would need somebody with database access — which on a door appliance sold to a
// facilities team means a site visit.
func (a *settingsApi) reset(w http.ResponseWriter, r *http.Request) {
	user, ok := sharedapis.LocalUserFromContext(r.Context())
	if !ok || !user.IsAdmin {
		controllers.SendError(w, controllers.ErrLimitedAccess, "administrators only")
		return
	}
	before, _ := a.settings.Get(r.Context())
	out, err := a.settings.Reset(r.Context(), user.Id)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	// A reset discards every edit made since install in one press, so the trail records what it
	// undid rather than the bare fact that it happened. Without that, the entries describing the
	// changes are still there and nothing says they stopped being true.
	changes, meta := describeSettingsChange(before, out)
	a.audit.Success(r, services.ActionSettingsReset, services.TargetSettings, "access",
		"access settings reset to the config.json seed: "+changes, meta)
	controllers.SendResult(w, out)
}
