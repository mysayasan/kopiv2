package apis

import (
	"context"
	"encoding/csv"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/mypintusan/services"
	sharedapis "github.com/mysayasan/kopiv2/domain/shared/apis"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
)

// The administrative trail's read surface, and the recording helper every audited handler holds.
//
// entities.AccessEvent answers "who went through which door, when, and was it allowed". This
// answers the other half — "who decided they could" — and the two are deliberately separate
// tables: one is a decision log written by the controller at machine rate, the other is a record of
// human authority exercised over that controller. Filtering, exporting or ageing one must not touch
// the other.
//
// There is NO delete route and NO update route here, not even for an administrator. The value of
// the trail is precisely that the person whose actions it records cannot edit it. Age-based
// retention exists (domain/shared/audit/retention.go), is reachable only from configuration,
// archives to disk before removing anything, and writes its own run into the trail it trimmed.

type auditApi struct {
	serv services.IAuditService
}

// Auditor is what an audited handler holds: the trail plus the trusted-proxy list needed to resolve
// a caller's real address. It is a value, not a global, so a handler that was never given one
// records nothing rather than silently reaching a package-level service that may not exist yet.
type Auditor struct {
	serv services.IAuditService
	// trustedProxies is the parsed rate-limit trusted-proxy list. Without it, X-Forwarded-For is
	// ignored entirely — an untrusted caller must not be able to forge the address recorded against
	// their own change to who may enter the building.
	trustedProxies []*net.IPNet
}

// NewAuditor builds the recording helper. trustedProxyCIDRs comes from the same config block the
// rate limiter uses, so "which hops may set X-Forwarded-For" has exactly one answer in this app.
func NewAuditor(serv services.IAuditService, trustedProxyCIDRs []string) *Auditor {
	return &Auditor{serv: serv, trustedProxies: middlewares.ParseTrustedProxies(trustedProxyCIDRs)}
}

// NewAuditApi mounts the trail.
//
//	GET /api/audit      — list, newest-first, filterable
//	GET /api/audit.csv  — the same listing as a CSV download, for an auditor who wants it outside
//	                      the product
//
// `/api/audit.csv` is its OWN catalog rule in services/rbac.go, not a child of `/api/audit`: the
// matrix matches segment-wise, and "audit.csv" is a different segment from "audit". A rule that
// covers the listing and not the export is the shape of mistake #224 found — a line of
// documentation shaped like policy.
func NewAuditApi(router *mux.Router, serv services.IAuditService) {
	h := &auditApi{serv: serv}
	router.HandleFunc("/audit", h.list).Methods("GET")
	router.HandleFunc("/audit.csv", h.exportCSV).Methods("GET")
}

// auditFilterFromQuery reads the shared narrowing parameters.
func auditFilterFromQuery(r *http.Request) services.AuditFilter {
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
	rows, total, err := a.serv.List(r.Context(), limit, offset, auditFilterFromQuery(r))
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

// auditCSVMaxRows caps an export rather than paging it: an export is for handing to somebody, and a
// silently truncated one is worse than a bounded one — so the cap is stated in a response header
// when it bites.
const auditCSVMaxRows = 10000

func (a *auditApi) exportCSV(w http.ResponseWriter, r *http.Request) {
	rows, total, err := a.serv.List(r.Context(), auditCSVMaxRows, 0, auditFilterFromQuery(r))
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf(`attachment; filename="mypintusan-trail-%s.csv"`, time.Now().Format("20060102-150405")))
	if total > uint64(len(rows)) {
		w.Header().Set("X-Audit-Truncated", strconv.FormatUint(total, 10))
	}

	// A UTF-8 BOM, because the destination for this file is Excel on somebody's Windows laptop.
	// The entries are full of em dashes and quoted names, and without the BOM Excel decodes the
	// whole export as the local codepage — an auditor's copy of the trail arrives as mojibake,
	// which is a bad look for the one artefact whose job is to be handed to an outsider.
	_, _ = w.Write([]byte{0xEF, 0xBB, 0xBF})

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

// auditActor resolves the signed-in principal for an entry.
//
// The actor is taken from the SESSION, never from a request body — the same rule doors.unlock
// already follows. An attacker-supplied actor name next to a grant edit is worse than no name at
// all: it is a forged record, and the trail's whole value is that it cannot be authored by its
// subject.
//
// An unauthenticated request cannot reach an audited route (the auth middleware rejects it first),
// so the zero return is a defensive path rather than an expected one — and it still records the
// action with the actor blank, because an unattributed entry beats a missing one.
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

// Record is the one call site every audited handler goes through, so no handler has to remember to
// resolve the actor, the client IP or the user agent — and so no handler can spell an outcome
// wrong.
//
// A nil Auditor records nothing and does not panic: auditing must never be able to fail the action
// being audited, and that has to hold for a mis-wired composition root too.
func (a *Auditor) Record(r *http.Request, action, targetType, targetID, outcome, detail string, meta map[string]any) {
	if a == nil || a.serv == nil {
		return
	}
	// Tell the fallback middleware this request has been accounted for. Done BEFORE the write, so a
	// trail that is failing to persist does not also produce a duplicate generic row for every
	// action — one broken thing, one symptom.
	markAudited(r.Context())
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

// Success and Denied are the two shapes nearly every call site wants.
//
// Denied — not "error" — is the outcome for a refused administrative act, and it is recorded as
// carefully as an accepted one: an operator who tried to edit a grant and was turned away is a
// thing an investigation wants to know, and it is invisible everywhere else.
func (a *Auditor) Success(r *http.Request, action, targetType, targetID, detail string, meta map[string]any) {
	a.Record(r, action, targetType, targetID, services.OutcomeSuccess, detail, meta)
}

func (a *Auditor) Denied(r *http.Request, action, targetType, targetID, detail string, meta map[string]any) {
	a.Record(r, action, targetType, targetID, services.OutcomeDenied, detail, meta)
}

// ID renders an int64 target id, so no call site reaches for strconv.
func ID(v int64) string { return strconv.FormatInt(v, 10) }

// --- the fallback: nothing accepted goes unrecorded --------------------------

// auditMarkKey is the context key for the per-request "a handler already audited this" flag.
type auditMarkKey struct{}

// auditMark is that flag. A pointer in the context rather than a value, because a handler records
// deep inside its own call stack and cannot hand a new context back up to the middleware.
type auditMark struct{ audited bool }

func markAudited(ctx context.Context) {
	if m, ok := ctx.Value(auditMarkKey{}).(*auditMark); ok && m != nil {
		m.audited = true
	}
}

// auditExemptPrefixes are accepted mutations the fallback deliberately does NOT record.
//
// Kept short and argued, because every entry here is a hole. Marking a feed notification read is a
// per-user UI state change with no effect on who may enter anything, and a controller in a busy
// building generates them by the dozen — a trail that fills with them is a trail nobody reads,
// which is the failure mode that makes an audit log worthless without ever looking broken.
var auditExemptPrefixes = []string{"/api/notifications"}

// NewAuditMiddleware records any ACCEPTED mutating request that reached no handler which audited
// itself.
//
// This exists because of the shape of defect this app keeps producing: the mechanism is right and
// nothing reaches it. Seven benches found the same thing seven times — alarms that could never
// fire, a cache that could never expire, a catalog rule that matched no route. An audit trail
// instrumented handler-by-handler has exactly that failure mode built in: it is correct on the day
// it ships and silently incomplete the first time somebody adds a route and forgets, and the
// symptom is an empty result in an investigation years later.
//
// So the default is RECORDED, and a handler's own entry is an enrichment rather than the only
// chance. A generic row names the actor, the method, the path and the outcome — thin, but never
// absent. This is what puts fleet pairing (mounted from shared code this app does not own) and
// anything added next year into the trail without a second decision.
//
// It must be registered INSIDE the auth and permission middleware, so the principal is in context.
// A matrix-level 403 is therefore NOT in the trail — it never reaches here — and that is stated
// rather than papered over: those refusals are in api_log. What this does catch is the refusal that
// matters more, a handler's own `administrators only` gate, where the caller passed the matrix and
// was still turned away.
func NewAuditMiddleware(auditor *Auditor) mux.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
			default:
				next.ServeHTTP(w, r)
				return
			}
			for _, p := range auditExemptPrefixes {
				if strings.HasPrefix(r.URL.Path, p) {
					next.ServeHTTP(w, r)
					return
				}
			}

			mark := &auditMark{}
			r = r.WithContext(context.WithValue(r.Context(), auditMarkKey{}, mark))
			rec := &statusCapture{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)

			if mark.audited {
				return
			}
			switch {
			case rec.status >= 200 && rec.status < 300:
				auditor.Success(r, services.ActionApiWrite, services.TargetApi, r.URL.Path,
					fmt.Sprintf("%s %s", r.Method, r.URL.Path), nil)
			case rec.status == http.StatusForbidden:
				// A refusal from a handler's own admin gate. The caller got past the matrix and was
				// still turned away, which is the one denial worth a row of its own.
				auditor.Denied(r, services.ActionApiWrite, services.TargetApi, r.URL.Path,
					fmt.Sprintf("%s %s refused", r.Method, r.URL.Path), nil)
			}
			// Everything else — a malformed body, a duplicate name, a missing row — is left out.
			// A validation error is not a security event, and a trail that records typing mistakes
			// buries the entries that are.
		})
	}
}

// statusCapture remembers the status code so the middleware can tell an accepted change from a
// refused one. A handler that never calls WriteHeader has written 200, which is why the zero value
// is not left as the default.
type statusCapture struct {
	http.ResponseWriter
	status int
}

func (s *statusCapture) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}
