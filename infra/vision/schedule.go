package vision

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	ScheduleModeAllow = "allow"
	ScheduleModeDeny  = "deny"
)

type SchedulePolicy struct {
	Preset     string              `json:"preset,omitempty"`
	Timezone   string              `json:"timezone,omitempty"`
	Mode       string              `json:"mode,omitempty"`
	Windows    []ScheduleWindow    `json:"windows,omitempty"`
	DateRanges []ScheduleDateRange `json:"dateRanges,omitempty"`
}

type ScheduleWindow struct {
	Days  []string `json:"days,omitempty"`
	Start string   `json:"start"`
	End   string   `json:"end"`
}

type ScheduleDateRange struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

func ValidateSchedulePolicy(value string) error {
	policy, empty, err := parseSchedulePolicy(value)
	if err != nil {
		return err
	}
	if empty {
		return nil
	}
	_, err = scheduleLocation(policy)
	if err != nil {
		return err
	}
	mode := normalizedScheduleMode(policy.Mode)
	if mode == "" {
		mode = ScheduleModeAllow
	}
	if mode != ScheduleModeAllow && mode != ScheduleModeDeny {
		return fmt.Errorf("schedulePolicy mode must be %q or %q", ScheduleModeAllow, ScheduleModeDeny)
	}
	if len(policy.Windows) == 0 && len(policy.DateRanges) == 0 {
		return errors.New("schedulePolicy must include windows or dateRanges")
	}
	for idx, window := range policy.Windows {
		if _, err := parseScheduleClock(window.Start); err != nil {
			return fmt.Errorf("schedulePolicy windows[%d].start must use HH:MM: %w", idx, err)
		}
		if _, err := parseScheduleClock(window.End); err != nil {
			return fmt.Errorf("schedulePolicy windows[%d].end must use HH:MM: %w", idx, err)
		}
		if strings.TrimSpace(window.Start) == strings.TrimSpace(window.End) {
			return fmt.Errorf("schedulePolicy windows[%d] start and end cannot be the same", idx)
		}
		for _, day := range window.Days {
			if _, ok := parseScheduleDay(day); !ok {
				return fmt.Errorf("schedulePolicy windows[%d].days contains invalid day %q", idx, day)
			}
		}
	}
	for idx, item := range policy.DateRanges {
		start, err := time.Parse(time.RFC3339, strings.TrimSpace(item.Start))
		if err != nil {
			return fmt.Errorf("schedulePolicy dateRanges[%d].start must be RFC3339: %w", idx, err)
		}
		end, err := time.Parse(time.RFC3339, strings.TrimSpace(item.End))
		if err != nil {
			return fmt.Errorf("schedulePolicy dateRanges[%d].end must be RFC3339: %w", idx, err)
		}
		if !end.After(start) {
			return fmt.Errorf("schedulePolicy dateRanges[%d].end must be after start", idx)
		}
	}
	return nil
}

func RuleActiveAt(rule DetectionRule, now time.Time) (bool, string) {
	policy, empty, err := parseSchedulePolicy(rule.SchedulePolicy)
	if err != nil || empty {
		return true, "always"
	}
	matched := schedulePolicyMatches(policy, now)
	mode := normalizedScheduleMode(policy.Mode)
	if mode == "" {
		mode = ScheduleModeAllow
	}
	if mode == ScheduleModeDeny {
		if matched {
			return false, "blocked_by_schedule"
		}
		return true, "outside_deny_schedule"
	}
	if matched {
		return true, "inside_schedule"
	}
	return false, "outside_schedule"
}

func parseSchedulePolicy(value string) (SchedulePolicy, bool, error) {
	if strings.TrimSpace(value) == "" {
		return SchedulePolicy{}, true, nil
	}
	var policy SchedulePolicy
	if err := json.Unmarshal([]byte(value), &policy); err != nil {
		return SchedulePolicy{}, false, fmt.Errorf("schedulePolicy must be valid JSON: %w", err)
	}
	return policy, false, nil
}

func schedulePolicyMatches(policy SchedulePolicy, now time.Time) bool {
	location, err := scheduleLocation(policy)
	if err != nil {
		location = time.Local
	}
	localNow := now.In(location)
	for _, item := range policy.DateRanges {
		start, startErr := time.Parse(time.RFC3339, strings.TrimSpace(item.Start))
		end, endErr := time.Parse(time.RFC3339, strings.TrimSpace(item.End))
		if startErr == nil && endErr == nil && !localNow.Before(start) && localNow.Before(end) {
			return true
		}
	}
	for _, window := range policy.Windows {
		if scheduleWindowMatches(window, localNow) {
			return true
		}
	}
	return false
}

func scheduleLocation(policy SchedulePolicy) (*time.Location, error) {
	timezone := strings.TrimSpace(policy.Timezone)
	if timezone == "" || strings.EqualFold(timezone, "local") {
		return time.Local, nil
	}
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return nil, fmt.Errorf("schedulePolicy timezone is invalid: %w", err)
	}
	return location, nil
}

func scheduleWindowMatches(window ScheduleWindow, now time.Time) bool {
	start, startErr := parseScheduleClock(window.Start)
	end, endErr := parseScheduleClock(window.End)
	if startErr != nil || endErr != nil || start == end {
		return false
	}
	current := now.Hour()*60 + now.Minute()
	if start < end {
		return dayAllowed(window.Days, now.Weekday()) && current >= start && current < end
	}
	if current >= start {
		return dayAllowed(window.Days, now.Weekday())
	}
	if current < end {
		return dayAllowed(window.Days, previousWeekday(now.Weekday()))
	}
	return false
}

func parseScheduleClock(value string) (int, error) {
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) != 2 {
		return 0, errors.New("invalid clock")
	}
	parsed, err := time.Parse("15:04", fmt.Sprintf("%s:%s", parts[0], parts[1]))
	if err != nil {
		return 0, err
	}
	return parsed.Hour()*60 + parsed.Minute(), nil
}

func dayAllowed(days []string, weekday time.Weekday) bool {
	if len(days) == 0 {
		return true
	}
	for _, day := range days {
		if parsed, ok := parseScheduleDay(day); ok && parsed == weekday {
			return true
		}
	}
	return false
}

func parseScheduleDay(value string) (time.Weekday, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "sun", "sunday":
		return time.Sunday, true
	case "mon", "monday":
		return time.Monday, true
	case "tue", "tues", "tuesday":
		return time.Tuesday, true
	case "wed", "wednesday":
		return time.Wednesday, true
	case "thu", "thur", "thurs", "thursday":
		return time.Thursday, true
	case "fri", "friday":
		return time.Friday, true
	case "sat", "saturday":
		return time.Saturday, true
	default:
		return time.Sunday, false
	}
}

func previousWeekday(day time.Weekday) time.Weekday {
	if day == time.Sunday {
		return time.Saturday
	}
	return day - 1
}

func normalizedScheduleMode(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
