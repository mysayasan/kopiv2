package services

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/mysayasan/kopiv2/apps/myiotsan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	"github.com/mysayasan/kopiv2/domain/notification"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// RuleService owns the rules, drives the evaluator, and turns a decision into an alert and a
// notification.
//
// It caches the rule set in memory and indexes it by key, because OnReading is called for
// EVERY sample of every device: re-reading the rules from the database per sample would put a
// query on the hot path, which is the one thing the ingest design refuses to do.
type RuleService struct {
	rules   dbsql.IGenericRepo[entities.IotRule]
	alerts  dbsql.IGenericRepo[entities.AlertEvent]
	engine  *RuleEngine
	series  *TelemetryService
	notify  *notification.Service
	devices *DeviceService
	logf    func(format string, args ...any)

	mu     sync.RWMutex
	cached []*entities.IotRule
}

func NewRuleService(
	db dbsql.IDbCrud,
	engine *RuleEngine,
	series *TelemetryService,
	notify *notification.Service,
	devices *DeviceService,
	logf func(string, ...any),
) *RuleService {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &RuleService{
		rules:   dbsql.NewGenericRepo[entities.IotRule](db),
		alerts:  dbsql.NewGenericRepo[entities.AlertEvent](db),
		engine:  engine,
		series:  series,
		notify:  notify,
		devices: devices,
		logf:    logf,
	}
}

// Reload refreshes the rule cache and re-seeds every cooldown from the database.
//
// The seeding is the whole reason this is not just a cache refresh. Without it the cooldown
// resets on every restart, and an appliance that reboots re-fires every rule that is still
// true — the alert storm mymatasan actually shipped.
//
// The cooldown is per (rule, DEVICE), and it is seeded FROM THE ALERT LOG rather than from a
// column on the rule. That is not a shortcut — it is the correct source. An alert row already
// records exactly when a rule last fired on a given device, so the log IS the durable record of
// the cooldown and there is no second table to keep in step with it. A rule-wide
// LastTriggeredAt column cannot express this at all: a tag-scoped rule over ten fridges needs
// ten independent cooldowns, and collapsing them into one would let fridge A's alert suppress
// fridge B's.
func (s *RuleService) Reload(ctx context.Context) error {
	rows, _, err := s.rules.Get(ctx, "", 500, 0, nil,
		[]sqldataenums.Sorter{{FieldName: "Id", Sort: sqldataenums.ASC}})
	if err != nil && !isNoResultErr(err) {
		return err
	}

	// Seed each (rule, device) cooldown from the most recent alert for that pair. The recent
	// tail is enough: an alert older than the longest sane cooldown cannot suppress anything.
	recent, _, aerr := s.alerts.Get(ctx, "", 2000, 0, nil,
		[]sqldataenums.Sorter{{FieldName: "CreatedAt", Sort: sqldataenums.DESC}})
	if aerr != nil && !isNoResultErr(aerr) {
		return aerr
	}
	seeded := map[[2]int64]bool{}
	for _, a := range recent {
		k := [2]int64{a.RuleId, a.DeviceId}
		if seeded[k] {
			continue // newest-first, so the first row for a pair is the one that counts
		}
		seeded[k] = true
		s.engine.SeedCooldown(a.RuleId, a.DeviceId, a.CreatedAt)
	}

	s.mu.Lock()
	s.cached = rows
	s.mu.Unlock()
	return nil
}

func (s *RuleService) List(ctx context.Context) ([]*entities.IotRule, error) {
	s.mu.RLock()
	cached := s.cached
	s.mu.RUnlock()
	if cached != nil {
		return cached, nil
	}
	if err := s.Reload(ctx); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cached, nil
}

// SaveRuleRequest is the create/update body.
type SaveRuleRequest struct {
	Name               string  `json:"name"`
	Enabled            bool    `json:"enabled"`
	DeviceId           int64   `json:"deviceId"`
	Tag                string  `json:"tag"`
	Key                string  `json:"key"`
	Condition          string  `json:"condition"`
	Threshold          float64 `json:"threshold"`
	Hysteresis         float64 `json:"hysteresis"`
	ConsecutiveSamples int     `json:"consecutiveSamples"`
	WindowSeconds      int     `json:"windowSeconds"`
	CooldownSeconds    int     `json:"cooldownSeconds"`
	SchedulePolicy     string  `json:"schedulePolicy"`
	ScheduleStart      string  `json:"scheduleStart"`
	ScheduleEnd        string  `json:"scheduleEnd"`
	ScheduleDays       string  `json:"scheduleDays"`
	Severity           string  `json:"severity"`
}

// validConditions is the closed set. An unknown condition would silently never fire, and a rule
// that silently never fires is the most dangerous thing a monitoring product can contain — the
// operator believes they are covered.
var validConditions = map[string]bool{
	"above": true, "below": true, "equals": true,
	"delta": true, "rate": true, "stuck": true, "offline": true,
}

func (s *RuleService) validate(req SaveRuleRequest) error {
	cond := strings.ToLower(strings.TrimSpace(req.Condition))
	if !validConditions[cond] {
		return fmt.Errorf("unknown condition %q", req.Condition)
	}
	if cond != "offline" && strings.TrimSpace(req.Key) == "" {
		return fmt.Errorf("condition %q watches a telemetry key, so one must be chosen", cond)
	}
	if (cond == "delta" || cond == "rate" || cond == "stuck" || cond == "offline") && req.WindowSeconds <= 0 {
		return fmt.Errorf("condition %q needs a window to measure across", cond)
	}
	return nil
}

func (s *RuleService) Create(ctx context.Context, req SaveRuleRequest, actor int64) (*entities.IotRule, error) {
	if err := s.validate(req); err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	r := entities.IotRule{
		Name:               strings.TrimSpace(req.Name),
		Enabled:            req.Enabled,
		DeviceId:           req.DeviceId,
		Tag:                strings.TrimSpace(req.Tag),
		Key:                strings.TrimSpace(req.Key),
		Condition:          strings.ToLower(strings.TrimSpace(req.Condition)),
		Threshold:          req.Threshold,
		Hysteresis:         req.Hysteresis,
		ConsecutiveSamples: req.ConsecutiveSamples,
		WindowSeconds:      req.WindowSeconds,
		CooldownSeconds:    req.CooldownSeconds,
		SchedulePolicy:     defaultString(req.SchedulePolicy, "always"),
		ScheduleStart:      req.ScheduleStart,
		ScheduleEnd:        req.ScheduleEnd,
		ScheduleDays:       req.ScheduleDays,
		Severity:           defaultString(req.Severity, "warning"),
		CreatedBy:          actor,
		CreatedAt:          now,
		UpdatedBy:          actor,
		UpdatedAt:          now,
	}
	id, err := s.rules.Create(ctx, "", r)
	if err != nil {
		return nil, err
	}
	r.Id = int64(id)
	return &r, s.Reload(ctx)
}

func (s *RuleService) Update(ctx context.Context, id int64, req SaveRuleRequest, actor int64) (*entities.IotRule, error) {
	if err := s.validate(req); err != nil {
		return nil, err
	}
	existing, err := s.rules.GetById(ctx, "", uint64(id))
	if err != nil || existing == nil {
		return nil, fmt.Errorf("rule not found")
	}
	existing.Name = strings.TrimSpace(req.Name)
	existing.Enabled = req.Enabled
	existing.DeviceId = req.DeviceId
	existing.Tag = strings.TrimSpace(req.Tag)
	existing.Key = strings.TrimSpace(req.Key)
	existing.Condition = strings.ToLower(strings.TrimSpace(req.Condition))
	existing.Threshold = req.Threshold
	existing.Hysteresis = req.Hysteresis
	existing.ConsecutiveSamples = req.ConsecutiveSamples
	existing.WindowSeconds = req.WindowSeconds
	existing.CooldownSeconds = req.CooldownSeconds
	existing.SchedulePolicy = defaultString(req.SchedulePolicy, "always")
	existing.ScheduleStart = req.ScheduleStart
	existing.ScheduleEnd = req.ScheduleEnd
	existing.ScheduleDays = req.ScheduleDays
	existing.Severity = defaultString(req.Severity, "warning")
	existing.UpdatedBy = actor
	existing.UpdatedAt = time.Now().Unix()
	if _, err := s.rules.UpdateById(ctx, "", *existing); err != nil {
		return nil, err
	}
	// Drop the evaluator's memory: a debounce counter half-filled against the OLD thresholds
	// means nothing under the new ones, and carrying it over could fire immediately on an edit.
	s.engine.Forget(id)
	return existing, s.Reload(ctx)
}

func (s *RuleService) Delete(ctx context.Context, id int64) error {
	if _, err := s.rules.DeleteById(ctx, "", uint64(id)); err != nil {
		return err
	}
	s.engine.Forget(id)
	return s.Reload(ctx)
}

// OnReading is called for EVERY decoded sample — including the ones the deadband suppressed.
//
// That is deliberate and it matters: the deadband is a STORAGE decision. A value that has sat
// three degrees above the limit without moving is not worth another row, but it is absolutely
// worth an alert. Evaluating rules only on stored readings would mean a steady overheat is
// never alerted on, which would be the worst bug this app could contain.
func (s *RuleService) OnReading(ctx context.Context, dev *entities.IotDevice, key string, value float64, nowSec int64) {
	for _, rule := range s.rulesFor(dev, key) {
		obs := Observation{DeviceId: dev.Id, Value: value, Now: nowSec}

		// delta/rate/stuck need the value at the start of the window. This IS a query, but only
		// for the handful of rules that use those conditions — a threshold rule, which is the
		// overwhelming majority, never touches the database here.
		switch rule.Condition {
		case "delta", "rate", "stuck":
			prev, at, ok := s.series.ValueAt(ctx, dev.Id, key, nowSec-int64(rule.WindowSeconds))
			if !ok {
				continue
			}
			obs.Previous = prev
			obs.HasPrevious = true
			obs.ElapsedSeconds = float64(nowSec - at)
		}

		if d := s.engine.Evaluate(*rule, obs); d.Fire {
			s.fire(ctx, rule, dev, key, value, d.Reason, nowSec)
		}
	}
}

// rulesFor selects the rules watching this device and key, honoring the scope ladder: a rule
// names one device, or a tag, or (naming neither) every device that reports the key.
func (s *RuleService) rulesFor(dev *entities.IotDevice, key string) []*entities.IotRule {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]*entities.IotRule, 0, 4)
	for _, r := range s.cached {
		if !r.Enabled || r.Condition == "offline" { // offline is driven by the sweep, not by a reading
			continue
		}
		if r.Key != key {
			continue
		}
		switch {
		case r.DeviceId > 0:
			if r.DeviceId != dev.Id {
				continue
			}
		case r.Tag != "":
			if !strings.EqualFold(r.Tag, dev.Tag) {
				continue
			}
		}
		out = append(out, r)
	}
	return out
}

// fire records the alert and pushes the notification.
//
// The alert is written STRAIGHT TO DISK, not through the batched reading writer. Readings are
// a sampled, redundant signal and losing 250ms of them in a crash costs nothing; an alert lost
// in a buffer during the very crash it was warning about is worse than useless.
func (s *RuleService) fire(ctx context.Context, rule *entities.IotRule, dev *entities.IotDevice, key string, value float64, reason string, nowSec int64) {
	message := reason
	if message == "" {
		message = fmt.Sprintf("%s triggered on %s", rule.Name, dev.Name)
	}

	alert := entities.AlertEvent{
		RuleId:     rule.Id,
		DeviceId:   dev.Id,
		RuleName:   rule.Name,
		DeviceName: dev.Name,
		Key:        key,
		Value:      value,
		Message:    message,
		Severity:   defaultString(rule.Severity, "warning"),
		CreatedAt:  nowSec,
	}
	if _, err := s.alerts.Create(ctx, "", alert); err != nil {
		s.logf("could not record alert for rule %q: %v", rule.Name, err)
	}

	// Persist the cooldown. If this write is lost the rule re-fires after a restart — the exact
	// storm SeedCooldown exists to prevent — so it is worth its own error line.
	// Kept for the UI ("when did this rule last fire?"). The DURABLE cooldown is the alert row
	// written just above — see Reload.
	rule.LastTriggeredAt = s.engine.LastTriggered(rule.Id, dev.Id)
	if _, err := s.rules.UpdateById(ctx, "", *rule); err != nil {
		s.logf("could not persist the cooldown for rule %q — it may re-fire after a restart: %v", rule.Name, err)
	}

	if s.notify != nil {
		s.notify.Publish(ctx, notification.Notification{
			Category: notification.CategoryDeviceAlert,
			Severity: severityOf(rule.Severity),
			Title:    rule.Name,
			Body:     fmt.Sprintf("%s: %s", dev.Name, message),
			Source:   "device:" + dev.DeviceKey,
			Data: map[string]any{
				"deviceId":   dev.Id,
				"deviceName": dev.Name,
				"ruleId":     rule.Id,
				"key":        key,
				"value":      value,
			},
		})
	}
}

func severityOf(s string) notification.Severity {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "critical":
		return notification.Critical
	case "info":
		return notification.Info
	default:
		return notification.Warning
	}
}

// SweepOffline fires the "offline" rules. They cannot be driven by a reading, because their
// whole subject is the ABSENCE of readings — a device that has gone silent will never call
// OnReading again, which is precisely the event. A dead sensor that nobody notices is a
// monitoring system that is lying to you, so this sweep is what makes silence audible.
func (s *RuleService) SweepOffline(ctx context.Context) {
	nowSec := time.Now().Unix()

	s.mu.RLock()
	rules := append([]*entities.IotRule(nil), s.cached...)
	s.mu.RUnlock()

	var offline []*entities.IotRule
	for _, r := range rules {
		if r.Enabled && r.Condition == "offline" {
			offline = append(offline, r)
		}
	}
	if len(offline) == 0 {
		return
	}

	devices, _, err := s.devices.List(ctx, 1000, 0)
	if err != nil {
		return
	}

	for _, dev := range devices {
		if !dev.Enabled {
			continue
		}
		silent := float64(nowSec - dev.LastSeenAt)
		if dev.LastSeenAt == 0 {
			// Never heard from at all. Only meaningful once it has been given a chance, so use
			// the device's creation time as the clock's start.
			silent = float64(nowSec - dev.CreatedAt)
		}
		for _, rule := range offline {
			if rule.DeviceId > 0 && rule.DeviceId != dev.Id {
				continue
			}
			if rule.Tag != "" && !strings.EqualFold(rule.Tag, dev.Tag) {
				continue
			}
			obs := Observation{DeviceId: dev.Id, SilentSeconds: silent, Now: nowSec}
			if d := s.engine.Evaluate(*rule, obs); d.Fire {
				_ = s.devices.MarkHealth(ctx, dev.Id, "offline")
				s.fire(ctx, rule, dev, "", silent, d.Reason, nowSec)
			}
		}
	}
}

// --- alerts ---------------------------------------------------------------------------

func (s *RuleService) ListAlerts(ctx context.Context, deviceId int64, limit, offset uint64) ([]*entities.AlertEvent, uint64, error) {
	if limit == 0 {
		limit = 100
	}
	var filters []sqldataenums.Filter
	if deviceId > 0 {
		filters = append(filters, sqldataenums.Filter{FieldName: "DeviceId", Compare: sqldataenums.Equal, Value: deviceId})
	}
	rows, total, err := s.alerts.Get(ctx, "", limit, offset, filters,
		[]sqldataenums.Sorter{{FieldName: "CreatedAt", Sort: sqldataenums.DESC}})
	if err != nil && isNoResultErr(err) {
		return []*entities.AlertEvent{}, 0, nil
	}
	return rows, total, err
}

// AckAlert marks an alert as seen. Acknowledging is an OPERATOR power; deleting is not — the
// same evidentiary line mymatasan draws, and the reason there is no DeleteAlert here.
func (s *RuleService) AckAlert(ctx context.Context, id int64, actor int64) error {
	alert, err := s.alerts.GetById(ctx, "", uint64(id))
	if err != nil || alert == nil {
		return fmt.Errorf("alert not found")
	}
	alert.AckedAt = time.Now().Unix()
	alert.AckedBy = actor
	_, err = s.alerts.UpdateById(ctx, "", *alert)
	return err
}
