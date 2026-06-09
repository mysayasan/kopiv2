package vision

import (
	"strings"
	"testing"
	"time"
)

func TestRuleActiveAtWeeklyWindows(t *testing.T) {
	rule := DetectionRule{
		SchedulePolicy: `{"timezone":"UTC","mode":"allow","windows":[{"days":["mon","tue","wed","thu","fri"],"start":"08:00","end":"18:00"}]}`,
	}

	activeAt := mustTime(t, "2026-06-08T09:30:00Z")
	inactiveAt := mustTime(t, "2026-06-08T19:30:00Z")

	if active, reason := RuleActiveAt(rule, activeAt); !active || reason != "inside_schedule" {
		t.Fatalf("RuleActiveAt active = %v, reason = %q", active, reason)
	}
	if active, reason := RuleActiveAt(rule, inactiveAt); active || reason != "outside_schedule" {
		t.Fatalf("RuleActiveAt inactive = %v, reason = %q", active, reason)
	}
}

func TestRuleActiveAtOvernightWindowUsesPreviousDay(t *testing.T) {
	rule := DetectionRule{
		SchedulePolicy: `{"timezone":"UTC","mode":"allow","windows":[{"days":["mon"],"start":"18:00","end":"07:00"}]}`,
	}

	tuesdayMorning := mustTime(t, "2026-06-09T02:30:00Z")
	tuesdayEvening := mustTime(t, "2026-06-09T20:30:00Z")

	if active, reason := RuleActiveAt(rule, tuesdayMorning); !active || reason != "inside_schedule" {
		t.Fatalf("RuleActiveAt overnight active = %v, reason = %q", active, reason)
	}
	if active, reason := RuleActiveAt(rule, tuesdayEvening); active || reason != "outside_schedule" {
		t.Fatalf("RuleActiveAt overnight inactive = %v, reason = %q", active, reason)
	}
}

func TestRuleActiveAtDenyMode(t *testing.T) {
	rule := DetectionRule{
		SchedulePolicy: `{"timezone":"UTC","mode":"deny","windows":[{"days":["sat","sun"],"start":"00:00","end":"23:59"}]}`,
	}

	saturday := mustTime(t, "2026-06-13T10:00:00Z")
	monday := mustTime(t, "2026-06-15T10:00:00Z")

	if active, reason := RuleActiveAt(rule, saturday); active || reason != "blocked_by_schedule" {
		t.Fatalf("RuleActiveAt deny blocked = %v, reason = %q", active, reason)
	}
	if active, reason := RuleActiveAt(rule, monday); !active || reason != "outside_deny_schedule" {
		t.Fatalf("RuleActiveAt deny allowed = %v, reason = %q", active, reason)
	}
}

func TestRuleActiveAtDateRange(t *testing.T) {
	rule := DetectionRule{
		SchedulePolicy: `{"mode":"allow","dateRanges":[{"start":"2026-06-10T08:00:00+08:00","end":"2026-06-10T18:00:00+08:00"}]}`,
	}

	inside := mustTime(t, "2026-06-10T04:00:00Z")
	outside := mustTime(t, "2026-06-10T12:00:00Z")

	if active, reason := RuleActiveAt(rule, inside); !active || reason != "inside_schedule" {
		t.Fatalf("RuleActiveAt range active = %v, reason = %q", active, reason)
	}
	if active, reason := RuleActiveAt(rule, outside); active || reason != "outside_schedule" {
		t.Fatalf("RuleActiveAt range inactive = %v, reason = %q", active, reason)
	}
}

func TestValidateSchedulePolicyRejectsInvalidInputs(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{name: "bad json", value: "{bad", want: "schedulePolicy must be valid JSON"},
		{name: "bad timezone", value: `{"timezone":"Mars/Base"}`, want: "timezone is invalid"},
		{name: "bad mode", value: `{"mode":"maybe"}`, want: "mode must be"},
		{name: "bad day", value: `{"windows":[{"days":["holiday"],"start":"08:00","end":"18:00"}]}`, want: "invalid day"},
		{name: "bad time", value: `{"windows":[{"start":"8am","end":"18:00"}]}`, want: "start must use HH:MM"},
		{name: "bad range", value: `{"dateRanges":[{"start":"2026-06-10T18:00:00Z","end":"2026-06-10T08:00:00Z"}]}`, want: "end must be after start"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSchedulePolicy(tt.value)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("ValidateSchedulePolicy() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func mustTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}
