package config

import (
	"encoding/json"
	"testing"
)

// The safe default here is the opposite of loginSecurity's: an absent block must NOT start
// deleting security history. Unbounded growth is a disk problem an operator can see and
// fix; silently discarded audit records are neither visible nor recoverable.
func TestAuditRetentionAbsentBlockDefaultsOff(t *testing.T) {
	var cfg AppConfigModel
	if err := json.Unmarshal([]byte(`{}`), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	eff, clamped := cfg.Audit.EffectiveAuditRetention()
	if eff.Enabled {
		t.Fatal("an absent audit.retention block must not enable deletion of the security trail")
	}
	if clamped {
		t.Fatal("nothing was configured, so nothing should report as clamped")
	}
}

// A retention window short enough to be useless — or to be someone trimming away last
// week's break-in — is raised to the floor, and the caller is told so it can warn.
func TestAuditRetentionRaisesShortWindowToFloor(t *testing.T) {
	var cfg AppConfigModel
	raw := `{"audit":{"retention":{"enabled":true,"maxRetentionDays":3}}}`
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	eff, clamped := cfg.Audit.EffectiveAuditRetention()
	if !eff.Enabled {
		t.Fatal("explicit true must enable retention")
	}
	if eff.MaxRetentionDays != MinAuditRetentionDays() {
		t.Fatalf("MaxRetentionDays got %d want the %d-day floor", eff.MaxRetentionDays, MinAuditRetentionDays())
	}
	if !clamped {
		t.Fatal("clamping must be reported so the operator can be warned rather than silently overridden")
	}
}

// A zero window taken literally would mean "everything is expired" and wipe the table. It
// must resolve to the generous default, not the destructive reading.
func TestAuditRetentionZeroDaysDoesNotMeanDeleteEverything(t *testing.T) {
	var cfg AppConfigModel
	raw := `{"audit":{"retention":{"enabled":true}}}`
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	eff, _ := cfg.Audit.EffectiveAuditRetention()
	if eff.MaxRetentionDays != defaultAuditRetentionDays {
		t.Fatalf("MaxRetentionDays got %d want the default %d", eff.MaxRetentionDays, defaultAuditRetentionDays)
	}
	if eff.FrequencyHours != defaultAuditPurgeHours {
		t.Fatalf("FrequencyHours got %d want %d", eff.FrequencyHours, defaultAuditPurgeHours)
	}
	if eff.ArchiveDir != defaultAuditArchiveDir {
		t.Fatalf("ArchiveDir got %q want %q", eff.ArchiveDir, defaultAuditArchiveDir)
	}
}

// Values at or above the floor are the operator's to choose and must survive untouched.
func TestAuditRetentionKeepsOperatorValuesAboveFloor(t *testing.T) {
	var cfg AppConfigModel
	raw := `{"audit":{"retention":{"enabled":true,"maxRetentionDays":730,"frequencyHours":6,"archiveDir":"/var/audit"}}}`
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	eff, clamped := cfg.Audit.EffectiveAuditRetention()
	if clamped {
		t.Fatal("730 days is above the floor and must not be reported as clamped")
	}
	if eff.MaxRetentionDays != 730 || eff.FrequencyHours != 6 || eff.ArchiveDir != "/var/audit" {
		t.Fatalf("operator values were altered: %+v", eff)
	}
}
