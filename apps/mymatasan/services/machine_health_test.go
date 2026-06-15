package services

import "testing"

func TestMetricLevel(t *testing.T) {
	cases := []struct {
		pct        float64
		warn, crit int
		want       int
	}{
		{10, 85, 95, 0},
		{85, 85, 95, 1},
		{90, 85, 95, 1},
		{95, 85, 95, 2},
		{99, 85, 95, 2},
	}
	for _, c := range cases {
		if got := metricLevel(c.pct, c.warn, c.crit); got != c.want {
			t.Errorf("metricLevel(%.0f,%d,%d) = %d, want %d", c.pct, c.warn, c.crit, got, c.want)
		}
	}
}

func TestStepMachineLevelDebouncesUpAndDown(t *testing.T) {
	st := &machineMetricState{}
	// Two breaching samples (level 2) below the sustained threshold of 3: no change.
	for i := 0; i < 2; i++ {
		if changed, _ := stepMachineLevel(st, 2, 3, 2); changed {
			t.Fatalf("sample %d: unexpected change before sustained threshold", i+1)
		}
	}
	// Third consecutive breach announces critical.
	changed, level := stepMachineLevel(st, 2, 3, 2)
	if !changed || level != 2 {
		t.Fatalf("expected change to critical, got changed=%v level=%d", changed, level)
	}
	// One normal sample: below recovery threshold of 2, stays critical.
	if changed, _ := stepMachineLevel(st, 0, 3, 2); changed {
		t.Fatal("unexpected recovery on first normal sample")
	}
	// Second consecutive normal sample announces recovery to normal.
	changed, level = stepMachineLevel(st, 0, 3, 2)
	if !changed || level != 0 {
		t.Fatalf("expected recovery to normal, got changed=%v level=%d", changed, level)
	}
}

func TestStepMachineLevelResetsStreakOnReversal(t *testing.T) {
	st := &machineMetricState{}
	stepMachineLevel(st, 2, 3, 2) // up streak 1
	stepMachineLevel(st, 2, 3, 2) // up streak 2
	if changed, _ := stepMachineLevel(st, 0, 3, 2); changed {
		t.Fatal("a normal sample must not announce a change while still at normal")
	}
	// Streak reset: two more breaches should still be below the threshold.
	stepMachineLevel(st, 2, 3, 2)
	if changed, _ := stepMachineLevel(st, 2, 3, 2); changed {
		t.Fatal("up streak should have reset after the normal sample")
	}
}

func TestMachineHealthReadinessStatus(t *testing.T) {
	m := &MachineHealthMonitor{states: map[string]*machineMetricState{}}
	if got := m.ReadinessStatus(); got != "ok" {
		t.Fatalf("empty state = %q, want ok", got)
	}
	m.states["cpu"] = &machineMetricState{announced: 1}
	if got := m.ReadinessStatus(); got != "warning" {
		t.Fatalf("warning state = %q, want warning", got)
	}
	m.states["disk:/"] = &machineMetricState{announced: 2}
	if got := m.ReadinessStatus(); got != "critical" {
		t.Fatalf("critical state = %q, want critical", got)
	}
	m.pausedByUs = true
	if got := m.ReadinessStatus(); got != "critical (recording paused)" {
		t.Fatalf("paused state = %q, want paused note", got)
	}
}

func TestNormalizeMachineHealthSettingsDefaultsAndOrdering(t *testing.T) {
	got := normalizeMachineHealthSettings(MachineHealthSettings{})
	d := DefaultMachineHealthSettings()
	if got.IntervalMs != d.IntervalMs || got.SustainedSamples != d.SustainedSamples {
		t.Errorf("zero values not defaulted: %+v", got)
	}
	if got.CPU.WarnPercent != d.CPU.WarnPercent || got.CPU.CriticalPercent != d.CPU.CriticalPercent {
		t.Errorf("cpu defaults wrong: %+v", got.CPU)
	}

	// Warn >= critical is corrected so critical stays above warn.
	fixed := normalizeMachineHealthSettings(MachineHealthSettings{
		Disk: MachineDiskSettings{WarnPercent: 90, CriticalPercent: 80},
	})
	if fixed.Disk.CriticalPercent <= fixed.Disk.WarnPercent {
		t.Errorf("critical (%d) must exceed warn (%d)", fixed.Disk.CriticalPercent, fixed.Disk.WarnPercent)
	}

	// Resume must stay below pause.
	mit := normalizeMachineHealthSettings(MachineHealthSettings{
		Mitigation: MachineMitigationSettings{Enabled: true, PauseRecordingAtPercent: 90, ResumePercent: 95},
	}).Mitigation
	if mit.ResumePercent >= mit.PauseRecordingAtPercent {
		t.Errorf("resume (%d) must be below pause (%d)", mit.ResumePercent, mit.PauseRecordingAtPercent)
	}
}
