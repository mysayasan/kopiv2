package apis

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	"github.com/mysayasan/kopiv2/apps/mymatasan/services"
	sharedapis "github.com/mysayasan/kopiv2/domain/shared/apis"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
)

type observationApi struct {
	serv   *services.ObservationService
	search *services.SightingSearch
}

// NewObservationApi registers the object-metadata search routes under /observations.
// It backs the "what objects did this camera see" search: query by camera, label, and
// time window, each result linked to the footage segment covering it.
//
// /search is the FEDERATION entry point: the control plane calls it over the control
// channel, once per node, to answer a fleet-wide search. It lives under /observations
// rather than a namespace of its own so the existing Objects page grant governs it — a
// role that may read this node's object metadata may read it through the tunnel too, and
// no role gains reach because a fleet feature shipped.
func NewObservationApi(router *mux.Router, serv *services.ObservationService, search *services.SightingSearch) {
	h := &observationApi{serv: serv, search: search}
	g := router.PathPrefix("/observations").Subrouter()

	g.HandleFunc("", h.list).Methods("GET")
	g.HandleFunc("/labels", h.labels).Methods("GET")
	g.HandleFunc("/search", h.searchObjects).Methods("GET")
}

// searchObjects answers one node's share of a federated object search.
func (a *observationApi) searchObjects(w http.ResponseWriter, r *http.Request) {
	if a.search == nil {
		controllers.SendError(w, controllers.ErrInternalServerError, "search is unavailable")
		return
	}
	page, err := a.search.SearchObjects(r.Context(), sightingQueryFromRequest(r))
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, page, "succeed")
}

// sightingQueryFromRequest parses the federated-search query string. It is shared by the
// object and identity endpoints so the two halves of one fleet search can never disagree
// about which window or confidence floor they were asked for.
func sightingQueryFromRequest(r *http.Request) services.SightingQuery {
	q := services.SightingQuery{
		From:          parseInt64Query(r, "from"),
		To:            parseInt64Query(r, "to"),
		Labels:        splitCSVQuery(r, "labels"),
		IdentityKinds: splitCSVQuery(r, "identityKinds"),
		Text:          strings.TrimSpace(r.URL.Query().Get("text")),
		Limit:         int(parseInt64Query(r, "limit")),
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("minConfidence")); raw != "" {
		if v, err := strconv.ParseFloat(raw, 64); err == nil {
			q.MinConfidence = v
		}
	}
	return q
}

// splitCSVQuery reads a comma-separated repeatable query parameter into a slice.
func splitCSVQuery(r *http.Request, key string) []string {
	out := []string{}
	for _, raw := range r.URL.Query()[key] {
		for _, part := range strings.Split(raw, ",") {
			if part = strings.TrimSpace(part); part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}

func (a *observationApi) list(w http.ResponseWriter, r *http.Request) {
	limit, offset := readPaging(r)
	cameraId := parseInt64Query(r, "cameraId")
	// The grid drives filtering + sorting server-side: `filters`/`sorters` query params
	// (DataTable format) are validated against ObjectObservation's fields, so paging
	// runs over the true filtered set rather than a client-side slice.
	opts, err := sharedapis.ParseListQueryOptions[entities.ObjectObservation](r)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	items, total, err := a.serv.GetObservations(r.Context(), limit, offset, cameraId, opts.Filters, opts.Sorters)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{
		"items": items,
		"total": total,
	}, "succeed")
}

func (a *observationApi) labels(w http.ResponseWriter, r *http.Request) {
	cameraId := parseInt64Query(r, "cameraId")
	labels, err := a.serv.Labels(r.Context(), cameraId)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, labels, "succeed")
}
