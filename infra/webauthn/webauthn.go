// Package webauthn wraps github.com/go-webauthn/webauthn with the two things this suite
// needs and the library deliberately leaves to the caller: resolving the Relying Party
// identity from the incoming request when it is not configured, and keeping every other
// package free of direct protocol-type imports.
//
// Why a wrapper at all: the RP ID and the allowed origin are the security boundary of a
// WebAuthn ceremony, and getting them from config is a chore an operator should not have to
// do for the common single-host install. Deriving them here, in ONE place, means the
// derivation rule can be audited once instead of at each call site.
package webauthn

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	lib "github.com/go-webauthn/webauthn/webauthn"
	"github.com/go-webauthn/webauthn/protocol"
)

// Re-exported so callers never import the library directly. Credential and SessionData are
// the two values a caller must persist between the begin and finish legs of a ceremony.
type (
	Credential          = lib.Credential
	SessionData         = lib.SessionData
	User                = lib.User
	CredentialCreation  = protocol.CredentialCreation
	CredentialAssertion = protocol.CredentialAssertion
)

// ErrDisabled is returned when a ceremony is attempted while WebAuthn is switched off.
var ErrDisabled = errors.New("webauthn is not enabled")

// Settings is the resolved configuration for one install (infra/config's EffectiveWebAuthn
// maps onto it). RelyingPartyId and Origins may be empty, in which case they are derived
// per request — see resolve.
type Settings struct {
	Enabled          bool
	RelyingPartyId   string
	RelyingPartyName string
	Origins          []string
	// UserVerification is "preferred" | "required" | "discouraged".
	UserVerification string
	Timeout          time.Duration
}

// Authority builds a per-request ceremony handler. It holds no mutable state, so one
// instance is shared for the process lifetime.
type Authority struct {
	settings Settings
}

func New(s Settings) *Authority {
	if s.RelyingPartyName == "" {
		s.RelyingPartyName = "MyIDSan"
	}
	if s.Timeout <= 0 {
		s.Timeout = 60 * time.Second
	}
	return &Authority{settings: s}
}

func (a *Authority) Enabled() bool { return a != nil && a.settings.Enabled }

// UserVerificationRequired reports whether the configured policy demands the authenticator
// itself verify the user (PIN/biometric) rather than merely prove possession.
func (a *Authority) UserVerificationRequired() bool {
	return a != nil && strings.EqualFold(a.settings.UserVerification, "required")
}

// resolve builds the library handle for this request.
//
// When RelyingPartyId is unset it is the request's host with any port stripped, and the
// allowed origin is that host with the scheme the connection actually used. Both are taken
// from the REQUEST rather than from anything the browser asserts about itself: an attacker
// who could nominate the origin an assertion is accepted from would defeat the check
// entirely, so the client-supplied Origin header is never consulted here.
//
// A deployment behind a TLS-terminating proxy sees http on the inside and must therefore
// set webauthn.relyingPartyOrigins explicitly — otherwise the derived origin would be
// http://host while the browser reports https://host, and every assertion would be
// refused. That is a loud, immediate failure rather than a silent weakening, which is the
// right way round.
func (a *Authority) resolve(r *http.Request) (*lib.WebAuthn, error) {
	if !a.Enabled() {
		return nil, ErrDisabled
	}
	host := r.Host
	if host == "" {
		return nil, fmt.Errorf("webauthn: request has no host")
	}
	if h, _, err := net.SplitHostPort(host); err == nil && h != "" {
		host = h
	}

	rpId := a.settings.RelyingPartyId
	if rpId == "" {
		rpId = host
	}

	origins := a.settings.Origins
	if len(origins) == 0 {
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		// r.Host (with port) is the origin the browser used, so keep the port here even
		// though the RP ID above drops it — an origin is scheme://host[:port].
		origins = []string{scheme + "://" + r.Host}
	}

	uv := protocol.VerificationPreferred
	switch strings.ToLower(strings.TrimSpace(a.settings.UserVerification)) {
	case "required":
		uv = protocol.VerificationRequired
	case "discouraged":
		uv = protocol.VerificationDiscouraged
	}

	// Enforce the timeout server-side as well as in the browser: a client that ignores the
	// hint must not get an unbounded window to answer a challenge.
	timeouts := lib.TimeoutsConfig{
		Login:        lib.TimeoutConfig{Enforce: true, Timeout: a.settings.Timeout, TimeoutUVD: a.settings.Timeout},
		Registration: lib.TimeoutConfig{Enforce: true, Timeout: a.settings.Timeout, TimeoutUVD: a.settings.Timeout},
	}

	return lib.New(&lib.Config{
		RPID:          rpId,
		RPDisplayName: a.settings.RelyingPartyName,
		RPOrigins:     origins,
		// Attestation is NOT requested. "none" keeps the ceremony privacy-preserving (no
		// authenticator-model identifier is conveyed to us) and avoids implying we verify
		// attestation statements we have no metadata service to check against. The AAGUID
		// the authenticator volunteers is still recorded, for diagnostics only.
		AttestationPreference: protocol.PreferNoAttestation,
		AuthenticatorSelection: protocol.AuthenticatorSelection{
			UserVerification: uv,
			// Deliberately NOT requiring a resident/discoverable credential: requiring it
			// excludes older hardware keys with limited slots, and this is a SECOND factor
			// reached after a username is already known, so discoverability buys nothing.
			RequireResidentKey: protocol.ResidentKeyNotRequired(),
		},
		Timeouts: timeouts,
	})
}

// RelyingPartyId reports the RP ID this request would use, for display and diagnostics.
func (a *Authority) RelyingPartyId(r *http.Request) string {
	w, err := a.resolve(r)
	if err != nil {
		return ""
	}
	return w.Config.RPID
}

// BeginRegistration starts an enrolment. exclude lists the user's existing credential IDs
// so an authenticator already registered to this account refuses to enrol twice (the
// browser reports InvalidStateError), which is friendlier than accepting a duplicate row.
func (a *Authority) BeginRegistration(r *http.Request, user User, exclude [][]byte) (*CredentialCreation, *SessionData, error) {
	w, err := a.resolve(r)
	if err != nil {
		return nil, nil, err
	}
	var opts []lib.RegistrationOption
	if len(exclude) > 0 {
		descriptors := make([]protocol.CredentialDescriptor, 0, len(exclude))
		for _, id := range exclude {
			descriptors = append(descriptors, protocol.CredentialDescriptor{
				Type:         protocol.PublicKeyCredentialType,
				CredentialID: id,
			})
		}
		opts = append(opts, lib.WithExclusions(descriptors))
	}
	return w.BeginRegistration(user, opts...)
}

// FinishRegistration verifies the authenticator's attestation response, read from body.
func (a *Authority) FinishRegistration(r *http.Request, user User, session SessionData, body io.Reader) (*Credential, error) {
	w, err := a.resolve(r)
	if err != nil {
		return nil, err
	}
	parsed, err := protocol.ParseCredentialCreationResponseBody(body)
	if err != nil {
		return nil, err
	}
	return w.CreateCredential(user, session, parsed)
}

// BeginLogin starts an assertion for a known user (the second-factor case: the password
// already identified the account).
func (a *Authority) BeginLogin(r *http.Request, user User) (*CredentialAssertion, *SessionData, error) {
	w, err := a.resolve(r)
	if err != nil {
		return nil, nil, err
	}
	return w.BeginLogin(user)
}

// FinishLogin verifies an assertion read from body and returns the credential that signed
// it — the caller must persist its updated SignCount and CloneWarning.
func (a *Authority) FinishLogin(r *http.Request, user User, session SessionData, body io.Reader) (*Credential, error) {
	w, err := a.resolve(r)
	if err != nil {
		return nil, err
	}
	parsed, err := protocol.ParseCredentialRequestResponseBody(body)
	if err != nil {
		return nil, err
	}
	return w.ValidateLogin(user, session, parsed)
}

// TransportsCSV flattens a credential's transport hints for storage.
func TransportsCSV(c *Credential) string {
	if c == nil || len(c.Transport) == 0 {
		return ""
	}
	parts := make([]string, 0, len(c.Transport))
	for _, t := range c.Transport {
		if s := strings.TrimSpace(string(t)); s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, ",")
}
