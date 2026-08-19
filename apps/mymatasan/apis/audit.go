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
	"github.com/mysayasan/kopiv2/apps/mymatasan/services"
	sharedapis "github.com/mysayasan/kopiv2/domain/shared/apis"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
)

// The audit read surface. Writing is done by the handlers that perform the audited
// action; this only exposes the trail for review and export.
//
// There is deliberately no delete route and no update route — not even for a superadmin.
// The value of the trail is that the person whose actions it records cannot edit it, and
// an endpoint that trimmed it would remove exactly that property. Age-based retention
// exists (domain/shared/audit/retention.go) but is reachable only from configuration, and
// archives to disk before it removes anything.

type auditApi struct {
	serv services.IAuditService
}

// Auditor is what an audited handler holds: the trail plus the trusted-proxy list needed
// to resolve a caller's real address. It is a value, not a global, so a handler that was
// never given one simply records nothing rather than silently reaching a package-level
// service that may not exist yet.
type Auditor struct {
	serv services.IAuditService
	// trustedProxies is the parsed rate-limit trusted-proxy list. Without it,
	// X-Forwarded-For is ignored entirely — an untrusted caller must not be able to
	// forge the address recorded against their own action.
	trustedProxies []*net.IPNet
}

// NewAuditor builds the recording helper. trustedProxyCIDRs comes from the same config
// block the rate limiter uses, so "which hops may set X-Forwarded-For" has one answer.
func NewAuditor(serv services.IAuditService, trustedProxyCIDRs []string) *Auditor {
	return &Auditor{serv: serv, trustedProxies: middlewares.ParseTrustedProxies(trustedProxyCIDRs)}
}

// NewAuditApi mounts the trail under /audit.
//
//	GET /api/audit      — list, newest-first, filterable
//	GET /api/audit.csv  — the same listing as a CSV download, for an auditor who wants it
//	                      outside the product
//
// Access is decided by the role permission matrix like every other route (see
// services/pages.go, which grants it to admins only): the trail names who watched which
// footage, so it is itself sensitive.
func NewAuditApi(router *mux.Router, serv services.IAuditService) {
	h := &auditApi{serv: serv}
	router.HandleFunc("/audit", h.list).Methods("GET")
	router.HandleFunc("/audit.csv", h.exportCSV).Methods("GET")
}

// filterFromQuery reads the shared narrowing parameters.
func filterFromQuery(r *http.Request) services.AuditFilter {
	q := r.URL.Query()
	from, _ := strconv.ParseInt(q.Get("from"), 10, 64)
	to, _ := strconv.ParseInt(q.Get("to"), 10, 64)
	return services.AuditFilter{
		Action:     q.Get("action"),
		Outcome:    q.Get("outcome"),
		ActorEmail: q.Get("actor"),
		TargetType: q.Get("targetType"),
		TargetId:   q.Get("targetId"),
		From:       from,
		To:         to,
	}
}

func (a *auditApi) list(w http.ResponseWriter, r *http.Request) {
	limit, offset := readPaging(r)
	rows, total, err := a.serv.List(r.Context(), limit, offset, filterFromQuery(r))
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{
		"items":  rows,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	}, "succeed")
}

// exportCSV streams the trail as CSV. Capped at auditCSVMaxRows rather than paged: an
// export is for handing to somebody, and a silently truncated one is worse than a
// bounded one, so the cap is stated in the response headers and the filename.
const auditCSVMaxRows = 10000

func (a *auditApi) exportCSV(w http.ResponseWriter, r *http.Request) {
	rows, total, err := a.serv.List(r.Context(), auditCSVMaxRows, 0, filterFromQuery(r))
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf(`attachment; filename="mymatasan-audit-%s.csv"`, time.Now().Format("20060102-150405")))
	// Says so in the response when the export is not the whole story.
	if total > uint64(len(rows)) {
		w.Header().Set("X-Audit-Truncated", strconv.FormatUint(total, 10))
	}

	cw := csv.NewWriter(w)
	defer cw.Flush()
	_ = cw.Write([]string{"time", "action", "outcome", "actor", "actorId", "targetType", "targetId", "detail", "clientIp", "userAgent", "metadata"})
	for _, row := range rows {
		if row == nil {
			continue
		}
		_ = cw.Write([]string{
			time.Unix(row.CreatedAt, 0).UTC().Format(time.RFC3339),
			row.Action,
			row.Outcome,
			row.ActorEmail,
			strconv.FormatInt(row.ActorId, 10),
			row.TargetType,
			row.TargetId,
			row.Detail,
			row.ClientIp,
			row.UserAgent,
			row.Metadata,
		})
	}
}

// --- recording helpers, used by every audited handler -----------------------

// auditActor resolves the signed-in principal for an audit entry.
//
// mymatasan authenticates with its own local session rather than a JWT, so this reads the
// principal the auth middleware put in the context. An unauthenticated request cannot
// reach an audited route (the middleware rejects it first), so the zero return is a
// defensive path rather than an expected one — and it still records the action, with the
// actor blank, because an unattributed entry beats a missing one.
func auditActor(r *http.Request) (id int64, label string, role int64) {
	user, ok := sharedapis.LocalUserFromContext(r.Context())
	if !ok || user == nil {
		return 0, "", 0
	}
	label = strings.TrimSpace(user.Username)
	if label == "" {
		label = strings.TrimSpace(user.DisplayName)
	}
	if label == "" {
		label = strconv.FormatInt(user.Id, 10)
	}
	return user.Id, label, user.RoleId
}

// Record is the one call site every audited handler goes through, so no handler has to
// remember to resolve the actor, the client IP or the user agent.
//
// A nil Auditor records nothing and does not panic: auditing must never be able to fail
// the action being audited, and that has to hold for a mis-wired composition root too.
func (a *Auditor) Record(r *http.Request, action, targetType, targetID, outcome, detail string, meta map[string]any) {
	if a == nil || a.serv == nil {
		return
	}
	actorID, actorLabel, roleID := auditActor(r)
	a.serv.Record(r.Context(), services.AuditEntry{
		Action:     action,
		ActorId:    actorID,
		ActorEmail: actorLabel,
		ActorRole:  roleID,
		TargetType: targetType,
		TargetId:   targetID,
		Outcome:    outcome,
		Detail:     detail,
		Metadata:   meta,
		ClientIp:   middlewares.ClientIP(r, a.trustedProxies),
		UserAgent:  r.UserAgent(),
	})
}

// Success and Failure are the two shapes nearly every call site wants, so the outcome
// string is never spelled out (and never misspelled) at a handler.
func (a *Auditor) Success(r *http.Request, action, targetType, targetID, detail string, meta map[string]any) {
	a.Record(r, action, targetType, targetID, services.OutcomeSuccess, detail, meta)
}

func (a *Auditor) Failure(r *http.Request, action, targetType, targetID, detail string, meta map[string]any) {
	a.Record(r, action, targetType, targetID, services.OutcomeError, detail, meta)
}
