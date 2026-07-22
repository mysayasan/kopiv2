package login

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/jcmturner/gokrb5/v8/credentials"
	"github.com/jcmturner/gokrb5/v8/keytab"
	"github.com/jcmturner/gokrb5/v8/service"
	"github.com/jcmturner/gokrb5/v8/spnego"
)

// spnegoCtxCredentialsKey is the context key gokrb5's SPNEGO verifier stores the
// authenticated *credentials.Credentials under (spnego/krb5Token.go). The constant
// itself is unexported there, but the key is a plain string, so the same literal
// retrieves the value. Pinned to gokrb5 v8.4.4 — re-verify on any version bump.
const spnegoCtxCredentialsKey = "github.com/jcmturner/gokrb5/v8/ctxCredentials"

// KerberosProviderKey names the SSO option on login pages and in metrics. It is NOT
// an Identity.Provider value: when a directory is configured, Kerberos logins
// resolve through LDAP to the SAME (ldap, objectGUID) identity as password logins,
// so one person never splits into two accounts. Only the standalone fallback (no
// directory) stores identities under this key.
const KerberosProviderKey = "kerberos"

var (
	// ErrKerberosNoToken: the request carried no Negotiate token — the caller must
	// answer with the WWW-Authenticate: Negotiate challenge, not an error page.
	ErrKerberosNoToken = errors.New("no negotiate token presented")
	// ErrKerberosRejected: a token was presented but did not verify against the
	// keytab (wrong SPN, expired/forged ticket, clock skew, ...).
	ErrKerberosRejected = errors.New("kerberos ticket was rejected")
	// ErrKerberosRealmNotAllowed: the ticket verified but its realm is not on the
	// configured allow-list.
	ErrKerberosRealmNotAllowed = errors.New("kerberos realm is not allowed")
)

// KerberosPrincipal is the verified outcome of a SPNEGO exchange.
type KerberosPrincipal struct {
	Username string // sAMAccountName-style login name, no realm
	Realm    string // uppercase Kerberos realm, e.g. CORP.LOCAL
}

// KerberosSettings configures the server-side SPNEGO acceptor.
type KerberosSettings struct {
	KeytabPath string
	// ServicePrincipal must match the SPN the keytab was exported for
	// (e.g. "HTTP/myidsan.corp.local"). Empty lets gokrb5 match any principal
	// present in the keytab.
	ServicePrincipal string
	// OnlyRealms optionally allow-lists accepted realms (case-insensitive).
	OnlyRealms []string
}

// KerberosAuthenticator validates SPNEGO Negotiate tokens against a keytab. Build
// once at startup (the keytab is read from disk once); safe for concurrent use.
type KerberosAuthenticator struct {
	kt         *keytab.Keytab
	spn        string
	onlyRealms map[string]bool
}

// NewKerberosAuthenticator loads the keytab and prepares the acceptor. Errors here
// mean misconfiguration (missing/unreadable keytab) — callers should log and treat
// Kerberos as "not offered" rather than fail the whole app, mirroring how a
// half-configured OAuth provider is nilled at boot.
func NewKerberosAuthenticator(settings KerberosSettings) (*KerberosAuthenticator, error) {
	path := strings.TrimSpace(settings.KeytabPath)
	if path == "" {
		return nil, errors.New("kerberos keytab path is required")
	}
	kt, err := keytab.Load(path)
	if err != nil {
		return nil, fmt.Errorf("load keytab %s: %w", path, err)
	}
	realms := map[string]bool{}
	for _, r := range settings.OnlyRealms {
		if r = strings.ToUpper(strings.TrimSpace(r)); r != "" {
			realms[r] = true
		}
	}
	return &KerberosAuthenticator{
		kt:         kt,
		spn:        strings.TrimSpace(settings.ServicePrincipal),
		onlyRealms: realms,
	}, nil
}

// Negotiate verifies the request's `Authorization: Negotiate <token>` header.
// ErrKerberosNoToken means "challenge the browser"; other errors are real refusals.
func (m *KerberosAuthenticator) Negotiate(r *http.Request) (*KerberosPrincipal, error) {
	raw := negotiateToken(r)
	if raw == "" {
		return nil, ErrKerberosNoToken
	}
	tokenBytes, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: token is not valid base64", ErrKerberosRejected)
	}

	var token spnego.SPNEGOToken
	if err := token.Unmarshal(tokenBytes); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrKerberosRejected, err)
	}

	var opts []func(*service.Settings)
	if m.spn != "" {
		opts = append(opts, service.KeytabPrincipal(m.spn))
	}
	svc := spnego.SPNEGOService(m.kt, opts...)

	authed, ctx, status := svc.AcceptSecContext(&token)
	if !authed {
		return nil, fmt.Errorf("%w: %s", ErrKerberosRejected, status.Message)
	}
	if ctx == nil {
		return nil, fmt.Errorf("%w: no verified context", ErrKerberosRejected)
	}

	id, ok := ctx.Value(spnegoCtxCredentialsKey).(*credentials.Credentials)
	if !ok || id == nil || strings.TrimSpace(id.UserName()) == "" {
		return nil, fmt.Errorf("%w: no identity in verified context", ErrKerberosRejected)
	}

	realm := strings.ToUpper(strings.TrimSpace(id.Realm()))
	if realm == "" {
		realm = strings.ToUpper(strings.TrimSpace(id.Domain()))
	}
	if len(m.onlyRealms) > 0 && !m.onlyRealms[realm] {
		return nil, ErrKerberosRealmNotAllowed
	}

	return &KerberosPrincipal{
		Username: strings.TrimSpace(id.UserName()),
		Realm:    realm,
	}, nil
}

// Challenge writes the 401 + WWW-Authenticate: Negotiate response that makes the
// browser retry the request with a service ticket.
func KerberosChallenge(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", "Negotiate")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte("Negotiate authentication required"))
}

func negotiateToken(r *http.Request) string {
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if header == "" {
		return ""
	}
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Negotiate") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

// StandaloneKerberosIdentity builds an Identity for installs WITHOUT a directory:
// the principal is the stable subject and username@realm (lowercased) stands in as
// the email. Prefer configuring the directory — it yields real emails, groups for
// role mapping, and the same identity as password logins.
func StandaloneKerberosIdentity(p *KerberosPrincipal) *Identity {
	if p == nil {
		return nil
	}
	realmLower := strings.ToLower(p.Realm)
	return &Identity{
		Provider:      KerberosProviderKey,
		Subject:       strings.ToLower(p.Username) + "@" + realmLower,
		Email:         strings.ToLower(p.Username) + "@" + realmLower,
		EmailVerified: true,
		Name:          p.Username,
		GivenName:     p.Username,
	}
}
