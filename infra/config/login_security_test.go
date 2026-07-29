package config

import (
	"encoding/json"
	"testing"
)

// An entirely absent loginSecurity block must resolve to ON. This is the regression that
// mattered: while Enabled was a plain bool, deploy/dist/myseliasan-config.json and the
// myidsan/myseliasan dev configs all omitted the block, so the failed-login guard silently
// became a no-op on the very services that most needed it.
func TestLoginSecurityAbsentBlockDefaultsOn(t *testing.T) {
	var cfg AppConfigModel
	if err := json.Unmarshal([]byte(`{}`), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	eff := cfg.LoginSecurity.Effective()
	if !eff.Enabled {
		t.Fatal("an absent loginSecurity block must enable the lockout, not disable it")
	}
	if eff.MaxAttempts != defaultLoginMaxAttempts {
		t.Fatalf("MaxAttempts got %d want %d", eff.MaxAttempts, defaultLoginMaxAttempts)
	}
	if eff.WindowSeconds != defaultLoginWindowSeconds {
		t.Fatalf("WindowSeconds got %d want %d", eff.WindowSeconds, defaultLoginWindowSeconds)
	}
	if eff.LockoutSeconds != defaultLoginLockoutSeconds {
		t.Fatalf("LockoutSeconds got %d want %d", eff.LockoutSeconds, defaultLoginLockoutSeconds)
	}
	if eff.LockoutMaxSeconds != defaultLoginLockoutMaxSeconds {
		t.Fatalf("LockoutMaxSeconds got %d want %d", eff.LockoutMaxSeconds, defaultLoginLockoutMaxSeconds)
	}
	if eff.FailedDelayMs != defaultLoginFailedDelayMs {
		t.Fatalf("FailedDelayMs got %d want %d", eff.FailedDelayMs, defaultLoginFailedDelayMs)
	}
}

// Turning the lockout off must still be possible — it just has to be deliberate.
func TestLoginSecurityExplicitFalseIsHonoured(t *testing.T) {
	var cfg AppConfigModel
	if err := json.Unmarshal([]byte(`{"loginSecurity":{"enabled":false}}`), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if cfg.LoginSecurity.Effective().Enabled {
		t.Fatal(`an explicit "enabled": false must disable the lockout`)
	}
}

// Operator-supplied tunables win over the defaults; unset ones are still filled in, so a
// partially specified block cannot produce a lockout that trips on the first attempt.
func TestLoginSecurityPartialBlockKeepsOperatorValues(t *testing.T) {
	var cfg AppConfigModel
	raw := `{"loginSecurity":{"enabled":true,"maxAttempts":3,"notifyOnLockout":true}}`
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	eff := cfg.LoginSecurity.Effective()
	if !eff.Enabled {
		t.Fatal("explicit true must stay enabled")
	}
	if eff.MaxAttempts != 3 {
		t.Fatalf("MaxAttempts got %d want the operator value 3", eff.MaxAttempts)
	}
	if !eff.NotifyOnLockout {
		t.Fatal("NotifyOnLockout must survive Effective()")
	}
	if eff.WindowSeconds != defaultLoginWindowSeconds {
		t.Fatalf("an unset WindowSeconds must fall back to %d, got %d", defaultLoginWindowSeconds, eff.WindowSeconds)
	}
}

// A zero MaxAttempts is the dangerous case: taken literally it locks a user out before
// their first attempt is even evaluated. It must resolve to the default instead.
func TestLoginSecurityZeroTunablesDoNotLockOutImmediately(t *testing.T) {
	enabled := true
	l := LoginSecurityConfigModel{Enabled: &enabled}

	eff := l.Effective()
	if eff.MaxAttempts <= 1 {
		t.Fatalf("MaxAttempts resolved to %d — a zero tunable must not lock out on the first failure", eff.MaxAttempts)
	}
}
