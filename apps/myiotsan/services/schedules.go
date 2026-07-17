package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/mysayasan/kopiv2/apps/myiotsan/entities"
	sharedentities "github.com/mysayasan/kopiv2/domain/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// locationKey is the RuntimeSetting under which the site's latitude/longitude live. Location is
// operator-set runtime data (someone picks it on a map), not deploy-time infra, so it belongs in the
// settings store, not config.json.
const locationKey = "site.location"

// ScheduleService fires scenes and commands on the CLOCK — the automation a telemetry rule cannot
// express, because its trigger is a time (or sunrise/sunset), not a reading.
//
// It fires through the same actuation path everything else does: a scheduled scene run and a
// scheduled command both flow through SceneService.Run / CommandService.Issue, so every gate still
// applies. A schedule can command nothing an admin has not made commandable. The firing actor is a
// synthetic "schedule:<name>" (id 0), so the audit trail attributes it, never "System".
type ScheduleService struct {
	schedules dbsql.IGenericRepo[entities.Schedule]
	settings  dbsql.IGenericRepo[sharedentities.RuntimeSetting]
	scenes    *SceneService
	commands  commandIssuer
	logf      func(format string, args ...any)
}

func NewScheduleService(db dbsql.IDbCrud, scenes *SceneService, commands *CommandService, logf func(string, ...any)) *ScheduleService {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &ScheduleService{
		schedules: dbsql.NewGenericRepo[entities.Schedule](db),
		settings:  dbsql.NewGenericRepo[sharedentities.RuntimeSetting](db),
		scenes:    scenes,
		commands:  commands,
		logf:      logf,
	}
}

// SaveScheduleRequest creates or replaces a schedule.
type SaveScheduleRequest struct {
	Name          string  `json:"name"`
	Enabled       bool    `json:"enabled"`
	TriggerType   string  `json:"triggerType"`
	TimeOfDay     string  `json:"timeOfDay"`
	OffsetMinutes int     `json:"offsetMinutes"`
	Days          string  `json:"days"`
	TargetType    string  `json:"targetType"`
	SceneId       int64   `json:"sceneId"`
	DeviceId      int64   `json:"deviceId"`
	CommandName   string  `json:"commandName"`
	Value         float64 `json:"value"`
}

func (s *ScheduleService) List(ctx context.Context) ([]*entities.Schedule, error) {
	rows, _, err := s.schedules.Get(ctx, "", 500, 0, nil,
		[]sqldataenums.Sorter{{FieldName: "Name", Sort: sqldataenums.ASC}})
	if err != nil && isNoResultErr(err) {
		return nil, nil
	}
	return rows, err
}

func (s *ScheduleService) validate(ctx context.Context, req *SaveScheduleRequest) error {
	if strings.TrimSpace(req.Name) == "" {
		return fmt.Errorf("a schedule needs a name")
	}
	switch strings.ToLower(strings.TrimSpace(req.TriggerType)) {
	case "clock":
		if _, ok := parseClock(req.TimeOfDay); !ok {
			return fmt.Errorf("a clock schedule needs a valid time of day (HH:MM)")
		}
	case "sunrise", "sunset":
		if _, _, ok := s.GetLocation(ctx); !ok {
			return fmt.Errorf("set the site location first — a sunrise/sunset schedule needs a latitude and longitude to compute the sun's times")
		}
	default:
		return fmt.Errorf("trigger must be clock, sunrise or sunset")
	}
	switch strings.ToLower(strings.TrimSpace(req.TargetType)) {
	case "scene":
		if req.SceneId <= 0 {
			return fmt.Errorf("choose a scene for this schedule to run")
		}
	case "command":
		if req.DeviceId <= 0 || strings.TrimSpace(req.CommandName) == "" {
			return fmt.Errorf("choose a device and command for this schedule to run")
		}
	default:
		return fmt.Errorf("target must be a scene or a command")
	}
	return nil
}

func (s *ScheduleService) Create(ctx context.Context, req SaveScheduleRequest, actor int64) (*entities.Schedule, error) {
	if err := s.validate(ctx, &req); err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	row := s.fromRequest(req)
	row.CreatedBy, row.CreatedAt, row.UpdatedBy, row.UpdatedAt = actor, now, actor, now
	id, err := s.schedules.Create(ctx, "", row)
	if err != nil {
		return nil, err
	}
	row.Id = int64(id)
	return &row, nil
}

func (s *ScheduleService) Update(ctx context.Context, id int64, req SaveScheduleRequest, actor int64) (*entities.Schedule, error) {
	if err := s.validate(ctx, &req); err != nil {
		return nil, err
	}
	existing, err := s.schedules.GetById(ctx, "", uint64(id))
	if err != nil || existing == nil {
		return nil, fmt.Errorf("schedule not found")
	}
	row := s.fromRequest(req)
	row.Id = id
	row.CreatedBy, row.CreatedAt = existing.CreatedBy, existing.CreatedAt
	row.LastFiredAt = existing.LastFiredAt // keep the double-fire guard across an edit
	row.UpdatedBy, row.UpdatedAt = actor, time.Now().Unix()
	if _, err := s.schedules.UpdateById(ctx, "", row); err != nil {
		return nil, err
	}
	return &row, nil
}

func (s *ScheduleService) Delete(ctx context.Context, id int64) error {
	_, err := s.schedules.DeleteById(ctx, "", uint64(id))
	return err
}

func (s *ScheduleService) fromRequest(req SaveScheduleRequest) entities.Schedule {
	return entities.Schedule{
		Name:          strings.TrimSpace(req.Name),
		Enabled:       req.Enabled,
		TriggerType:   strings.ToLower(strings.TrimSpace(req.TriggerType)),
		TimeOfDay:     strings.TrimSpace(req.TimeOfDay),
		OffsetMinutes: req.OffsetMinutes,
		Days:          strings.TrimSpace(req.Days),
		TargetType:    strings.ToLower(strings.TrimSpace(req.TargetType)),
		SceneId:       req.SceneId,
		DeviceId:      req.DeviceId,
		CommandName:   strings.TrimSpace(req.CommandName),
		Value:         req.Value,
	}
}

// --- location (RuntimeSetting) --------------------------------------------------------------

type siteLocation struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

// GetLocation returns the site latitude/longitude, ok=false if it was never set.
func (s *ScheduleService) GetLocation(ctx context.Context) (lat, lon float64, ok bool) {
	row, err := s.settings.GetByUnique(ctx, "", "key", locationKey)
	if err != nil || row == nil || strings.TrimSpace(row.Value) == "" {
		return 0, 0, false
	}
	var loc siteLocation
	if json.Unmarshal([]byte(row.Value), &loc) != nil {
		return 0, 0, false
	}
	return loc.Latitude, loc.Longitude, true
}

// SetLocation stores the site latitude/longitude, upserting the single setting row.
func (s *ScheduleService) SetLocation(ctx context.Context, lat, lon float64, actor int64) error {
	if lat < -90 || lat > 90 || lon < -180 || lon > 180 {
		return fmt.Errorf("latitude must be -90..90 and longitude -180..180")
	}
	val, _ := json.Marshal(siteLocation{Latitude: lat, Longitude: lon})
	now := time.Now().Unix()
	row, err := s.settings.GetByUnique(ctx, "", "key", locationKey)
	if err != nil && !isNoResultErr(err) {
		return err
	}
	if row == nil {
		_, err = s.settings.Create(ctx, "", sharedentities.RuntimeSetting{
			Key: locationKey, Value: string(val), CreatedBy: actor, CreatedAt: now, UpdatedBy: actor, UpdatedAt: now,
		})
		return err
	}
	row.Value = string(val)
	row.UpdatedBy, row.UpdatedAt = actor, now
	_, err = s.settings.UpdateById(ctx, "", *row)
	return err
}

// --- evaluation & firing --------------------------------------------------------------------

// fireInstant resolves the local wall-clock instant a schedule should fire ON THE DAY of `day`. For
// a clock trigger that is TimeOfDay; for a sun trigger it is local sunrise/sunset shifted by the
// offset. ok is false when the time cannot be computed (bad clock, missing location, polar day).
func (s *ScheduleService) fireInstant(sched *entities.Schedule, day time.Time, lat, lon float64, haveLoc bool) (time.Time, bool) {
	loc := day.Location()
	y, m, d := day.Date()
	switch sched.TriggerType {
	case "clock":
		mins, ok := parseClock(sched.TimeOfDay)
		if !ok {
			return time.Time{}, false
		}
		return time.Date(y, m, d, mins/60, mins%60, 0, 0, loc), true
	case "sunrise", "sunset":
		if !haveLoc {
			return time.Time{}, false
		}
		rise, set, ok := SunriseSunset(day, lat, lon)
		if !ok {
			return time.Time{}, false
		}
		base := rise
		if sched.TriggerType == "sunset" {
			base = set
		}
		return base.In(loc).Add(time.Duration(sched.OffsetMinutes) * time.Minute), true
	}
	return time.Time{}, false
}

// DueAt reports whether a schedule should fire in the minute of `now`: its weekday matches and its
// resolved fire instant falls in the same minute. Matching is minute-granular because the scheduler
// ticks once a minute.
func (s *ScheduleService) DueAt(sched *entities.Schedule, now time.Time, lat, lon float64, haveLoc bool) bool {
	if !sched.Enabled || !daysMatch(sched.Days, now.Weekday()) {
		return false
	}
	inst, ok := s.fireInstant(sched, now, lat, lon, haveLoc)
	if !ok {
		return false
	}
	return inst.Truncate(time.Minute).Equal(now.Truncate(time.Minute))
}

// Tick fires every due schedule once for the minute of `now`. The double-fire guard is LastFiredAt
// rounded to the unix-minute and PERSISTED: a tick that runs twice in a minute, or a restart inside
// the firing minute, cannot fire a schedule a second time (the cooldown lesson, applied to time).
func (s *ScheduleService) Tick(ctx context.Context, now time.Time) {
	rows, err := s.List(ctx)
	if err != nil {
		s.logf("scheduler: list schedules: %v", err)
		return
	}
	lat, lon, haveLoc := s.GetLocation(ctx)
	thisMin := now.Unix() / 60
	for _, sc := range rows {
		if !sc.Enabled || sc.LastFiredAt == thisMin {
			continue
		}
		if !s.DueAt(sc, now, lat, lon, haveLoc) {
			continue
		}
		sc.LastFiredAt = thisMin
		if _, err := s.schedules.UpdateById(ctx, "", *sc); err != nil {
			// If we cannot persist the guard, do NOT fire — better a missed fire than a double one.
			s.logf("scheduler: persist last-fired for %q, skipping to avoid a double fire: %v", sc.Name, err)
			continue
		}
		s.fire(ctx, sc)
	}
}

// RunNow test-fires a schedule immediately, ignoring its trigger — the "test" button. It still goes
// through the gated actuation path, so a test cannot do anything the schedule itself could not.
func (s *ScheduleService) RunNow(ctx context.Context, id int64, actor int64, actorName string) error {
	sc, err := s.schedules.GetById(ctx, "", uint64(id))
	if err != nil || sc == nil {
		return fmt.Errorf("schedule not found")
	}
	s.fireAs(ctx, sc, actor, actorName)
	return nil
}

func (s *ScheduleService) fire(ctx context.Context, sc *entities.Schedule) {
	// A scheduled fire has no user behind it: actor id 0, name "schedule:<name>" so the audit trail
	// attributes it rather than reading "System".
	s.fireAs(ctx, sc, 0, "schedule:"+sc.Name)
}

func (s *ScheduleService) fireAs(ctx context.Context, sc *entities.Schedule, actor int64, actorName string) {
	switch sc.TargetType {
	case "scene":
		if _, err := s.scenes.Run(ctx, sc.SceneId, actor, actorName); err != nil {
			s.logf("schedule %q: run scene: %v", sc.Name, err)
		}
	case "command":
		if _, err := s.commands.Issue(ctx, sc.DeviceId, IssueRequest{Name: sc.CommandName, Value: sc.Value}, actor, actorName); err != nil {
			s.logf("schedule %q: issue command: %v", sc.Name, err)
		}
	}
	s.logf("fired schedule %q (%s)", sc.Name, sc.TargetType)
}

// daysMatch reports whether wd is in a comma-separated weekday list (0=Sunday..6=Saturday). An empty
// list means every day.
func daysMatch(days string, wd time.Weekday) bool {
	days = strings.TrimSpace(days)
	if days == "" {
		return true
	}
	for _, part := range strings.Split(days, ",") {
		if d, err := strconv.Atoi(strings.TrimSpace(part)); err == nil && d == int(wd) {
			return true
		}
	}
	return false
}
