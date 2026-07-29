package apis

import (
	"encoding/csv"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/myidsan/services"
	enumauth "github.com/mysayasan/kopiv2/domain/enums/auth"
	"github.com/mysayasan/kopiv2/domain/models"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
)

type auditApi struct {
	audit services.IAuditService
}

// NewAuditApi mounts the read-only, superadmin-gated security trail.
//
//	GET /api/audit?limit=&offset=&action=&outcome=&actorEmail=&targetType=&targetId=&from=&to=
//	GET /api/audit/export.csv?<same filters>
//
// There is deliberately no create/update/delete route: entries are written server-side by
// the handlers that perform the audited actions, and nothing in the product can edit the
// trail. Superadmin-only because the trail names who did what from where — it is itself
// sensitive, and on an identity server it also reveals which usernames exist.
func NewAuditApi(router *mux.Router, auth middlewares.AuthMidware, access *middlewares.AccessSessionMidware, audit services.IAuditService) {
	h := &auditApi{audit: audit}

	group := router.PathPrefix("/audit").Subrouter()
	group.Use(auth.Middleware)
	group.Use(access.Middleware)
	group.Use(access.RequireSuperadmin)

	group.HandleFunc("", h.list).Methods("GET")
	group.HandleFunc("/export.csv", h.exportCSV).Methods("GET")
}

// auditFilterFromQuery parses the shared filter set used by both the list and the export,
// so a CSV always contains exactly what the screen was showing.
func auditFilterFromQuery(r *http.Request) services.AuditFilter {
	q := r.URL.Query()
	parseUnix := func(key string) int64 {
		raw := strings.TrimSpace(q.Get(key))
		if raw == "" {
			return 0
		}
		if secs, err := strconv.ParseInt(raw, 10, 64); err == nil {
			return secs
		}
		// Also accept a plain YYYY-MM-DD, which is what a date input sends.
		if t, err := time.Parse("2006-01-02", raw); err == nil {
			if key == "to" {
				// Make an end date inclusive of the whole day.
				return t.Add(24*time.Hour - time.Second).Unix()
			}
			return t.Unix()
		}
		return 0
	}
	return services.AuditFilter{
		Action:     q.Get("action"),
		Outcome:    q.Get("outcome"),
		ActorEmail: q.Get("actorEmail"),
		TargetType: q.Get("targetType"),
		TargetId:   q.Get("targetId"),
		From:       parseUnix("from"),
		To:         parseUnix("to"),
	}
}

func (a *auditApi) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.ParseUint(q.Get("limit"), 10, 64)
	offset, _ := strconv.ParseUint(q.Get("offset"), 10, 64)

	rows, total, err := a.audit.List(r.Context(), limit, offset, auditFilterFromQuery(r))
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendPagingResult(w, rows, limit, offset, total, "succeed")
}

// auditExportMaxRows caps a single export. Compliance reporting wants a bounded file it
// can actually open, and an unbounded export of a busy server is a memory and timeout
// hazard; narrow the date range to get the rest.
const auditExportMaxRows = 50000

func (a *auditApi) exportCSV(w http.ResponseWriter, r *http.Request) {
	rows, _, err := a.audit.List(r.Context(), auditExportMaxRows, 0, auditFilterFromQuery(r))
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}

	filename := fmt.Sprintf("myidsan-audit-%s.csv", time.Now().Format("20060102-150405"))
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename="+filename)
	w.WriteHeader(http.StatusOK)

	cw := csv.NewWriter(w)
	defer cw.Flush()

	_ = cw.Write([]string{
		"timestamp_utc", "action", "outcome", "actor_id", "actor_email", "actor_role",
		"target_type", "target_id", "detail", "client_ip", "user_agent", "metadata",
	})
	for _, row := range rows {
		if row == nil {
			continue
		}
		_ = cw.Write([]string{
			time.Unix(row.CreatedAt, 0).UTC().Format(time.RFC3339),
			row.Action,
			row.Outcome,
			strconv.FormatInt(row.ActorId, 10),
			csvSafe(row.ActorEmail),
			strconv.FormatInt(row.ActorRole, 10),
			row.TargetType,
			csvSafe(row.TargetId),
			csvSafe(row.Detail),
			row.ClientIp,
			csvSafe(row.UserAgent),
			csvSafe(row.Metadata),
		})
	}
}

// csvSafe defuses spreadsheet formula injection. Excel and Sheets evaluate a cell starting
// with = + - @ (or a leading tab/CR) as a formula, and this export carries attacker-influenced
// text: a failed-login "username" or a User-Agent is whatever the caller sent. Prefixing a
// quote makes the cell literal without altering the value a human reads.
func csvSafe(v string) string {
	if v == "" {
		return v
	}
	switch v[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + v
	}
	return v
}

// --- shared actor/context helpers used by the audited handlers -------------------

// auditActor pulls the requesting operator's identity from the JWT claims. Returns zeroes
// for an unauthenticated request, which is correct for a failed sign-in: the caller has no
// identity yet, and the attempted username is recorded as ActorEmail by the caller instead.
func auditActor(r *http.Request) (id int64, label string, role int64) {
	claims, ok := r.Context().Value(enumauth.Claims).(*models.JwtCustomClaims)
	if !ok || claims == nil {
		return 0, "", 0
	}
	label = strings.TrimSpace(claims.Email)
	if label == "" {
		label = strings.TrimSpace(claims.Name)
	}
	if label == "" {
		label = strconv.FormatInt(claims.Id, 10)
	}
	return claims.Id, label, claims.RoleId
}

// auditContext fills the request-derived fields every entry shares.
func auditContext(r *http.Request, trusted []*net.IPNet) (clientIP, userAgent string) {
	return middlewares.ClientIP(r, trusted), middlewares.UserAgent(r)
}

// auditRecorder is embedded by the API handlers that perform auditable actions. It exists
// so each one does not repeat the actor lookup, the client-address resolution and the
// nil-service guard — three things that are easy to get subtly different, and where
// "different" means the same operator appears under two identities in the trail.
type auditRecorder struct {
	audit   services.IAuditService
	trusted []*net.IPNet
}

func newAuditRecorder(audit services.IAuditService, trustedProxies []string) auditRecorder {
	return auditRecorder{audit: audit, trusted: middlewares.ParseTrustedProxies(trustedProxies)}
}

// record fills in the actor from the request's claims and the request-derived context,
// then writes the entry. Fields already set on e are left alone, so a caller can override
// the actor for an event raised on someone else's behalf.
func (a auditRecorder) record(r *http.Request, e services.AuditEntry) {
	if a.audit == nil {
		return
	}
	if e.ActorId == 0 && e.ActorEmail == "" {
		e.ActorId, e.ActorEmail, e.ActorRole = auditActor(r)
	}
	e.ClientIp, e.UserAgent = auditContext(r, a.trusted)
	a.audit.Record(r.Context(), e)
}
