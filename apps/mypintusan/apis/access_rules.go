package apis

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/mypintusan/entities"
	"github.com/mysayasan/kopiv2/apps/mypintusan/services"
	"github.com/mysayasan/kopiv2/domain/notification"
	sharedapis "github.com/mysayasan/kopiv2/domain/shared/apis"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// The access rules API: groups, schedules and grants — the classic triple, exposed at last.
//
// Until this file existed a person created through the wizard could not badge through ANY door:
// the decision path requires a grant, grants require a group and a schedule, and nothing served
// them. The rbac.go header explains why every WRITE here is admin-only while credentials are
// operator-level: issuing a badge is daily, reversible and logged; editing a grant silently
// changes who may enter every door in that group, indefinitely, and looks like nothing in a log.
//
// Which is why the mutations here are deliberately NOT silent: every grant created or deleted and
// every membership change publishes a notification naming the administrator who did it. That
// lands in the local feed AND — on an adopted node — travels up the fleet channel, so the change
// is visible one level above the person who made it.
type accessRulesApi struct {
	groups    dbsql.IGenericRepo[entities.AccessGroup]
	members   dbsql.IGenericRepo[entities.AccessGroupMember]
	schedules dbsql.IGenericRepo[entities.Schedule]
	windows   dbsql.IGenericRepo[entities.ScheduleWindow]
	holidays  dbsql.IGenericRepo[entities.Holiday]
	grants    dbsql.IGenericRepo[entities.Grant]
	doors     dbsql.IGenericRepo[entities.Door]
	holders   dbsql.IGenericRepo[entities.Holder]
	notify    *notification.Service
	// audit is the durable half of the pair. The notification below says a rule changed while
	// somebody is still watching the feed; this says who changed it, to what, from where, in a
	// table that is still there next year. Neither replaces the other.
	audit *Auditor
}

// NewAccessRulesApi registers the groups / schedules / grants surface.
func NewAccessRulesApi(router *mux.Router, db dbsql.IDbCrud, notify *notification.Service, audit *Auditor) {
	a := &accessRulesApi{
		groups:    dbsql.NewGenericRepo[entities.AccessGroup](db),
		members:   dbsql.NewGenericRepo[entities.AccessGroupMember](db),
		schedules: dbsql.NewGenericRepo[entities.Schedule](db),
		windows:   dbsql.NewGenericRepo[entities.ScheduleWindow](db),
		holidays:  dbsql.NewGenericRepo[entities.Holiday](db),
		grants:    dbsql.NewGenericRepo[entities.Grant](db),
		doors:     dbsql.NewGenericRepo[entities.Door](db),
		holders:   dbsql.NewGenericRepo[entities.Holder](db),
		notify:    notify,
		audit:     audit,
	}

	g := router.PathPrefix("/groups").Subrouter()
	g.HandleFunc("", a.listGroups).Methods("GET")
	g.HandleFunc("", a.createGroup).Methods("POST")
	g.HandleFunc("/{id:[0-9]+}", a.deleteGroup).Methods("DELETE")
	g.HandleFunc("/{id:[0-9]+}/members", a.listMembers).Methods("GET")
	g.HandleFunc("/{id:[0-9]+}/members", a.addMember).Methods("POST")
	g.HandleFunc("/{id:[0-9]+}/members/{memberId:[0-9]+}", a.removeMember).Methods("DELETE")

	s := router.PathPrefix("/schedules").Subrouter()
	// The literal /holidays routes are registered BEFORE /{id}; the numeric constraint on {id}
	// keeps them from ever colliding, but the order still reads as the intent.
	s.HandleFunc("/holidays", a.listHolidays).Methods("GET")
	s.HandleFunc("/holidays", a.createHoliday).Methods("POST")
	s.HandleFunc("/holidays/{id:[0-9]+}", a.deleteHoliday).Methods("DELETE")
	s.HandleFunc("", a.listSchedules).Methods("GET")
	s.HandleFunc("", a.createSchedule).Methods("POST")
	s.HandleFunc("/{id:[0-9]+}", a.deleteSchedule).Methods("DELETE")

	gr := router.PathPrefix("/grants").Subrouter()
	gr.HandleFunc("", a.listGrants).Methods("GET")
	gr.HandleFunc("", a.createGrant).Methods("POST")
	gr.HandleFunc("/{id:[0-9]+}", a.deleteGrant).Methods("DELETE")
}

// requireAdmin is the explicit gate on top of the permission matrix, mirroring doors.create: a
// wrong value here does not produce a bad reading, it changes who may enter the building.
func requireAdmin(w http.ResponseWriter, r *http.Request) (int64, string, bool) {
	user, ok := sharedapis.LocalUserFromContext(r.Context())
	if !ok || !user.IsAdmin {
		controllers.SendError(w, controllers.ErrLimitedAccess, "administrators only")
		return 0, "", false
	}
	name := user.DisplayName
	if name == "" {
		name = user.Username
	}
	return user.Id, name, true
}

func pathID(r *http.Request, key string) int64 {
	id, _ := strconv.ParseInt(mux.Vars(r)[key], 10, 64)
	return id
}

func eq(field string, v any) sqldataenums.Filter {
	return sqldataenums.Filter{FieldName: field, Compare: sqldataenums.Equal, Value: v}
}

// ruleChanged publishes the audit notification for a mutation. Info severity: this is a record,
// not an alarm — but it is a record that exists precisely because the change itself looks like
// nothing in the access log.
func (a *accessRulesApi) ruleChanged(ctx context.Context, actor string, body string) {
	if a.notify == nil {
		return
	}
	a.notify.Publish(ctx, notification.Notification{
		Category: "access.rule-change",
		Severity: notification.Info,
		Title:    "Access rules changed",
		Body:     body + " — by " + actor,
		Source:   "mypintusan",
	})
}

// --- groups -------------------------------------------------------------------------------------

func (a *accessRulesApi) listGroups(w http.ResponseWriter, r *http.Request) {
	rows, total, err := a.groups.Get(r.Context(), "", 500, 0, nil, nil)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{"items": rows, "total": total}, "succeed")
}

func (a *accessRulesApi) createGroup(w http.ResponseWriter, r *http.Request) {
	actorId, actor, ok := requireAdmin(w, r)
	if !ok {
		return
	}
	var body struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if body.Name == "" {
		controllers.SendError(w, controllers.ErrBadRequest, "a group needs a name")
		return
	}
	group := entities.AccessGroup{
		Name: body.Name, Description: body.Description,
		CreatedBy: actorId, CreatedAt: time.Now().Unix(),
	}
	id, err := a.groups.Create(r.Context(), "", group)
	if err != nil {
		controllers.SendError(w, controllers.ErrConflict, err.Error())
		return
	}
	group.Id = int64(id)
	// A group is empty and reaches nothing on the day it is made, which is exactly why its creation
	// was the one rule change nothing here announced. It is also the first half of every grant that
	// follows, so an investigation reading backwards has to be able to find it.
	a.audit.Success(r, services.ActionGroupCreate, services.TargetGroup, ID(group.Id),
		fmt.Sprintf("access group %q created", group.Name),
		map[string]any{"name": group.Name, "description": group.Description})
	a.ruleChanged(r.Context(), actor, fmt.Sprintf("group %q created", group.Name))
	controllers.SendResult(w, group, "succeed")
}

func (a *accessRulesApi) deleteGroup(w http.ResponseWriter, r *http.Request) {
	_, actor, ok := requireAdmin(w, r)
	if !ok {
		return
	}
	id := pathID(r, "id")
	group, err := a.groups.GetById(r.Context(), "", uint64(id))
	if err != nil || group == nil {
		controllers.SendError(w, controllers.ErrNotFound, "no such group")
		return
	}
	// Refuse while grants reference it. Deleting the group would not revoke the access — the
	// grant rows would keep matching nothing and an auditor would find rules pointing at a ghost.
	if _, n, err := a.grants.Get(r.Context(), "", 1, 0, []sqldataenums.Filter{eq("GroupId", id)}, nil); err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	} else if n > 0 {
		controllers.SendError(w, controllers.ErrConflict,
			"this group still grants access to doors; delete its grants first")
		return
	}
	// Memberships go with the group — they mean nothing without it.
	if rows, _, _ := a.members.Get(r.Context(), "", 1, 0, []sqldataenums.Filter{eq("GroupId", id)}, nil); len(rows) > 0 {
		if _, err := a.members.Delete(r.Context(), "", []sqldataenums.Filter{eq("GroupId", id)}); err != nil {
			controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
			return
		}
	}
	if _, err := a.groups.Delete(r.Context(), "", []sqldataenums.Filter{eq("Id", id)}); err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	a.audit.Success(r, services.ActionGroupDelete, services.TargetGroup, ID(group.Id),
		fmt.Sprintf("access group %q deleted", group.Name),
		map[string]any{"name": group.Name})
	a.ruleChanged(r.Context(), actor, fmt.Sprintf("group %q deleted", group.Name))
	controllers.SendResult(w, map[string]any{"deleted": true}, "succeed")
}

// --- members ------------------------------------------------------------------------------------

func (a *accessRulesApi) listMembers(w http.ResponseWriter, r *http.Request) {
	id := pathID(r, "id")
	rows, total, err := a.members.Get(r.Context(), "", 1000, 0, []sqldataenums.Filter{eq("GroupId", id)}, nil)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	// Hydrate holder names so the screen does not show "holder 4471". The list is small (one
	// group), so per-row lookups are fine and keep this free of join plumbing.
	type memberRow struct {
		entities.AccessGroupMember
		HolderName string `json:"holderName"`
	}
	out := make([]memberRow, 0, len(rows))
	for _, m := range rows {
		row := memberRow{AccessGroupMember: *m}
		if h, err := a.holders.GetById(r.Context(), "", uint64(m.HolderId)); err == nil && h != nil {
			row.HolderName = h.Name
		}
		out = append(out, row)
	}
	controllers.SendResult(w, map[string]any{"items": out, "total": total}, "succeed")
}

func (a *accessRulesApi) addMember(w http.ResponseWriter, r *http.Request) {
	actorId, actor, ok := requireAdmin(w, r)
	if !ok {
		return
	}
	groupId := pathID(r, "id")
	group, err := a.groups.GetById(r.Context(), "", uint64(groupId))
	if err != nil || group == nil {
		controllers.SendError(w, controllers.ErrNotFound, "no such group")
		return
	}
	var body struct {
		HolderId int64 `json:"holderId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	holder, err := a.holders.GetById(r.Context(), "", uint64(body.HolderId))
	if err != nil || holder == nil {
		controllers.SendError(w, controllers.ErrNotFound, "no such holder")
		return
	}
	// Adding twice is a no-op, not an error: grants are additive, so a duplicate row would only
	// double-count — refuse it quietly with the existing row.
	if rows, _, _ := a.members.Get(r.Context(), "", 1, 0,
		[]sqldataenums.Filter{eq("GroupId", groupId), eq("HolderId", body.HolderId)}, nil); len(rows) > 0 {
		controllers.SendResult(w, rows[0], "succeed")
		return
	}
	m := entities.AccessGroupMember{
		GroupId: groupId, HolderId: body.HolderId,
		AddedBy: actorId, AddedAt: time.Now().Unix(),
	}
	id, err := a.members.Create(r.Context(), "", m)
	if err != nil {
		controllers.SendError(w, controllers.ErrConflict, err.Error())
		return
	}
	m.Id = int64(id)
	// Membership IS access. Adding one person to one group can open every door that group reaches,
	// at every hour its schedules allow, and the access log will show nothing but ordinary badges.
	a.audit.Success(r, services.ActionGroupMemberAdd, services.TargetGroup, ID(groupId),
		fmt.Sprintf("%q added to access group %q", holder.Name, group.Name),
		map[string]any{"groupId": groupId, "groupName": group.Name,
			"holderId": holder.Id, "holderName": holder.Name})
	a.ruleChanged(r.Context(), actor, fmt.Sprintf("%q added to group %q", holder.Name, group.Name))
	controllers.SendResult(w, m, "succeed")
}

func (a *accessRulesApi) removeMember(w http.ResponseWriter, r *http.Request) {
	_, actor, ok := requireAdmin(w, r)
	if !ok {
		return
	}
	groupId, memberId := pathID(r, "id"), pathID(r, "memberId")
	rows, _, err := a.members.Get(r.Context(), "", 1, 0,
		[]sqldataenums.Filter{eq("Id", memberId), eq("GroupId", groupId)}, nil)
	if err != nil || len(rows) == 0 {
		controllers.SendError(w, controllers.ErrNotFound, "no such membership")
		return
	}
	if _, err := a.members.Delete(r.Context(), "", []sqldataenums.Filter{eq("Id", memberId)}); err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	holderName := fmt.Sprintf("holder %d", rows[0].HolderId)
	if h, err := a.holders.GetById(r.Context(), "", uint64(rows[0].HolderId)); err == nil && h != nil {
		holderName = h.Name
	}
	groupName := fmt.Sprintf("group %d", groupId)
	if g, err := a.groups.GetById(r.Context(), "", uint64(groupId)); err == nil && g != nil {
		groupName = g.Name
	}
	a.audit.Success(r, services.ActionGroupMemberRemove, services.TargetGroup, ID(groupId),
		fmt.Sprintf("%q removed from access group %q", holderName, groupName),
		map[string]any{"groupId": groupId, "groupName": groupName,
			"holderId": rows[0].HolderId, "holderName": holderName})
	a.ruleChanged(r.Context(), actor, fmt.Sprintf("%q removed from group %q", holderName, groupName))
	controllers.SendResult(w, map[string]any{"deleted": true}, "succeed")
}

// --- schedules ----------------------------------------------------------------------------------

type scheduleWithWindows struct {
	entities.Schedule
	Windows []entities.ScheduleWindow `json:"windows"`
}

func (a *accessRulesApi) listSchedules(w http.ResponseWriter, r *http.Request) {
	rows, total, err := a.schedules.Get(r.Context(), "", 500, 0, nil, nil)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	out := make([]scheduleWithWindows, 0, len(rows))
	for _, s := range rows {
		item := scheduleWithWindows{Schedule: *s, Windows: []entities.ScheduleWindow{}}
		if wins, _, err := a.windows.Get(r.Context(), "", 100, 0,
			[]sqldataenums.Filter{eq("ScheduleId", s.Id)}, nil); err == nil {
			for _, win := range wins {
				item.Windows = append(item.Windows, *win)
			}
		}
		out = append(out, item)
	}
	controllers.SendResult(w, map[string]any{"items": out, "total": total}, "succeed")
}

func (a *accessRulesApi) createSchedule(w http.ResponseWriter, r *http.Request) {
	actorId, actor, ok := requireAdmin(w, r)
	if !ok {
		return
	}
	var body struct {
		Name    string `json:"name"`
		Always  bool   `json:"always"`
		Windows []struct {
			Weekday  int `json:"weekday"`
			StartMin int `json:"startMin"`
			EndMin   int `json:"endMin"`
		} `json:"windows"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if body.Name == "" {
		controllers.SendError(w, controllers.ErrBadRequest, "a schedule needs a name")
		return
	}
	// A schedule that is neither 24/7 nor has any window matches NOTHING — every grant through it
	// would deny out-of-schedule forever, which reads as a broken door, not a broken rule.
	if !body.Always && len(body.Windows) == 0 {
		controllers.SendError(w, controllers.ErrBadRequest,
			"a schedule needs either the 24/7 flag or at least one weekly window")
		return
	}
	for _, win := range body.Windows {
		if win.Weekday < 0 || win.Weekday > 6 {
			controllers.SendError(w, controllers.ErrBadRequest, "weekday must be 0 (Sunday) to 6 (Saturday)")
			return
		}
		// EndMin < StartMin is VALID — it wraps past midnight (the night shift).
		if win.StartMin < 0 || win.StartMin > 1439 || win.EndMin < 0 || win.EndMin > 1440 {
			controllers.SendError(w, controllers.ErrBadRequest,
				"window minutes must be between 0 and 1440, and a window cannot start at 24:00")
			return
		}
		// A ZERO-LENGTH WINDOW IS THE ONE WAY A SCHEDULE CAN FAIL OPEN, so it is refused here
		// rather than stored and interpreted. `windowCovers` treats end-before-start as the night
		// shift; end EQUAL to start used to land in that same branch and matched every minute of
		// every day. Two ways to arrive at one: a slip on the schedules screen, and — far more
		// likely — a client sending field names this handler does not read, so every window arrives
		// as 0–0 while the "a schedule needs a window" guard above happily passes. Measured live:
		// such a schedule opened a door at 02:51 that was meant to be shut until 09:00.
		if win.StartMin == win.EndMin {
			controllers.SendError(w, controllers.ErrBadRequest,
				"a window cannot end at the same minute it starts; for round-the-clock access use "+
					"the 24/7 flag, and for an overnight shift give the real end time (22:00 to 06:00)")
			return
		}
	}

	sched := entities.Schedule{
		Name: body.Name, Always: body.Always,
		CreatedBy: actorId, CreatedAt: time.Now().Unix(),
	}
	id, err := a.schedules.Create(r.Context(), "", sched)
	if err != nil {
		controllers.SendError(w, controllers.ErrConflict, err.Error())
		return
	}
	sched.Id = int64(id)
	out := scheduleWithWindows{Schedule: sched, Windows: []entities.ScheduleWindow{}}
	for _, win := range body.Windows {
		row := entities.ScheduleWindow{
			ScheduleId: sched.Id, Weekday: win.Weekday, StartMin: win.StartMin, EndMin: win.EndMin,
		}
		wid, werr := a.windows.Create(r.Context(), "", row)
		if werr != nil {
			controllers.SendError(w, controllers.ErrInternalServerError, werr.Error())
			return
		}
		row.Id = int64(wid)
		out.Windows = append(out.Windows, row)
	}
	// The HOURS are the point of the entry, not the name. "Schedule created" tells an auditor
	// nothing; "Mon 09:00-17:00, Tue 09:00-17:00" is what lets them see that somebody widened the
	// night shift to cover 02:00 and that the door was behaving exactly as configured.
	a.audit.Success(r, services.ActionScheduleCreate, services.TargetSchedule, ID(sched.Id),
		fmt.Sprintf("schedule %q created (%s)", sched.Name, describeSchedule(sched, out.Windows)),
		map[string]any{"name": sched.Name, "always": sched.Always,
			"windows": describeSchedule(sched, out.Windows)})
	a.ruleChanged(r.Context(), actor, fmt.Sprintf("schedule %q created (%s)",
		sched.Name, describeSchedule(sched, out.Windows)))
	controllers.SendResult(w, out, "succeed")
}

// describeSchedule renders a schedule for the audit line, because "schedule created" without its
// hours says nothing that would let anyone spot a mistake afterwards.
func describeSchedule(s entities.Schedule, windows []entities.ScheduleWindow) string {
	if s.Always {
		return "24/7"
	}
	parts := make([]string, 0, len(windows))
	for _, w := range windows {
		parts = append(parts, fmt.Sprintf("%s %02d:%02d-%02d:%02d", weekdayName(w.Weekday),
			w.StartMin/60, w.StartMin%60, w.EndMin/60, w.EndMin%60))
	}
	return strings.Join(parts, ", ")
}

func weekdayName(d int) string {
	if d < 0 || d > 6 {
		return "?"
	}
	return time.Weekday(d).String()[:3]
}

func (a *accessRulesApi) deleteSchedule(w http.ResponseWriter, r *http.Request) {
	_, actor, ok := requireAdmin(w, r)
	if !ok {
		return
	}
	id := pathID(r, "id")
	sched, err := a.schedules.GetById(r.Context(), "", uint64(id))
	if err != nil || sched == nil {
		controllers.SendError(w, controllers.ErrNotFound, "no such schedule")
		return
	}
	if _, n, err := a.grants.Get(r.Context(), "", 1, 0, []sqldataenums.Filter{eq("ScheduleId", id)}, nil); err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	} else if n > 0 {
		controllers.SendError(w, controllers.ErrConflict,
			"grants still use this schedule; delete or repoint them first")
		return
	}
	if rows, _, _ := a.windows.Get(r.Context(), "", 1, 0, []sqldataenums.Filter{eq("ScheduleId", id)}, nil); len(rows) > 0 {
		if _, err := a.windows.Delete(r.Context(), "", []sqldataenums.Filter{eq("ScheduleId", id)}); err != nil {
			controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
			return
		}
	}
	if _, err := a.schedules.Delete(r.Context(), "", []sqldataenums.Filter{eq("Id", id)}); err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	a.audit.Success(r, services.ActionScheduleDelete, services.TargetSchedule, ID(sched.Id),
		fmt.Sprintf("schedule %q deleted", sched.Name),
		map[string]any{"name": sched.Name, "always": sched.Always})
	a.ruleChanged(r.Context(), actor, fmt.Sprintf("schedule %q deleted", sched.Name))
	controllers.SendResult(w, map[string]any{"deleted": true}, "succeed")
}

// --- holidays -----------------------------------------------------------------------------------

func (a *accessRulesApi) listHolidays(w http.ResponseWriter, r *http.Request) {
	rows, total, err := a.holidays.Get(r.Context(), "", 500, 0, nil, nil)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{"items": rows, "total": total}, "succeed")
}

// createHoliday adds a day to the calendar.
//
// A HOLIDAY IS THE ONLY RULE CHANGE THAT SHUTS A BUILDING WITHOUT ANYBODY TOUCHING A GRANT. One row
// here can deny every holder at every door on the site for a whole day — including a 24/7 schedule,
// because `scheduleAllows` consults the calendar ABOVE the always-on short circuit. That is why it
// is audited like a grant edit: the access log records DECISIONS, and `access.rule-change` is the
// only record this app keeps of who decided them.
func (a *accessRulesApi) createHoliday(w http.ResponseWriter, r *http.Request) {
	actorId, actor, ok := requireAdmin(w, r)
	if !ok {
		return
	}
	var body struct {
		Name      string `json:"name"`
		Date      string `json:"date"`
		SiteId    int64  `json:"siteId"`
		Behaviour string `json:"behaviour"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	if strings.TrimSpace(body.Name) == "" {
		controllers.SendError(w, controllers.ErrBadRequest, "a holiday needs a name")
		return
	}
	if _, err := time.Parse("2006-01-02", body.Date); err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, "date must be YYYY-MM-DD")
		return
	}
	switch body.Behaviour {
	case entities.HolidayDeny, entities.HolidayFollowSunday, entities.HolidayIgnore:
	case "":
		body.Behaviour = entities.HolidayDeny
	default:
		controllers.SendError(w, controllers.ErrBadRequest, "behaviour must be deny, follow-sunday or ignore")
		return
	}
	h := entities.Holiday{
		Name: strings.TrimSpace(body.Name), Date: body.Date, SiteId: body.SiteId,
		Behaviour: body.Behaviour, CreatedBy: actorId, CreatedAt: time.Now().Unix(),
	}
	id, err := a.holidays.Create(r.Context(), "", h)
	if err != nil {
		controllers.SendError(w, controllers.ErrConflict, err.Error())
		return
	}
	h.Id = int64(id)
	a.audit.Success(r, services.ActionHolidayCreate, services.TargetHoliday, ID(h.Id),
		fmt.Sprintf("holiday %q added on %s (%s)", h.Name, h.Date, h.Behaviour),
		map[string]any{"name": h.Name, "date": h.Date, "behaviour": h.Behaviour, "siteId": h.SiteId})
	a.ruleChanged(r.Context(), actor, fmt.Sprintf("holiday %q added on %s (%s)",
		h.Name, h.Date, h.Behaviour))
	controllers.SendResult(w, h, "succeed")
}

// deleteHoliday removes a day from the calendar — which REOPENS the building on it. Audited for the
// same reason the create is, and arguably more: closing a site early is embarrassing, and opening
// one that was meant to be shut is the incident.
func (a *accessRulesApi) deleteHoliday(w http.ResponseWriter, r *http.Request) {
	_, actor, ok := requireAdmin(w, r)
	if !ok {
		return
	}
	id := pathID(r, "id")
	h, err := a.holidays.GetById(r.Context(), "", uint64(id))
	if err != nil || h == nil {
		controllers.SendError(w, controllers.ErrNotFound, "no such holiday")
		return
	}
	if _, err := a.holidays.Delete(r.Context(), "", []sqldataenums.Filter{eq("Id", id)}); err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	// Deleting a holiday REOPENS a site that was meant to be shut. Of every row in this trail this
	// is the one most likely to be the answer to "why was the building open that day".
	a.audit.Success(r, services.ActionHolidayDelete, services.TargetHoliday, ID(h.Id),
		fmt.Sprintf("holiday %q on %s removed — the site is no longer closed that day", h.Name, h.Date),
		map[string]any{"name": h.Name, "date": h.Date, "behaviour": h.Behaviour, "siteId": h.SiteId})
	a.ruleChanged(r.Context(), actor, fmt.Sprintf("holiday %q on %s removed", h.Name, h.Date))
	controllers.SendResult(w, map[string]any{"deleted": true}, "succeed")
}

// --- grants -------------------------------------------------------------------------------------

// grantRow is a grant with the names an operator actually reads. The raw ids stay for the UI.
type grantRow struct {
	entities.Grant
	GroupName    string `json:"groupName"`
	DoorName     string `json:"doorName"`
	ScheduleName string `json:"scheduleName"`
}

func (a *accessRulesApi) listGrants(w http.ResponseWriter, r *http.Request) {
	rows, total, err := a.grants.Get(r.Context(), "", 1000, 0, nil, nil)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	out := make([]grantRow, 0, len(rows))
	for _, gr := range rows {
		row := grantRow{Grant: *gr}
		if g, err := a.groups.GetById(r.Context(), "", uint64(gr.GroupId)); err == nil && g != nil {
			row.GroupName = g.Name
		}
		if d, err := a.doors.GetById(r.Context(), "", uint64(gr.DoorId)); err == nil && d != nil {
			row.DoorName = d.Name
		}
		if s, err := a.schedules.GetById(r.Context(), "", uint64(gr.ScheduleId)); err == nil && s != nil {
			row.ScheduleName = s.Name
		}
		out = append(out, row)
	}
	controllers.SendResult(w, map[string]any{"items": out, "total": total}, "succeed")
}

func (a *accessRulesApi) createGrant(w http.ResponseWriter, r *http.Request) {
	actorId, actor, ok := requireAdmin(w, r)
	if !ok {
		return
	}
	var body struct {
		GroupId    int64 `json:"groupId"`
		DoorId     int64 `json:"doorId"`
		ScheduleId int64 `json:"scheduleId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	// All three legs must exist BEFORE the row is written. A grant pointing at a deleted door is
	// not dangerous, but a grant pointing at a mistyped id looks configured and does nothing —
	// the half-configured state this app keeps refusing to create.
	group, err := a.groups.GetById(r.Context(), "", uint64(body.GroupId))
	if err != nil || group == nil {
		controllers.SendError(w, controllers.ErrNotFound, "no such group")
		return
	}
	door, err := a.doors.GetById(r.Context(), "", uint64(body.DoorId))
	if err != nil || door == nil {
		controllers.SendError(w, controllers.ErrNotFound, "no such door")
		return
	}
	sched, err := a.schedules.GetById(r.Context(), "", uint64(body.ScheduleId))
	if err != nil || sched == nil {
		controllers.SendError(w, controllers.ErrNotFound, "no such schedule")
		return
	}
	// A duplicate grant is a no-op returned as the existing row: grants are additive (OR), so two
	// identical rows change nothing except an auditor's confidence.
	if rows, _, _ := a.grants.Get(r.Context(), "", 1, 0, []sqldataenums.Filter{
		eq("GroupId", body.GroupId), eq("DoorId", body.DoorId), eq("ScheduleId", body.ScheduleId),
	}, nil); len(rows) > 0 {
		controllers.SendResult(w, rows[0], "succeed")
		return
	}
	gr := entities.Grant{
		GroupId: body.GroupId, DoorId: body.DoorId, ScheduleId: body.ScheduleId,
		CreatedBy: actorId, CreatedAt: time.Now().Unix(),
	}
	id, err := a.grants.Create(r.Context(), "", gr)
	if err != nil {
		controllers.SendError(w, controllers.ErrConflict, err.Error())
		return
	}
	gr.Id = int64(id)
	// THE row this whole file exists for. A grant is the sentence "these people may open this door
	// at these hours", and until now the only trace it left was its effect: ordinary green badge
	// events, weeks later, on a door somebody was never meant to reach.
	a.audit.Success(r, services.ActionGrantCreate, services.TargetGrant, ID(gr.Id),
		fmt.Sprintf("group %q granted door %q on schedule %q", group.Name, door.Name, sched.Name),
		map[string]any{
			"groupId": group.Id, "groupName": group.Name,
			"doorId": door.Id, "doorName": door.Name,
			"scheduleId": sched.Id, "scheduleName": sched.Name, "scheduleAlways": sched.Always,
		})
	a.ruleChanged(r.Context(), actor,
		fmt.Sprintf("group %q granted door %q on schedule %q", group.Name, door.Name, sched.Name))
	controllers.SendResult(w, gr, "succeed")
}

func (a *accessRulesApi) deleteGrant(w http.ResponseWriter, r *http.Request) {
	_, actor, ok := requireAdmin(w, r)
	if !ok {
		return
	}
	id := pathID(r, "id")
	gr, err := a.grants.GetById(r.Context(), "", uint64(id))
	if err != nil || gr == nil {
		controllers.SendError(w, controllers.ErrNotFound, "no such grant")
		return
	}
	if _, err := a.grants.Delete(r.Context(), "", []sqldataenums.Filter{eq("Id", id)}); err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	groupName, doorName := fmt.Sprintf("group %d", gr.GroupId), fmt.Sprintf("door %d", gr.DoorId)
	if g, err := a.groups.GetById(r.Context(), "", uint64(gr.GroupId)); err == nil && g != nil {
		groupName = g.Name
	}
	if d, err := a.doors.GetById(r.Context(), "", uint64(gr.DoorId)); err == nil && d != nil {
		doorName = d.Name
	}
	a.audit.Success(r, services.ActionGrantDelete, services.TargetGrant, ID(gr.Id),
		fmt.Sprintf("grant revoked: %s no longer reaches %s", groupName, doorName),
		map[string]any{
			"groupId": gr.GroupId, "groupName": groupName,
			"doorId": gr.DoorId, "doorName": doorName, "scheduleId": gr.ScheduleId,
		})
	a.ruleChanged(r.Context(), actor,
		fmt.Sprintf("grant revoked: %s no longer reaches %s", groupName, doorName))
	controllers.SendResult(w, map[string]any{"deleted": true}, "succeed")
}
