package config

import (
	"encoding/json"
	"testing"
)

// An entirely absent webauthn block must resolve to ON. Enabled is a *bool for exactly this
// reason, and the LoginSecurityConfigModel regression is the precedent: while that field was
// a plain bool every shipped config.json that omitted the block silently disabled the
// feature. Here "on" is cheap — WebAuthn is per-user opt-in, so it only means "a user MAY
// enrol a key" — but an operator who never edits config.json must still be able to enrol
// one, and a user who already has a key must not be locked out of it by an upgrade that
// merely failed to add a block.
func TestWebAuthnAbsentBlockDefaultsOn(t *testing.T) {
	var cfg AppConfigModel
	if err := json.Unmarshal([]byte(`{}`), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !cfg.WebAuthn.Effective().Enabled {
		t.Fatal("an absent webauthn block must enable security keys, not disable them")
	}

	// The zero value reached without JSON at all (a caller constructing the model directly)
	// must agree, or the two paths into the same struct disagree about the default.
	if !(WebAuthnConfigModel{}).Effective().Enabled {
		t.Fatal("the zero WebAuthnConfigModel must resolve to Enabled=true")
	}
}

// Switching the feature off must remain possible; it just has to be deliberate. An install
// that has no business exposing a WebAuthn ceremony (say, one fronted by a proxy that cannot
// carry the origin correctly) needs a way to say so.
func TestWebAuthnExplicitFalseIsHonoured(t *testing.T) {
	var cfg AppConfigModel
	if err := json.Unmarshal([]byte(`{"webauthn":{"enabled":false}}`), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if cfg.WebAuthn.Effective().Enabled {
		t.Fatal(`an explicit "enabled": false must disable security keys`)
	}

	// And an explicit true is not accidentally inverted by the pointer handling.
	if err := json.Unmarshal([]byte(`{"webauthn":{"enabled":true}}`), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !cfg.WebAuthn.Effective().Enabled {
		t.Fatal(`an explicit "enabled": true must stay enabled`)
	}
}

// userVerification decides whether the authenticator itself must check a PIN/biometric.
// Both wrong answers to a typo are harmful in opposite directions: "required" would refuse
// every key that cannot verify a user (locking people out of working hardware over a
// spelling mistake), and "discouraged" would silently weaken the factor to bare possession.
// "preferred" is the only resolution that neither breaks nor quietly downgrades.
func TestWebAuthnUserVerificationFallsBackToPreferred(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"unset", "", WebAuthnUvPreferred},
		{"whitespace only", "   ", WebAuthnUvPreferred},
		{"garbage", "nonsense", WebAuthnUvPreferred},
		{"near miss on required", "require", WebAuthnUvPreferred},
		{"near miss on discouraged", "discourage", WebAuthnUvPreferred},
		{"required", WebAuthnUvRequired, WebAuthnUvRequired},
		{"discouraged", WebAuthnUvDiscouraged, WebAuthnUvDiscouraged},
		// Case and stray whitespace are operator typing, not intent — an operator who asked
		// for the strict setting must get it whether or not they held shift.
		{"required upper case", "REQUIRED", WebAuthnUvRequired},
		{"required mixed case padded", "  Required  ", WebAuthnUvRequired},
		{"discouraged mixed case", "DisCouraged", WebAuthnUvDiscouraged},
		{"preferred explicit", "PREFERRED", WebAuthnUvPreferred},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := WebAuthnConfigModel{UserVerification: tc.raw}.Effective().UserVerification
			if got != tc.want {
				t.Fatalf("userVerification %q resolved to %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

// The name is what the authenticator shows in its prompt, and an empty one reads as a
// browser bug to the user. The timeout bounds how long the prompt stays open; taken
// literally a zero or negative value would mean "no window at all", so the ceremony would
// expire before the user could touch the key.
func TestWebAuthnNameAndTimeoutDefaults(t *testing.T) {
	t.Run("empty name defaults", func(t *testing.T) {
		for _, raw := range []string{"", "   "} {
			eff := WebAuthnConfigModel{RelyingPartyName: raw}.Effective()
			if eff.RelyingPartyName != defaultWebAuthnRpName {
				t.Errorf("relyingPartyName %q resolved to %q, want %q", raw, eff.RelyingPartyName, defaultWebAuthnRpName)
			}
		}
	})

	t.Run("operator name wins", func(t *testing.T) {
		eff := WebAuthnConfigModel{RelyingPartyName: "  Acme SSO  "}.Effective()
		if eff.RelyingPartyName != "Acme SSO" {
			t.Fatalf("relyingPartyName = %q, want the trimmed operator value", eff.RelyingPartyName)
		}
	})

	t.Run("non-positive timeout defaults", func(t *testing.T) {
		for _, ms := range []int{0, -1, -60000} {
			eff := WebAuthnConfigModel{TimeoutMs: ms}.Effective()
			if eff.TimeoutMs != defaultWebAuthnTimeoutMs {
				t.Errorf("timeoutMs %d resolved to %d, want %d", ms, eff.TimeoutMs, defaultWebAuthnTimeoutMs)
			}
		}
	})

	t.Run("operator timeout wins", func(t *testing.T) {
		if got := (WebAuthnConfigModel{TimeoutMs: 15000}).Effective().TimeoutMs; got != 15000 {
			t.Fatalf("timeoutMs = %d, want the operator value 15000", got)
		}
	})
}

// RelyingPartyId is the load-bearing field: a credential is bound to the RP ID it was
// created under, so a stray space would produce a DIFFERENT relying party and every enrolled
// key would stop working. It must be trimmed, and an empty value must stay empty so the
// request-derived path in infra/webauthn takes over rather than an RP ID of "".
func TestWebAuthnRelyingPartyIdTrimmedAndEmptyMeansDerive(t *testing.T) {
	if got := (WebAuthnConfigModel{RelyingPartyId: "  sso.corp.local  "}).Effective().RelyingPartyId; got != "sso.corp.local" {
		t.Fatalf("relyingPartyId = %q, want it trimmed", got)
	}
	if got := (WebAuthnConfigModel{RelyingPartyId: "   "}).Effective().RelyingPartyId; got != "" {
		t.Fatalf("a blank relyingPartyId resolved to %q, want empty so it is derived per request", got)
	}
}

// Origins is the allow-list an assertion may arrive from — the anti-phishing check itself.
// Effective() must hand back a COPY: if the resolved value aliased the config model, a later
// write to the model (the in-app settings editor rewrites these blocks) would retroactively
// change the origins an already-running Authority accepts, with no ceremony in between.
func TestWebAuthnEffectiveCopiesOrigins(t *testing.T) {
	raw := WebAuthnConfigModel{RelyingPartyOrigins: []string{"https://sso.corp.local"}}
	eff := raw.Effective()

	raw.RelyingPartyOrigins[0] = "https://evil.example"
	if eff.Origins[0] != "https://sso.corp.local" {
		t.Fatalf("mutating the config changed an already-resolved origin: %q", eff.Origins[0])
	}

	// And the reverse direction: mutating the resolved copy must not write back into config.
	eff.Origins[0] = "https://also-evil.example"
	if raw.RelyingPartyOrigins[0] != "https://evil.example" {
		t.Fatal("the resolved Origins slice aliases the config model")
	}
}

// A fully specified block must survive resolution unchanged — the defaults must only fill
// gaps, never override an operator who took the trouble to be explicit.
func TestWebAuthnFullyPopulatedBlockSurvives(t *testing.T) {
	var cfg AppConfigModel
	raw := `{"webauthn":{
		"enabled": true,
		"relyingPartyId": "corp.local",
		"relyingPartyName": "Acme SSO",
		"relyingPartyOrigins": ["https://sso.corp.local", "https://id.corp.local"],
		"userVerification": "required",
		"timeoutMs": 30000
	}}`
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	eff := cfg.WebAuthn.Effective()
	if !eff.Enabled {
		t.Error("Enabled was dropped")
	}
	if eff.RelyingPartyId != "corp.local" {
		t.Errorf("RelyingPartyId = %q", eff.RelyingPartyId)
	}
	if eff.RelyingPartyName != "Acme SSO" {
		t.Errorf("RelyingPartyName = %q", eff.RelyingPartyName)
	}
	if len(eff.Origins) != 2 || eff.Origins[0] != "https://sso.corp.local" || eff.Origins[1] != "https://id.corp.local" {
		t.Errorf("Origins = %v", eff.Origins)
	}
	if eff.UserVerification != WebAuthnUvRequired {
		t.Errorf("UserVerification = %q", eff.UserVerification)
	}
	if eff.TimeoutMs != 30000 {
		t.Errorf("TimeoutMs = %d", eff.TimeoutMs)
	}
}
