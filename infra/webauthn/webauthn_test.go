package webauthn

import (
	"crypto/tls"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
)

// enabled builds an Authority with the feature on and everything else defaulted. Every test
// that exercises resolve() needs Enabled=true, because a disabled Authority short-circuits
// before any derivation happens.
func enabled(s Settings) *Authority {
	s.Enabled = true
	return New(s)
}

// req builds a plain http request against host. httptest.NewRequest sets r.Host from the
// target, which is exactly the field the derivation reads.
func req(host string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "http://"+host+"/api/webauthn/enroll", nil)
	r.Host = host
	return r
}

// The no-configuration case, and the reason the wrapper exists: a developer on
// http://localhost:3011 and an operator on https://sso.corp.local both get a working
// ceremony without touching config.json. The RP ID must be the host with the port STRIPPED —
// an RP ID is a domain, and "localhost:3011" is not one, so a browser would reject the whole
// ceremony rather than merely mismatch.
func TestRelyingPartyIdDerivedFromHostWithPortStripped(t *testing.T) {
	cases := []struct {
		name string
		host string
		want string
	}{
		{"localhost with dev port", "localhost:3011", "localhost"},
		{"localhost bare", "localhost", "localhost"},
		{"fqdn with port", "sso.corp.local:8443", "sso.corp.local"},
		{"fqdn bare", "sso.corp.local", "sso.corp.local"},
		{"ipv4 with port", "127.0.0.1:3011", "127.0.0.1"},
		// A bracketed IPv6 literal must not have its colons mistaken for a port separator.
		{"ipv6 with port", "[::1]:3011", "::1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := enabled(Settings{})
			if got := a.RelyingPartyId(req(tc.host)); got != tc.want {
				t.Fatalf("host %q derived RP ID %q, want %q", tc.host, got, tc.want)
			}
		})
	}
}

// An explicitly configured RP ID is the multi-hostname answer: one install answering on
// several names needs every name to produce the SAME relying party, or a key enrolled on one
// hostname stops working on the others. So config must win over the request, unconditionally
// — including when the request host looks nothing like it.
func TestConfiguredRelyingPartyIdWinsOverTheHost(t *testing.T) {
	a := enabled(Settings{RelyingPartyId: "corp.local"})

	for _, host := range []string{"sso.corp.local:8443", "id.corp.local", "localhost:3011"} {
		if got := a.RelyingPartyId(req(host)); got != "corp.local" {
			t.Errorf("host %q produced RP ID %q, want the configured corp.local", host, got)
		}
	}
}

// A request with no Host is a broken request, and the one thing that must NOT happen is a
// ceremony running against a silently-wrong (empty) relying party: an empty RP ID is not a
// harmless default, it is a different security scope. resolve must error instead.
func TestEmptyHostIsAnErrorNotASilentlyWrongRpId(t *testing.T) {
	a := enabled(Settings{})
	r := req("localhost:3011")
	r.Host = ""

	if _, err := a.resolve(r); err == nil {
		t.Fatal("a request with no host must be refused, not resolved to an empty RP ID")
	} else if !strings.Contains(err.Error(), "no host") {
		t.Errorf("unexpected error for a hostless request: %v", err)
	}

	// The public surface must not paper over it either: RelyingPartyId reports "" on error,
	// so a caller displaying it sees nothing rather than a plausible-looking wrong value.
	if got := a.RelyingPartyId(r); got != "" {
		t.Errorf("RelyingPartyId = %q for a hostless request, want empty", got)
	}

	// A ceremony must not start at all.
	if _, _, err := a.BeginRegistration(r, &testUser{}, nil); err == nil {
		t.Error("BeginRegistration must not start a ceremony for a hostless request")
	}
	if _, _, err := a.BeginLogin(r, &testUser{}); err == nil {
		t.Error("BeginLogin must not start a ceremony for a hostless request")
	}
}

// The origin is the anti-phishing check, so it must reflect the scheme the connection
// ACTUALLY used — never anything the client asserts. r.TLS being non-nil is the only signal
// that says "this was https"; an install behind a TLS-terminating proxy therefore derives
// http and must configure origins explicitly, which is a loud immediate failure rather than
// a silent weakening (see the resolve doc comment).
func TestOriginDerivedFromTheConnectionScheme(t *testing.T) {
	t.Run("plain http keeps the port", func(t *testing.T) {
		a := enabled(Settings{})
		w, err := a.resolve(req("localhost:3011"))
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		// An origin is scheme://host[:port] — the port stays here even though the RP ID
		// dropped it, because the browser reports the origin it actually loaded from.
		assertOrigins(t, w.Config.RPOrigins, "http://localhost:3011")
	})

	t.Run("tls yields https", func(t *testing.T) {
		a := enabled(Settings{})
		r := req("sso.corp.local")
		r.TLS = &tls.ConnectionState{}
		w, err := a.resolve(r)
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		assertOrigins(t, w.Config.RPOrigins, "https://sso.corp.local")
	})

	t.Run("a client-supplied Origin header is ignored", func(t *testing.T) {
		// The whole point: an attacker who could nominate the origin an assertion is
		// accepted from would defeat the check entirely.
		a := enabled(Settings{})
		r := req("sso.corp.local")
		r.Header.Set("Origin", "https://phishing.example")
		w, err := a.resolve(r)
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		assertOrigins(t, w.Config.RPOrigins, "http://sso.corp.local")
	})

	t.Run("configured origins win over derivation", func(t *testing.T) {
		a := enabled(Settings{
			RelyingPartyId: "corp.local",
			Origins:        []string{"https://sso.corp.local", "https://id.corp.local"},
		})
		w, err := a.resolve(req("localhost:3011"))
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		assertOrigins(t, w.Config.RPOrigins, "https://sso.corp.local", "https://id.corp.local")
	})
}

func assertOrigins(t *testing.T, got []string, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("origins = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("origins = %v, want %v", got, want)
		}
	}
}

// Enabled must be nil-safe. The Authority is wired from config at start-up and callers hold
// it as a *Authority; when the feature is off, the composition root may simply never build
// one. A nil-pointer panic on the login path would turn "security keys are off" into "nobody
// can log in", so the nil receiver has to answer false rather than crash.
func TestEnabledIsNilSafeAndOffByDefault(t *testing.T) {
	var nilAuthority *Authority
	if nilAuthority.Enabled() {
		t.Error("a nil *Authority must report disabled")
	}
	if nilAuthority.UserVerificationRequired() {
		t.Error("a nil *Authority must not claim user verification is required")
	}

	// A zero Settings is "not switched on" — New must not helpfully enable it.
	if New(Settings{}).Enabled() {
		t.Error("New(Settings{}) must be disabled; Enabled has to be asked for explicitly")
	}
	if New(Settings{Enabled: true}).Enabled() != true {
		t.Error("New(Settings{Enabled: true}) must be enabled")
	}
}

// A disabled Authority must refuse every ceremony with ErrDisabled, not merely return an
// empty result a caller might mistake for success.
func TestDisabledAuthorityRefusesEveryCeremony(t *testing.T) {
	a := New(Settings{}) // not enabled
	r := req("localhost:3011")

	if _, err := a.resolve(r); !errors.Is(err, ErrDisabled) {
		t.Errorf("resolve error = %v, want ErrDisabled", err)
	}
	if _, _, err := a.BeginRegistration(r, &testUser{}, nil); !errors.Is(err, ErrDisabled) {
		t.Errorf("BeginRegistration error = %v, want ErrDisabled", err)
	}
	if _, _, err := a.BeginLogin(r, &testUser{}); !errors.Is(err, ErrDisabled) {
		t.Errorf("BeginLogin error = %v, want ErrDisabled", err)
	}
	if _, err := a.FinishRegistration(r, &testUser{}, SessionData{}, strings.NewReader("{}")); !errors.Is(err, ErrDisabled) {
		t.Errorf("FinishRegistration error = %v, want ErrDisabled", err)
	}
	if _, err := a.FinishLogin(r, &testUser{}, SessionData{}, strings.NewReader("{}")); !errors.Is(err, ErrDisabled) {
		t.Errorf("FinishLogin error = %v, want ErrDisabled", err)
	}
}

// UserVerificationRequired is what the rest of the app asks to know whether a key alone
// counts as two factors (PIN/biometric on the key) or only as possession. Only the exact
// "required" policy may answer true — "preferred" must not, or the app would claim a
// guarantee the ceremony did not demand.
func TestUserVerificationRequiredOnlyForRequired(t *testing.T) {
	cases := map[string]bool{
		"required":   true,
		"REQUIRED":   true, // case is operator typing, not intent
		"Required":   true,
		"preferred":  false,
		"":           false,
		"discourage": false,
		"nonsense":   false,
	}
	for uv, want := range cases {
		if got := enabled(Settings{UserVerification: uv}).UserVerificationRequired(); got != want {
			t.Errorf("UserVerificationRequired(%q) = %v, want %v", uv, got, want)
		}
	}
}

// The policy must reach the ceremony options the browser is handed, not just the predicate
// above — and an unrecognised value must land on "preferred" here too, mirroring the config
// layer's fallback so a typo cannot lock out keys that lack a PIN.
func TestUserVerificationReachesTheCeremonyOptions(t *testing.T) {
	cases := []struct {
		uv   string
		want protocol.UserVerificationRequirement
	}{
		{"required", protocol.VerificationRequired},
		{"  REQUIRED ", protocol.VerificationRequired},
		{"discouraged", protocol.VerificationDiscouraged},
		{"preferred", protocol.VerificationPreferred},
		{"", protocol.VerificationPreferred},
		{"nonsense", protocol.VerificationPreferred},
	}
	for _, tc := range cases {
		t.Run(tc.uv, func(t *testing.T) {
			w, err := enabled(Settings{UserVerification: tc.uv}).resolve(req("localhost:3011"))
			if err != nil {
				t.Fatalf("resolve: %v", err)
			}
			if got := w.Config.AuthenticatorSelection.UserVerification; got != tc.want {
				t.Fatalf("userVerification %q produced %q, want %q", tc.uv, got, tc.want)
			}
		})
	}
}

// The timeout is enforced server-side as well as hinted to the browser: a client that ignores
// the hint must not get an unbounded window to answer a challenge. New also has to substitute
// a real default for a non-positive value, since zero would otherwise mean "expired already".
func TestTimeoutDefaultedAndEnforcedServerSide(t *testing.T) {
	for _, in := range []time.Duration{0, -time.Second} {
		w, err := enabled(Settings{Timeout: in}).resolve(req("localhost:3011"))
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		if w.Config.Timeouts.Registration.Timeout != 60*time.Second {
			t.Errorf("timeout %v resolved to %v, want the 60s default", in, w.Config.Timeouts.Registration.Timeout)
		}
	}

	w, err := enabled(Settings{Timeout: 25 * time.Second}).resolve(req("localhost:3011"))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !w.Config.Timeouts.Registration.Enforce || !w.Config.Timeouts.Login.Enforce {
		t.Error("timeouts must be ENFORCED, not merely advertised to the browser")
	}
	if w.Config.Timeouts.Login.Timeout != 25*time.Second {
		t.Errorf("login timeout = %v, want the configured 25s", w.Config.Timeouts.Login.Timeout)
	}
}

// The display name is what the authenticator puts in front of the user when it prompts. An
// empty one reads as a broken site, so New fills it in.
func TestRelyingPartyNameDefaulted(t *testing.T) {
	if got := New(Settings{}).settings.RelyingPartyName; got != "MyIDSan" {
		t.Errorf("RelyingPartyName = %q, want the MyIDSan default", got)
	}
	if got := New(Settings{RelyingPartyName: "Acme SSO"}).settings.RelyingPartyName; got != "Acme SSO" {
		t.Errorf("RelyingPartyName = %q, want the configured value", got)
	}
}

// Attestation is deliberately NOT requested and a resident key deliberately NOT required —
// both are documented decisions (privacy, and not excluding older hardware from a factor
// that is reached after the username is already known). Pinning them here so a future
// library upgrade that changes a default cannot silently alter what we ask of authenticators.
func TestCeremonyAsksForNoAttestationAndNoResidentKey(t *testing.T) {
	w, err := enabled(Settings{}).resolve(req("localhost:3011"))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if w.Config.AttestationPreference != protocol.PreferNoAttestation {
		t.Errorf("attestation preference = %q, want none", w.Config.AttestationPreference)
	}
	rk := w.Config.AuthenticatorSelection.RequireResidentKey
	if rk == nil || *rk {
		t.Error("a resident/discoverable credential must not be required")
	}
}

// BeginRegistration sends the account's existing credential IDs as exclusions so the browser
// refuses to enrol the same physical key twice (InvalidStateError), which is friendlier than
// accepting a duplicate row that the user then cannot tell apart from the original.
func TestBeginRegistrationCarriesExclusions(t *testing.T) {
	a := enabled(Settings{})
	u := &testUser{id: []byte("user-1"), name: "alice@corp.local"}

	creation, session, err := a.BeginRegistration(req("localhost:3011"), u, [][]byte{[]byte("cred-a"), []byte("cred-b")})
	if err != nil {
		t.Fatalf("BeginRegistration: %v", err)
	}
	if len(creation.Response.CredentialExcludeList) != 2 {
		t.Fatalf("exclude list has %d entries, want 2", len(creation.Response.CredentialExcludeList))
	}
	for _, d := range creation.Response.CredentialExcludeList {
		if d.Type != protocol.PublicKeyCredentialType {
			t.Errorf("exclusion type = %q, want public-key", d.Type)
		}
	}
	// The challenge must be handed back for the caller to persist, or the finish leg has
	// nothing to match the response against.
	if session == nil || len(session.Challenge) == 0 {
		t.Fatal("BeginRegistration returned no session challenge to persist")
	}
	// And the RP ID baked into the options must be the derived one.
	if creation.Response.RelyingParty.ID != "localhost" {
		t.Errorf("creation RP ID = %q, want localhost", creation.Response.RelyingParty.ID)
	}
}

// With no exclusions there must be no exclude list at all — an empty-but-present list is a
// different message to the browser than no list.
func TestBeginRegistrationWithoutExclusions(t *testing.T) {
	creation, _, err := enabled(Settings{}).BeginRegistration(req("localhost:3011"), &testUser{id: []byte("u")}, nil)
	if err != nil {
		t.Fatalf("BeginRegistration: %v", err)
	}
	if len(creation.Response.CredentialExcludeList) != 0 {
		t.Fatalf("exclude list = %v, want none", creation.Response.CredentialExcludeList)
	}
}

// BeginLogin is the second-factor path: the password already named the account, so the
// assertion is scoped to that user's credentials. A user with no credentials cannot be
// asserted against, and the library must say so rather than issue an empty challenge that
// any key could answer.
func TestBeginLoginRequiresAtLeastOneCredential(t *testing.T) {
	a := enabled(Settings{})

	if _, _, err := a.BeginLogin(req("localhost:3011"), &testUser{id: []byte("u")}); err == nil {
		t.Error("an assertion for a user with no credentials must be refused")
	}

	u := &testUser{id: []byte("u"), creds: []Credential{{ID: []byte("cred-a")}}}
	assertion, session, err := a.BeginLogin(req("localhost:3011"), u)
	if err != nil {
		t.Fatalf("BeginLogin: %v", err)
	}
	if assertion.Response.RelyingPartyID != "localhost" {
		t.Errorf("assertion RP ID = %q, want localhost", assertion.Response.RelyingPartyID)
	}
	if len(assertion.Response.AllowedCredentials) != 1 {
		t.Errorf("allowed credentials = %d, want the user's single key", len(assertion.Response.AllowedCredentials))
	}
	if session == nil || len(session.Challenge) == 0 {
		t.Fatal("BeginLogin returned no session challenge to persist")
	}
}

// Two ceremonies must never share a challenge. If they did, a response captured from one
// could be replayed into the other.
func TestEachCeremonyGetsAFreshChallenge(t *testing.T) {
	a := enabled(Settings{})
	u := &testUser{id: []byte("u")}

	_, first, err := a.BeginRegistration(req("localhost:3011"), u, nil)
	if err != nil {
		t.Fatalf("BeginRegistration: %v", err)
	}
	_, second, err := a.BeginRegistration(req("localhost:3011"), u, nil)
	if err != nil {
		t.Fatalf("BeginRegistration: %v", err)
	}
	if string(first.Challenge) == string(second.Challenge) {
		t.Fatal("two ceremonies were issued the same challenge; a captured response could be replayed")
	}
}

// A garbage response body must be rejected at parse time rather than reaching the verifier.
// The crypto itself needs a real authenticator to exercise, but the refusal path does not.
func TestFinishRejectsMalformedBodies(t *testing.T) {
	a := enabled(Settings{})
	r := req("localhost:3011")

	for _, body := range []string{"", "not json", `{"id":"x"}`} {
		if _, err := a.FinishRegistration(r, &testUser{id: []byte("u")}, SessionData{}, strings.NewReader(body)); err == nil {
			t.Errorf("FinishRegistration accepted a malformed body %q", body)
		}
		if _, err := a.FinishLogin(r, &testUser{id: []byte("u")}, SessionData{}, strings.NewReader(body)); err == nil {
			t.Errorf("FinishLogin accepted a malformed body %q", body)
		}
	}
}

// TransportsCSV feeds a database column, so it must be total: no panic on a nil credential
// (the caller has an error path where cred is nil) and no stray separators that would later
// read back as an empty transport hint.
func TestTransportsCSV(t *testing.T) {
	cases := []struct {
		name string
		cred *Credential
		want string
	}{
		{"nil credential", nil, ""},
		{"no transports", &Credential{}, ""},
		{"single", &Credential{Transport: []protocol.AuthenticatorTransport{"usb"}}, "usb"},
		{"multiple", &Credential{Transport: []protocol.AuthenticatorTransport{"usb", "nfc", "internal"}}, "usb,nfc,internal"},
		// Blank entries are dropped rather than producing "usb,," which would round-trip
		// back as a transport named "".
		{"blank entries dropped", &Credential{Transport: []protocol.AuthenticatorTransport{"usb", "", "  ", "nfc"}}, "usb,nfc"},
		{"only blanks", &Credential{Transport: []protocol.AuthenticatorTransport{"", " "}}, ""},
		{"whitespace trimmed", &Credential{Transport: []protocol.AuthenticatorTransport{" usb ", "nfc "}}, "usb,nfc"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := TransportsCSV(tc.cred); got != tc.want {
				t.Fatalf("TransportsCSV = %q, want %q", got, tc.want)
			}
		})
	}
}

// testUser is a minimal lib.User. WebAuthnID is the user handle baked into every credential,
// so it only has to be stable and non-empty here.
type testUser struct {
	id      []byte
	name    string
	display string
	creds   []Credential
}

func (u *testUser) WebAuthnID() []byte {
	if len(u.id) == 0 {
		return []byte("test-user")
	}
	return u.id
}
func (u *testUser) WebAuthnName() string { return u.name }
func (u *testUser) WebAuthnDisplayName() string {
	if u.display != "" {
		return u.display
	}
	return u.name
}
func (u *testUser) WebAuthnCredentials() []Credential { return u.creds }
