package apis

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/myseliasan/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
)

type fleetSearchApi struct {
	search  *services.FleetSearchService
	session *middlewares.AccessSessionMidware
	audit   services.IAuditService
}

// NewFleetSearchApi registers federated cross-node search (W2-4):
//
//	GET /api/nodes/search         — sightings across every node the caller can reach
//	GET /api/nodes/search/labels  — the fleet-wide union of recorded object labels
//
// IT LIVES UNDER /api/nodes ON PURPOSE. The permission matrix matches by path prefix, so a
// role already granted GET on /api/nodes — which every role that can use the fleet screens
// has, and which the browser-side search this replaces already relied on — can search
// without an administrator having to notice a new path and grant it. A new top-level prefix
// would have silently taken the feature away from every non-superadmin the day it shipped.
//
// The real authorization is not the prefix anyway: it is resolved per node inside the
// service, from the same per-node access grants the tunnel uses, and enforced a second time
// by the node itself against its own matrix.
func NewFleetSearchApi(router *mux.Router, auth middlewares.AuthMidware, session *middlewares.AccessSessionMidware, search *services.FleetSearchService, audit services.IAuditService) {
	h := &fleetSearchApi{search: search, session: session, audit: audit}
	g := router.PathPrefix("/nodes").Subrouter()
	g.Use(auth.Middleware)
	g.HandleFunc("/search", h.runSearch).Methods("GET")
	g.HandleFunc("/search/labels", h.labels).Methods("GET")
	// Federated appearance search (W3-2). Same prefix and the same per-node authorization
	// as /search, because it is the same question asked a different way — "where else did
	// this go?" rather than "what was seen?" — over the same index and the same grants.
	g.HandleFunc("/search/appearance", h.appearanceSearch).Methods("GET")
}

func (a *fleetSearchApi) runSearch(w http.ResponseWriter, r *http.Request) {
	if a.search == nil {
		controllers.SendError(w, controllers.ErrInternalServerError, "fleet search is unavailable")
		return
	}
	roleId := a.roleId(r)
	q := fleetSearchQueryFromRequest(r)
	result, err := a.search.Search(r.Context(), roleId, q)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	a.recordSearch(r, q, result)
	controllers.SendResult(w, result, "succeed")
}

// appearanceSearch answers "where else in the estate does this sighting appear?".
//
//	GET /api/nodes/search/appearance?nodeId=<source>&observationId=<n>&from=&to=&siteId=&minSimilarity=&limit=
//
// nodeId names the recorder holding the sighting the operator picked; the search itself
// still visits every node the caller can reach unless scopeNodeId narrows it.
func (a *fleetSearchApi) appearanceSearch(w http.ResponseWriter, r *http.Request) {
	if a.search == nil {
		controllers.SendError(w, controllers.ErrInternalServerError, "fleet search is unavailable")
		return
	}
	q := services.FleetAppearanceQuery{
		SourceNodeId:        strings.TrimSpace(r.URL.Query().Get("nodeId")),
		SourceObservationId: parseFleetInt64(r, "observationId"),
		From:                parseFleetInt64(r, "from"),
		To:                  parseFleetInt64(r, "to"),
		SiteId:              parseFleetInt64(r, "siteId"),
		NodeId:              strings.TrimSpace(r.URL.Query().Get("scopeNodeId")),
		MinStandout:         parseFleetFloat(r, "minStandout"),
		Limit:               int(parseFleetInt64(r, "limit")),
	}
	result, err := a.search.AppearanceSearch(r.Context(), a.roleId(r), q)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	a.recordAppearanceSearch(r, q, result)
	controllers.SendResult(w, result, "succeed")
}

func parseFleetInt64(r *http.Request, key string) int64 {
	v, _ := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get(key)), 10, 64)
	return v
}

func parseFleetFloat(r *http.Request, key string) float64 {
	v, _ := strconv.ParseFloat(strings.TrimSpace(r.URL.Query().Get(key)), 64)
	return v
}

func (a *fleetSearchApi) labels(w http.ResponseWriter, r *http.Request) {
	if a.search == nil {
		controllers.SendError(w, controllers.ErrInternalServerError, "fleet search is unavailable")
		return
	}
	result, err := a.search.Labels(r.Context(), a.roleId(r), fleetSearchQueryFromRequest(r))
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, result, "succeed")
}

// roleId resolves the requester's LIVE role, so a just-demoted operator loses node reach
// without needing to sign in again — the same rule the node proxy applies.
func (a *fleetSearchApi) roleId(r *http.Request) int64 {
	roleId, _ := operatorIdentity(r)
	if a.session != nil {
		if p, err := a.session.CurrentPrincipal(r); err == nil && p != nil {
			roleId = p.RoleId
		}
	}
	return roleId
}

// recordSearch audits a fleet-wide search.
//
// Tunneled READS are deliberately not audited elsewhere — auditing every page load would
// drown the trail. This one is different: it is an estate-wide sweep for a named person or
// a specific vehicle, across every site at once, and "who searched for this plate, and
// when" is precisely the question a data-protection review asks. The terms are recorded
// because a trail saying only "a search happened" answers nobody.
//
// The recorded outcome reflects COVERAGE, not row count: a search that reached three of
// nine nodes is a partial answer, and a trail that calls it success invites someone to
// later read "no sightings" off it as if the fleet had been asked.
// recordAppearanceSearch audits "who asked where else this person went".
//
// Audited at least as carefully as an object search, and arguably more: an object search
// asks what a camera saw, while this one follows an individual across an estate. The trail
// records WHICH sighting was used as the query, so a review can reconstruct exactly who was
// being looked for rather than only that somebody searched.
func (a *fleetSearchApi) recordAppearanceSearch(r *http.Request, q services.FleetAppearanceQuery, result services.FleetAppearanceResult) {
	if a.audit == nil {
		return
	}
	outcome := "success"
	if !result.Coverage.Complete {
		outcome = "partial"
	}
	actorID, actorLabel, roleID := auditActor(r)
	a.audit.Record(r.Context(), services.AuditEntry{
		Action:     "fleet.search.appearance",
		ActorId:    actorID,
		ActorEmail: actorLabel,
		ActorRole:  roleID,
		TargetType: "fleet",
		TargetId:   q.SourceNodeId,
		Outcome:    outcome,
		Detail: fmt.Sprintf("appearance of %s sighting %d on %s",
			defaultLabelName(result.Label), q.SourceObservationId, q.SourceNodeId),
		Metadata: map[string]any{
			"sourceNodeId":  q.SourceNodeId,
			"observationId": q.SourceObservationId,
			"label":         result.Label,
			"model":         result.Model,
			"from":          q.From,
			"to":            q.To,
			"matches":       result.Total,
			"scanned":       result.Scanned,
			"searched":      result.Coverage.Searched,
			"answered":      result.Coverage.Answered,
			"complete":      result.Coverage.Complete,
		},
	})
}

func defaultLabelName(label string) string {
	if strings.TrimSpace(label) == "" {
		return "an object"
	}
	return label
}

func (a *fleetSearchApi) recordSearch(r *http.Request, q services.FleetSearchQuery, result services.FleetSearchResult) {
	if a.audit == nil {
		return
	}
	outcome := "success"
	if !result.Coverage.Complete {
		outcome = "partial"
	}
	terms := []string{}
	if strings.TrimSpace(q.Text) != "" {
		terms = append(terms, "text="+strings.TrimSpace(q.Text))
	}
	if len(q.Labels) > 0 {
		terms = append(terms, "labels="+strings.Join(q.Labels, "|"))
	}
	detail := strings.Join(terms, " ")
	if detail == "" {
		detail = "any sighting"
	}
	actorID, actorLabel, roleID := auditActor(r)
	a.audit.Record(r.Context(), services.AuditEntry{
		Action:     "fleet.search",
		ActorId:    actorID,
		ActorEmail: actorLabel,
		ActorRole:  roleID,
		TargetType: "fleet",
		TargetId:   q.NodeId,
		Outcome:    outcome,
		Detail:     detail,
		Metadata: map[string]any{
			"from":     q.From,
			"to":       q.To,
			"text":     strings.TrimSpace(q.Text),
			"labels":   q.Labels,
			"siteId":   q.SiteId,
			"nodeId":   q.NodeId,
			"sources":  q.Sources,
			"results":  result.Total,
			"searched": result.Coverage.Searched,
			"answered": result.Coverage.Answered,
			"complete": result.Coverage.Complete,
		},
		ClientIp: clientIP(r),
	})
}

// fleetSearchQueryFromRequest parses the fleet-search query string.
func fleetSearchQueryFromRequest(r *http.Request) services.FleetSearchQuery {
	query := r.URL.Query()
	q := services.FleetSearchQuery{
		From:          parseInt64Param(query.Get("from")),
		To:            parseInt64Param(query.Get("to")),
		Sources:       splitCSVParam(query["sources"]),
		Labels:        splitCSVParam(query["labels"]),
		IdentityKinds: splitCSVParam(query["identityKinds"]),
		Text:          strings.TrimSpace(query.Get("text")),
		SiteId:        parseInt64Param(query.Get("siteId")),
		NodeId:        strings.TrimSpace(query.Get("nodeId")),
		Limit:         int(parseInt64Param(query.Get("limit"))),
	}
	if raw := strings.TrimSpace(query.Get("minConfidence")); raw != "" {
		if v, err := strconv.ParseFloat(raw, 64); err == nil {
			q.MinConfidence = v
		}
	}
	return q
}

// parseInt64Param reads an int64 query value, 0 when absent or unparseable.
func parseInt64Param(raw string) int64 {
	v, _ := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	return v
}

// splitCSVParam flattens a repeatable, comma-separated query parameter.
func splitCSVParam(values []string) []string {
	out := []string{}
	for _, raw := range values {
		for _, part := range strings.Split(raw, ",") {
			if part = strings.TrimSpace(part); part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}
