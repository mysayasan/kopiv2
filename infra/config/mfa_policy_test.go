package config

import (
	"encoding/json"
	"testing"
)

// An absent or unreadable policy must resolve to "optional". Defaulting to "required"
// would lock every existing admin out of a server the moment it upgraded; defaulting to
// "off" would silently discard a control the operator asked for.
func TestMfaPolicyDefaultsToOptional(t *testing.T) {
	var cfg AppConfigModel
	if err := json.Unmarshal([]byte(`{}`), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got := cfg.Mfa.Effective().Policy; got != MfaPolicyOptional {
		t.Fatalf("policy = %q, want %q", got, MfaPolicyOptional)
	}

	for _, raw := range []string{`{"mfa":{"policy":""}}`, `{"mfa":{"policy":"nonsense"}}`, `{"mfa":{"policy":"REQUIRE"}}`} {
		var c AppConfigModel
		if err := json.Unmarshal([]byte(raw), &c); err != nil {
			t.Fatalf("unmarshal %s: %v", raw, err)
		}
		if got := c.Mfa.Effective().Policy; got != MfaPolicyOptional {
			t.Errorf("%s resolved to %q, want optional", raw, got)
		}
	}
}

func TestMfaPolicyCaseInsensitive(t *testing.T) {
	var cfg AppConfigModel
	if err := json.Unmarshal([]byte(`{"mfa":{"policy":"REQUIRED"}}`), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got := cfg.Mfa.Effective().Policy; got != MfaPolicyRequired {
		t.Fatalf("policy = %q, want required", got)
	}
}

// Nobody is required under off/optional, whatever the role.
func TestRequiresFactorOnlyUnderRequired(t *testing.T) {
	for _, policy := range []string{MfaPolicyOff, MfaPolicyOptional} {
		eff := MfaPolicyConfigModel{Policy: policy}.Effective()
		if eff.RequiresFactor(1, false) {
			t.Errorf("policy %q must not require a factor", policy)
		}
	}
}

// "required" with no role list means everyone in scope.
func TestRequiresFactorAppliesToEveryoneWhenNoRolesListed(t *testing.T) {
	eff := MfaPolicyConfigModel{Policy: MfaPolicyRequired}.Effective()
	for _, roleId := range []int64{0, 1, 2, 99} {
		if !eff.RequiresFactor(roleId, false) {
			t.Errorf("role %d should be required when no role list is set", roleId)
		}
	}
}

// The common configuration: mandatory for administrators, optional for everyone else.
func TestRequiresFactorNarrowsToListedRoles(t *testing.T) {
	eff := MfaPolicyConfigModel{
		Policy:          MfaPolicyRequired,
		RequiredRoleIds: []int64{1, 4},
	}.Effective()

	for _, roleId := range []int64{1, 4} {
		if !eff.RequiresFactor(roleId, false) {
			t.Errorf("listed role %d should require a factor", roleId)
		}
	}
	for _, roleId := range []int64{0, 2, 3, 5} {
		if eff.RequiresFactor(roleId, false) {
			t.Errorf("unlisted role %d must not require a factor", roleId)
		}
	}
}

// Directory accounts are out of scope unless the operator opts in: their factor policy
// usually belongs to the domain, and forcing one here can duplicate it.
func TestRequiresFactorExcludesDirectoryAccountsByDefault(t *testing.T) {
	eff := MfaPolicyConfigModel{Policy: MfaPolicyRequired}.Effective()
	if eff.RequiresFactor(1, true) {
		t.Error("a directory account must be out of scope unless applyToDirectory is set")
	}
	if !eff.RequiresFactor(1, false) {
		t.Error("a local account must still be in scope")
	}

	optIn := MfaPolicyConfigModel{Policy: MfaPolicyRequired, ApplyToDirectory: true}.Effective()
	if !optIn.RequiresFactor(1, true) {
		t.Error("applyToDirectory must bring directory accounts into scope")
	}
}

// Effective() must copy the slice, so a later mutation of the config cannot silently
// change who is required.
func TestEffectiveCopiesTheRoleList(t *testing.T) {
	raw := MfaPolicyConfigModel{Policy: MfaPolicyRequired, RequiredRoleIds: []int64{1}}
	eff := raw.Effective()
	raw.RequiredRoleIds[0] = 999
	if !eff.RequiresFactor(1, false) {
		t.Fatal("mutating the source config changed an already-resolved policy")
	}
}
