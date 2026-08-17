package services

import "testing"

// The digest gate decides whether THIS instance generates. Getting it wrong is not
// cosmetic: a digest is an LLM call and an operator-visible artefact, so a duplicate
// costs money and makes the morning feed look like something fired twice.
func TestDigestScheduleGate(t *testing.T) {
	cases := []struct {
		name string
		gate func() bool
		want bool
	}{
		// A single-instance install passes no gate at all and must behave exactly as it
		// did before any of this existed.
		{"no gate means always run", nil, true},
		{"leader runs", func() bool { return true }, true},
		{"follower skips", func() bool { return false }, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &digestSchedule{gate: tc.gate}
			if got := s.shouldRun(); got != tc.want {
				t.Fatalf("shouldRun() = %v, want %v", got, tc.want)
			}
		})
	}
}

// The gate must be re-read every time rather than captured once: the schedule sleeps
// for hours between runs, and leadership can move during that sleep. A cached answer
// would leave a demoted instance still generating, or a promoted one never starting.
func TestDigestScheduleGateIsReReadEachTime(t *testing.T) {
	leader := false
	s := &digestSchedule{gate: func() bool { return leader }}

	if s.shouldRun() {
		t.Fatal("a follower must not generate")
	}
	leader = true
	if !s.shouldRun() {
		t.Fatal("a promoted instance must generate without a restart")
	}
	leader = false
	if s.shouldRun() {
		t.Fatal("a demoted instance must stop generating without a restart")
	}
}
