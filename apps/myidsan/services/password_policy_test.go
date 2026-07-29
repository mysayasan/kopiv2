package services

import (
	"errors"
	"strings"
	"testing"

	"github.com/mysayasan/kopiv2/infra/config"
)

func defaultPolicy() config.EffectivePasswordPolicy {
	return config.PasswordPolicyConfigModel{}.Effective()
}

// testPasswordPolicy is the shared fixture for the other service tests. They exercise
// authentication and federation, not policy, and most of their fixture passwords predate
// the policy — so they use a permissive one rather than being rewritten to satisfy rules
// they are not testing.
func testPasswordPolicy() config.EffectivePasswordPolicy {
	off := false
	return config.PasswordPolicyConfigModel{MinLength: 1, BlockCommon: &off}.Effective()
}

// An omitted passwordPolicy block must still enforce a real floor. A zero MinLength
// resolving to "no minimum" would silently disable the only control that is on by default.
func TestEffectivePolicyDefaultsToAMeaningfulFloor(t *testing.T) {
	eff := defaultPolicy()
	if eff.MinLength < 12 {
		t.Errorf("default MinLength = %d, want at least 12", eff.MinLength)
	}
	if !eff.BlockCommon {
		t.Error("the common-password denylist must be on when the block is absent")
	}
	// Composition rules default OFF on purpose: they push people toward P@ssw0rd1.
	if eff.RequireUpper || eff.RequireLower || eff.RequireDigit || eff.RequireSymbol {
		t.Error("character-class rules must default off")
	}
}

func TestValidatePasswordRejectsTooShort(t *testing.T) {
	err := ValidatePassword(defaultPolicy(), "short1", "alice@corp.local")
	if !errors.Is(err, ErrPasswordPolicy) {
		t.Fatalf("err = %v, want ErrPasswordPolicy", err)
	}
	// The message must say the target, or the person typing has nothing to act on.
	if !strings.Contains(err.Error(), "12") {
		t.Errorf("rejection should state the required length, got %q", err)
	}
}

func TestValidatePasswordAcceptsALongPassphrase(t *testing.T) {
	// No digits, no symbols, no capitals — and far stronger than P@ssw0rd1.
	if err := ValidatePassword(defaultPolicy(), "correct horse battery staple", "alice@corp.local"); err != nil {
		t.Fatalf("a long passphrase must be accepted by default: %v", err)
	}
}

// The denylist is the highest-value check: these are the guesses that actually get used.
func TestValidatePasswordRejectsCommonPasswords(t *testing.T) {
	for _, pw := range []string{
		"password123", "Password123", "PASSWORD123", // case must fold
		"qwertyuiop", "changeme123", "welcome123", "admin1234", "letmein123",
		"summer2025", "myidsan123",
	} {
		if err := ValidatePassword(defaultPolicy(), pw, ""); !errors.Is(err, ErrPasswordPolicy) {
			t.Errorf("common password %q was accepted", pw)
		}
	}
}

// A password equal to the username satisfies every character rule and is the first thing
// an attacker tries, so it is rejected regardless of policy toggles.
func TestValidatePasswordRejectsTheUsername(t *testing.T) {
	cases := []struct{ password, identifier string }{
		{"alice@corp.local", "alice@corp.local"},
		{"ALICE@CORP.LOCAL", "alice@corp.local"},
		// The local part used verbatim is the same mistake.
		{"averylongusername", "averylongusername@corp.local"},
	}
	for _, tc := range cases {
		if err := ValidatePassword(defaultPolicy(), tc.password, tc.identifier); !errors.Is(err, ErrPasswordPolicy) {
			t.Errorf("password %q equal to identifier %q was accepted", tc.password, tc.identifier)
		}
	}
}

func TestValidatePasswordEnforcesCharacterClassesWhenConfigured(t *testing.T) {
	on := true
	policy := config.PasswordPolicyConfigModel{
		MinLength:     12,
		RequireUpper:  true,
		RequireDigit:  true,
		RequireSymbol: true,
		BlockCommon:   &on,
	}.Effective()

	if err := ValidatePassword(policy, "alllowercaseletters", ""); !errors.Is(err, ErrPasswordPolicy) {
		t.Error("missing uppercase/digit/symbol should be rejected when required")
	}
	if err := ValidatePassword(policy, "HasUpperAndDigit1", ""); !errors.Is(err, ErrPasswordPolicy) {
		t.Error("missing symbol should be rejected when required")
	}
	if err := ValidatePassword(policy, "HasEverything1!", ""); err != nil {
		t.Errorf("a password meeting every rule was rejected: %v", err)
	}
}

// The rejection must name every missing class at once, not make the user discover them one
// failed submit at a time.
func TestValidatePasswordReportsAllMissingClasses(t *testing.T) {
	on := true
	policy := config.PasswordPolicyConfigModel{
		MinLength: 8, RequireUpper: true, RequireDigit: true, RequireSymbol: true, BlockCommon: &on,
	}.Effective()

	err := ValidatePassword(policy, "onlylowercase", "")
	if err == nil {
		t.Fatal("expected a rejection")
	}
	for _, want := range []string{"uppercase", "digit", "symbol"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("rejection %q does not mention the missing %s", err, want)
		}
	}
}

// A trailing space is a legitimate password character. Trimming before storing would make
// the stored password differ from what was typed, surfacing later as a login that fails
// for no visible reason.
func TestValidatePasswordDoesNotTrimForLength(t *testing.T) {
	policy := config.PasswordPolicyConfigModel{MinLength: 12}.Effective()
	// 11 visible characters plus a trailing space = 12.
	if err := ValidatePassword(policy, "elevenchars ", ""); err != nil {
		t.Errorf("a trailing space should count toward length: %v", err)
	}
	// All-whitespace is still empty in the sense that matters.
	if err := ValidatePassword(policy, "               ", ""); !errors.Is(err, ErrPasswordPolicy) {
		t.Error("an all-whitespace password must be rejected")
	}
}

// The generated bootstrap/temporary passwords must satisfy the DEFAULT policy, or a fresh
// install would issue itself a credential its own rules forbid.
func TestGeneratedPasswordsSatisfyTheDefaultPolicy(t *testing.T) {
	policy := defaultPolicy()
	for i := 0; i < 50; i++ {
		pw, err := generateBootstrapPassword()
		if err != nil {
			t.Fatalf("generateBootstrapPassword: %v", err)
		}
		if err := ValidatePassword(policy, pw, "admin"); err != nil {
			t.Fatalf("generated password %q fails the default policy: %v", pw, err)
		}
	}
}
