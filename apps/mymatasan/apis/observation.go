package apis

import (
	"errors"
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
	serv       *services.ObservationService
	search     *services.SightingSearch
	appearance *services.AppearanceService
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
func NewObservationApi(router *mux.Router, serv *services.ObservationService, search *services.SightingSearch, appearance *services.AppearanceService) {
	h := &observationApi{serv: serv, search: search, appearance: appearance}
	g := router.PathPrefix("/observations").Subrouter()

	g.HandleFunc("", h.list).Methods("GET")
	g.HandleFunc("/labels", h.labels).Methods("GET")
	g.HandleFunc("/search", h.searchObjects).Methods("GET")
	// Appearance search sits under /observations for the same reason /search does: it
	// ranks the very rows the Objects page grant already governs, so no administrator has
	// to notice a new path for an operator to keep the reach they had. It is also the
	// second federation entry point — the control plane calls it per node over the tunnel.
	g.HandleFunc("/appearance", h.appearanceSearch).Methods("GET")
	// The query vector for one sighting. The control plane fetches it from the node the
	// operator was watching and hands it to every other node, because a sighting id means
	// nothing anywhere else. It is a read of data this same grant already exposes in
	// ranked form, so it adds no reach — only a way to ask the question elsewhere.
	g.HandleFunc("/appearance/vector", h.appearanceVector).Methods("GET")
}

// appearanceVector returns one sighting's descriptor in the URL-safe wire form, so a
// federated search can carry the question to nodes that have never heard of the sighting.
func (a *observationApi) appearanceVector(w http.ResponseWriter, r *http.Request) {
	if a.appearance == nil {
		controllers.SendError(w, controllers.ErrInternalServerError, "appearance search is unavailable")
		return
	}
	obsId := parseInt64Query(r, "observationId")
	if obsId <= 0 {
		controllers.SendError(w, controllers.ErrBadRequest, "observationId is required")
		return
	}
	vec, model, label, err := a.appearance.VectorFor(r.Context(), obsId)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	if len(vec) == 0 {
		controllers.SendError(w, controllers.ErrBadRequest,
			"that sighting has no appearance description — appearance search was not enabled on this camera when it was recorded")
		return
	}
	controllers.SendResult(w, map[string]any{
		"observationId": obsId,
		"vector":        services.EncodeAppearanceVectorParam(vec),
		"model":         model,
		"label":         label,
		"dim":           len(vec),
	}, "succeed")
}

// appearanceSearch ranks this node's recorded sightings against one sighting's appearance.
//
//	GET /api/observations/appearance?observationId=&from=&to=&cameraId=&limit=&minSimilarity=
//
// The query is named by an OBSERVATION rather than an uploaded photograph, and that is a
// deliberate limit rather than an unfinished one. Ranking the estate against a picture
// somebody supplies is a different feature with a different risk profile — it turns a
// review tool into a watchlist — and the workflow this exists for starts on the screen:
// an operator watches someone leave shot and asks where else they went.
//
// When the control plane fans this out, it passes the query VECTOR instead (see
// vectorParam), because the sighting being searched for lives on whichever node the
// operator was watching and means nothing to the others.
func (a *observationApi) appearanceSearch(w http.ResponseWriter, r *http.Request) {
	if a.appearance == nil {
		controllers.SendError(w, controllers.ErrInternalServerError, "appearance search is unavailable")
		return
	}
	q := services.AppearanceQuery{
		From:          parseInt64Query(r, "from"),
		To:            parseInt64Query(r, "to"),
		Label:         strings.TrimSpace(r.URL.Query().Get("label")),
		Model:         strings.TrimSpace(r.URL.Query().Get("model")),
		MinSimilarity: parseFloatQuery(r, "minSimilarity"),
		Limit:         int(parseInt64Query(r, "limit")),
	}
	for _, raw := range r.URL.Query()["cameraId"] {
		for _, part := range strings.Split(raw, ",") {
			if id, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64); err == nil && id > 0 {
				q.CameraIds = append(q.CameraIds, id)
			}
		}
	}

	// Either the caller names a local sighting (the operator's own node) or hands over a
	// vector (the control plane, fanning the same question out to nodes that have never
	// heard of that sighting).
	if obsId := parseInt64Query(r, "observationId"); obsId > 0 {
		vec, model, label, err := a.appearance.VectorFor(r.Context(), obsId)
		if err != nil {
			controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
			return
		}
		if len(vec) == 0 {
			// Distinguished from "no matches" on purpose: this sighting was never
			// described, which is a property of how the camera was configured when it was
			// recorded, not of what else is out there. An operator told "no results" would
			// conclude the person appeared nowhere else.
			controllers.SendError(w, controllers.ErrBadRequest,
				"that sighting has no appearance description — appearance search was not enabled on this camera when it was recorded")
			return
		}
		q.Vector, q.Model, q.ExcludeObservationId = vec, model, obsId
		if q.Label == "" {
			q.Label = label
		}
	} else if raw := strings.TrimSpace(r.URL.Query().Get("vector")); raw != "" {
		vec, err := services.DecodeAppearanceVectorParam(raw)
		if err != nil {
			controllers.SendError(w, controllers.ErrBadRequest, err.Error())
			return
		}
		q.Vector = vec
		// The control plane fans one node's sighting out to every node INCLUDING the one it
		// came from — that node holds the rest of the site's footage and is usually where
		// the next hit is. It passes the id separately so that node alone drops the
		// operator's own pick, which would otherwise return as its own best match at 1.00.
		q.ExcludeObservationId = parseInt64Query(r, "excludeObservationId")
	} else {
		controllers.SendError(w, controllers.ErrBadRequest, "observationId or vector is required")
		return
	}
	if strings.TrimSpace(q.Label) == "" {
		controllers.SendError(w, controllers.ErrBadRequest, "label is required")
		return
	}
	if strings.TrimSpace(q.Model) == "" {
		controllers.SendError(w, controllers.ErrBadRequest, "model is required")
		return
	}

	res, err := a.appearance.Search(r.Context(), q)
	if err != nil {
		// A window the caller can narrow is a 400, not a 500 — the request is answerable,
		// just not this wide.
		if errors.Is(err, services.ErrAppearanceRangeTooWide) {
			controllers.SendError(w, controllers.ErrBadRequest, err.Error())
			return
		}
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, res, "succeed")
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
