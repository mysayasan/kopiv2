package apis

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	"github.com/mysayasan/kopiv2/apps/mymatasan/services"
	sharedapis "github.com/mysayasan/kopiv2/domain/shared/apis"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
)

type observationApi struct {
	serv *services.ObservationService
}

// NewObservationApi registers the object-metadata search routes under /observations.
// It backs the "what objects did this camera see" search: query by camera, label, and
// time window, each result linked to the footage segment covering it.
func NewObservationApi(router *mux.Router, serv *services.ObservationService) {
	h := &observationApi{serv: serv}
	g := router.PathPrefix("/observations").Subrouter()

	g.HandleFunc("", h.list).Methods("GET")
	g.HandleFunc("/labels", h.labels).Methods("GET")
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
