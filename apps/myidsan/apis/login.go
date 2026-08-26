package apis

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/myidsan/services"
	"github.com/mysayasan/kopiv2/domain/entities"
	enumauth "github.com/mysayasan/kopiv2/domain/enums/auth"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	"github.com/mysayasan/kopiv2/domain/models"
	sharedapis "github.com/mysayasan/kopiv2/domain/shared/apis"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
	"github.com/mysayasan/kopiv2/infra/cache"
	"github.com/mysayasan/kopiv2/infra/config"
	"github.com/mysayasan/kopiv2/infra/login"
	"github.com/mysayasan/kopiv2/infra/telemetry"
)

// MetricFederatedLoginTotal counts federated login outcomes by provider and result
// — the LDAP failure modes (unreachable directory, ambiguous filter) otherwise only
// surface as individual users failing to sign in.
const MetricFederatedLoginTotal = "myidsan_federated_login_total"

// LoginApi struct
type loginApi struct {
	auth        middlewares.AuthMidware
	userService services.IUserLoginService
	providers   *login.Registry
	directory   services.IDirectoryService
	kerberos    *login.KerberosAuthenticator
	kerbLabel   string
	guard       *sharedapis.LoginGuard
	metrics     telemetry.Metrics
	mfa         *mfaChallenger
	// webauthn is held directly as well as through the challenger: the two pre-session
	// security-key legs run the ceremony themselves, which the challenger (whose job is the
	// challenge TOKEN) has no business knowing how to do.
	webauthn services.IWebAuthnService
	reset    services.IPasswordResetService
	audit    services.IAuditService
	sessions services.ISessionService
	policy   config.EffectivePasswordPolicy
	// trustedProxies decides whether a forwarded client address may be believed when an
	// event is recorded. Empty means "trust nothing but the peer", which is correct for a
	// directly-exposed instance — an audit trail whose source IP the caller can choose is
	// worse than one with no IP at all.
	trustedProxies []*net.IPNet
}

// recordAudit writes one security event, tolerating a nil service so tests and any caller
// that has not been given one keep working.
func (m *loginApi) recordAudit(r *http.Request, e services.AuditEntry) {
	if m.audit == nil {
		return
	}
	e.ClientIp, e.UserAgent = auditContext(r, m.trustedProxies)
	m.audit.Record(r.Context(), e)
}

// LoginApiOptions carries the optional login integrations; every field may be
// zero (tests pass the empty struct).
type LoginApiOptions struct {
	// Directory enables LDAP/AD form login when its config row is enabled.
	Directory services.IDirectoryService
	// Kerberos (non-nil) enables SPNEGO SSO; KerberosLabel names its button.
	Kerberos      *login.KerberosAuthenticator
	KerberosLabel string
	// Guard applies the LoginSecurity per-IP lockout to every interactive
	// credential check (local AND directory).
	Guard   *sharedapis.LoginGuard
	Metrics telemetry.Metrics
	// Mfa (non-nil) gates the two PASSWORD paths (local + directory) behind a
	// pre-session second factor; Store backs the opaque challenge tokens. Both must
	// be set together to arm MFA — either absent leaves password-only behaviour.
	Mfa   services.IMfaService
	Store cache.Store
	// Reset (non-nil) enables the public forgot-password request endpoint.
	Reset services.IPasswordResetService
	// Audit (non-nil) records authentication events to the append-only security trail.
	Audit services.IAuditService
	// Sessions (non-nil) indexes issued sessions so they can be listed and revoked.
	Sessions services.ISessionService
	// TrustedProxies gates whether X-Forwarded-For may set the recorded client address.
	TrustedProxies []string
	// PasswordPolicy is published at /api/login/password-policy for the UI hints.
	PasswordPolicy config.EffectivePasswordPolicy
	// WebAuthn (optional) lets a security key satisfy the second factor instead of a
	// TOTP code. Additive: with it absent the challenge behaves exactly as before.
	WebAuthn services.IWebAuthnService
}

// Create LoginApi. Returns the identity-provider registry so the server-rendered
// federated login page can offer the same providers as the SPA.
func NewLoginApi(
	router *mux.Router,
	oAuth2Conf *login.OAuthProvidersConfigModel,
	auth middlewares.AuthMidware,
	userService services.IUserLoginService,
	opts LoginApiOptions) *login.Registry {
	kerbLabel := strings.TrimSpace(opts.KerberosLabel)
	if kerbLabel == "" {
		kerbLabel = "Windows (SSO)"
	}
	// The challenger exists when EITHER factor kind can gate a login: TOTP needs
	// opts.Mfa, security keys need opts.WebAuthn, and both need the shared store for the
	// pre-session challenge token.
	var challenger *mfaChallenger
	if opts.Store != nil && (opts.Mfa != nil || opts.WebAuthn != nil) {
		challenger = newMfaChallenger(opts.Store, opts.Mfa).withWebAuthn(opts.WebAuthn)
	}
	handler := &loginApi{
		auth:        auth,
		userService: userService,
		providers:   login.BuildRegistry(oAuth2Conf),
		directory:   opts.Directory,
		kerberos:    opts.Kerberos,
		kerbLabel:   kerbLabel,
		guard:          opts.Guard,
		metrics:        opts.Metrics,
		mfa:            challenger,
		webauthn:       opts.WebAuthn,
		reset:          opts.Reset,
		audit:          opts.Audit,
		sessions:       opts.Sessions,
		policy:         opts.PasswordPolicy,
		trustedProxies: middlewares.ParseTrustedProxies(opts.TrustedProxies),
	}

	// Create api sub-router
	loginGroup := router.PathPrefix("/login").Subrouter()
	callbackGroup := router.PathPrefix("/callback").Subrouter()

	// Public: which social-login providers are actually configured, so the SPA shows
	// only the buttons that work (no dead Google/GitHub links).
	loginGroup.HandleFunc("/providers", handler.listProviders).Methods("GET")
	// PUBLIC: the password rules are not a secret, and the sign-up / change-password
	// forms need them BEFORE the user types. A form that states the rules up front beats
	// one that rejects after the fact — and beats a hardcoded hint that drifts from the
	// configured policy, which is exactly what "at least 8 characters" had become.
	loginGroup.HandleFunc("/password-policy", handler.passwordPolicy).Methods("GET")
	loginGroup.HandleFunc("/default", handler.defaultLogin).Methods("POST")
	loginGroup.HandleFunc("/default/register", handler.defaultRegister).Methods("POST")
	loginGroup.HandleFunc("/default/logout", handler.defaultLogout).Methods("POST")
	// Pre-session second-factor exchange: swap a challenge token + code for the
	// session cookies. PUBLIC (there is no session yet) — the token itself is the
	// short-lived, single-use, client-bound authorization to complete this login.
	loginGroup.HandleFunc("/mfa", handler.mfaLogin).Methods("POST")
	// The security-key twin of the above, in two legs because the ceremony needs a
	// server-issued challenge before the authenticator can sign anything. PUBLIC for the
	// same reason: no session exists yet, and the challenge token is the whole
	// authorization to complete this one login.
	loginGroup.HandleFunc("/mfa/webauthn/begin", handler.webauthnLoginBegin).Methods("POST")
	loginGroup.HandleFunc("/mfa/webauthn/finish", handler.webauthnLoginFinish).Methods("POST")
	// Public account-recovery request. PUBLIC and deliberately non-committal: it
	// always returns the same generic result whether or not the identifier matches,
	// so it is not an account-enumeration oracle.
	loginGroup.HandleFunc("/forgot", handler.forgotPassword).Methods("POST")
	// Authenticated: change the signed-in local account's password (also clears the
	// forced first-login must-change flag).
	loginGroup.Handle("/default/change-password", auth.Middleware(http.HandlerFunc(handler.changePassword))).Methods("POST")
	// Directory (LDAP/AD) login is a credential POST, not a browser redirect, so it
	// does not go through the redirect-provider registry.
	loginGroup.HandleFunc("/ldap", handler.ldapLogin).Methods("POST")
	// Kerberos SPNEGO is its own dance (401 + Negotiate challenge on THIS url, no
	// callback), so it is a fixed route, not a registry provider.
	loginGroup.HandleFunc("/kerberos", handler.kerberosLogin).Methods("GET")

	// One generic pair of routes serves every registered redirect provider. The fixed
	// paths above are registered first, so mux matches them before this catch-all.
	loginGroup.HandleFunc("/{provider:[a-z][a-z0-9_.:-]*}", handler.providerLogin).Methods("GET")
	callbackGroup.HandleFunc("/{provider:[a-z][a-z0-9_.:-]*}", handler.providerCallback).Methods("GET")

	return handler.providers
}

// passwordPolicy publishes the effective password rules so the UI can state them.
func (m *loginApi) passwordPolicy(w http.ResponseWriter, r *http.Request) {
	p := m.policy
	controllers.SendResult(w, map[string]any{
		"minLength":     p.MinLength,
		"requireUpper":  p.RequireUpper,
		"requireLower":  p.RequireLower,
		"requireDigit":  p.RequireDigit,
		"requireSymbol": p.RequireSymbol,
		"blockCommon":   p.BlockCommon,
	})
}

// listProviders reports the configured federated login providers. `list` is the
// authoritative registry view (key + button label + kind, in render order); the
// per-key booleans keep the pre-registry SPA contract (`{google:bool, github:bool}`)
// working until every deployed frontend reads `list`. Kind "redirect" is a browser
// round-trip (OAuth); "form" is a credential form posted back to /api/login/{key}.
func (m *loginApi) listProviders(w http.ResponseWriter, r *http.Request) {
	type providerInfo struct {
		Key         string `json:"key"`
		DisplayName string `json:"displayName"`
		Kind        string `json:"kind"`
	}
	resp := map[string]any{
		"google": false,
		"github": false,
	}
	list := []providerInfo{}
	for _, key := range m.providers.Keys() {
		p := m.providers.Get(key)
		if p == nil {
			continue
		}
		resp[key] = true
		list = append(list, providerInfo{Key: key, DisplayName: p.DisplayName(), Kind: "redirect"})
	}
	if m.directory != nil {
		if enabled, label := m.directory.LoginOption(r.Context()); enabled {
			resp["ldap"] = true
			list = append(list, providerInfo{Key: login.LdapProviderKey, DisplayName: label, Kind: "form"})
		}
	}
	if m.kerberos != nil {
		// Kind "redirect": the SPA/login page just navigates to the URL — the
		// 401/Negotiate dance happens transparently in the browser.
		resp["kerberos"] = true
		list = append(list, providerInfo{Key: login.KerberosProviderKey, DisplayName: m.kerbLabel, Kind: "redirect"})
	}
	resp["list"] = list
	controllers.SendResult(w, resp)
}

// kerberosLogin is the SPNEGO SSO endpoint: a bare GET is answered with the 401 +
// WWW-Authenticate: Negotiate challenge; the browser (on a domain-joined machine
// that trusts this site) retries with a service ticket; a verified ticket becomes
// a session. This is browser navigation, so failures land back on the login page
// with an inline error — never a dead-end 401 body.
func (m *loginApi) kerberosLogin(w http.ResponseWriter, r *http.Request) {
	if m.kerberos == nil {
		controllers.SendError(w, controllers.ErrLimitedAccess, "kerberos login is not configured")
		return
	}
	continueTo := cleanContinuePath(r.URL.Query().Get("continue"))

	principal, err := m.kerberos.Negotiate(r)
	if err != nil {
		if errors.Is(err, login.ErrKerberosNoToken) {
			login.KerberosChallenge(w)
			return
		}
		log.Printf("kerberos login refused: %v", err)
		m.recordFederatedLogin(login.KerberosProviderKey, kerberosResultLabel(err))
		// A ticket that was presented and did NOT verify is the interesting Kerberos
		// event — a forged or replayed token, a realm outside the allow-list, a clock
		// skew wide enough to matter. It is audited; the no-token case above is not,
		// because that is just the first half of every SPNEGO handshake and recording it
		// would bury the real events under one entry per browser request.
		m.recordKerberosLoginFailure(r, "", err.Error())
		m.redirectKerberosFailure(w, r, continueTo)
		return
	}

	user, err := m.resolveKerberosUser(r, principal)
	if err != nil {
		log.Printf("kerberos principal %s@%s not admitted: %v", principal.Username, principal.Realm, err)
		m.recordFederatedLogin(login.KerberosProviderKey, kerberosResultLabel(err))
		// The ticket verified but the principal was not admitted (no directory match, a
		// disabled account). Attribution is available here, so the record names it.
		m.recordKerberosLoginFailure(r, principal.Username+"@"+principal.Realm, err.Error())
		m.redirectKerberosFailure(w, r, continueTo)
		return
	}

	if err := m.issueSessionCookies(w, r, user); err != nil {
		m.recordKerberosLoginFailure(r, user.Email, "session issue failed: "+err.Error())
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	m.recordFederatedLogin(login.KerberosProviderKey, "success")
	m.recordLoginSuccess(r, user, services.MethodKerberos)
	http.Redirect(w, r, continueTo, http.StatusFound)
}

// resolveKerberosUser: the ticket proves WHO the user is; the directory (when
// enabled) supplies email/groups and resolves to the SAME (ldap, objectGUID)
// identity as password logins. Without a directory, a principal-derived identity
// stands in (documented standalone mode).
func (m *loginApi) resolveKerberosUser(r *http.Request, principal *login.KerberosPrincipal) (*entities.UserLogin, error) {
	if m.directory != nil {
		user, err := m.directory.ResolveDirectoryUser(r.Context(), principal.Username)
		if err == nil {
			return user, nil
		}
		if !errors.Is(err, services.ErrDirectoryDisabled) {
			return nil, err
		}
	}
	identity := login.StandaloneKerberosIdentity(principal)
	return m.userService.UpsertFederated(r.Context(), *identity)
}

// redirectKerberosFailure sends the browser back to the federated login page with
// an inline error, preserving the pending continue target.
func (m *loginApi) redirectKerberosFailure(w http.ResponseWriter, r *http.Request, continueTo string) {
	target := "/api/auth/login?error=sso_failed"
	if continueTo != "" && continueTo != "/" {
		target += "&continue=" + url.QueryEscape(continueTo)
	}
	http.Redirect(w, r, target, http.StatusFound)
}

func kerberosResultLabel(err error) string {
	switch {
	case errors.Is(err, login.ErrKerberosRejected):
		return "ticket_rejected"
	case errors.Is(err, login.ErrKerberosRealmNotAllowed):
		return "realm_refused"
	case errors.Is(err, login.ErrLdapUnreachable):
		return "unreachable"
	case errors.Is(err, login.ErrLdapInvalidCredential):
		// From ResolveDirectoryUser: the verified principal has no directory entry.
		return "not_in_directory"
	case errors.Is(err, services.ErrFederatedIdentityConflict):
		return "identity_conflict"
	case errors.Is(err, services.ErrInactiveAccount):
		return "inactive"
	default:
		return "error"
	}
}

// ldapLogin authenticates a username/password pair against the configured
// directory. Same lockout counters as local login: to a password sprayer both
// surfaces are the same door.
func (m *loginApi) ldapLogin(w http.ResponseWriter, r *http.Request) {
	if m.directory == nil {
		controllers.SendError(w, controllers.ErrLimitedAccess, services.ErrDirectoryDisabled.Error())
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1048576)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	body := new(login.DefaultLoginRequestModel)
	if err := dec.Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}

	// Checked AFTER decoding so the per-account key is available: a lockout keyed only on
	// the source address never sees a spray distributed across many addresses.
	if locked, retry := guardLocked(m.guard, r, body.Username); locked {
		writeLoginLockout(w, retry)
		return
	}

	user, err := m.directory.AuthenticateLdap(r.Context(), body.Username, body.Password)
	if err != nil {
		m.recordFederatedLogin(login.LdapProviderKey, ldapResultLabel(err))
		switch {
		case errors.Is(err, login.ErrLdapInvalidCredential):
			m.recordLoginFailure(w, r, body.Username, services.MethodDirectory, "invalid directory credentials")
			controllers.SendError(w, controllers.ErrAuthFailed, err.Error())
		case errors.Is(err, services.ErrDirectoryDisabled),
			errors.Is(err, services.ErrFederatedIdentityConflict),
			errors.Is(err, services.ErrInactiveAccount):
			controllers.SendError(w, controllers.ErrLimitedAccess, err.Error())
		case errors.Is(err, login.ErrLdapNoEmail), errors.Is(err, login.ErrLdapAmbiguousUser):
			controllers.SendError(w, controllers.ErrStatusUnprocessableEntity, err.Error())
		default:
			controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		}
		return
	}

	guardSuccess(m.guard, r, body.Username)
	m.recordFederatedLogin(login.LdapProviderKey, "success")
	m.completeLoginOrChallenge(w, r, user, services.MethodDirectory)
}

func (m *loginApi) recordFederatedLogin(provider, result string) {
	if m.metrics == nil {
		return
	}
	m.metrics.Inc(MetricFederatedLoginTotal, telemetry.Labels{"provider": provider, "result": result})
}

// recordLoginFailure counts a genuine credential failure toward the per-IP lockout
// and applies the configured failure delay (slows offline-style guessing).
// The attempted identifier and sign-in method are passed in so the trail can answer
// "which account was being guessed, and over which surface" — the two questions a failed
// sign-in raises. The identifier may not name a real account; that is expected and is
// recorded as-is (bounded by the audit service) rather than resolved, since resolving it
// would turn the trail into its own account-existence oracle.
func (m *loginApi) recordLoginFailure(w http.ResponseWriter, r *http.Request, attempted, method, reason string) {
	m.recordCredentialFailure(r, services.ActionLoginFailure, attempted, method, reason)
}

// recordMfaChallengeFailure audits a refused SECOND-FACTOR attempt under its own action,
// and advances the same lockout a wrong password does.
//
// Filed as a plain login failure — which is what it used to be — a code being ground is
// indistinguishable from a password being guessed, and the two are not the same incident
// at all: whoever is here has ALREADY cleared the password. That is a much later stage of
// an intrusion, and the one where the operator still has time to act.
//
// No identifier is attached: this step presents a challenge token, not a username.
func (m *loginApi) recordMfaChallengeFailure(r *http.Request, reason string) {
	m.recordCredentialFailure(r, services.ActionMfaChallenge, "", services.MethodLocal, reason)
}

// recordCredentialFailure writes one refused-credential entry, delays the response, and
// advances the shared lockout — recording the lockout itself if this attempt engaged it.
func (m *loginApi) recordCredentialFailure(r *http.Request, action, attempted, method, reason string) {
	m.recordAudit(r, services.AuditEntry{
		Action:     action,
		ActorEmail: attempted,
		TargetType: "user",
		TargetId:   attempted,
		Outcome:    services.OutcomeDenied,
		Detail:     reason,
		Metadata:   map[string]any{"method": method},
	})
	if m.guard == nil {
		return
	}
	time.Sleep(m.guard.FailedDelay())
	if lockedNow, retry := m.guard.RecordFailure(loginGuardKeys(r, attempted)...); lockedNow {
		log.Printf("login lockout engaged ip=%s account=%q retryAfter=%s", loginGuardKey(r), attempted, retry)
		m.recordAudit(r, services.AuditEntry{
			Action:     services.ActionLoginLockout,
			ActorEmail: attempted,
			TargetType: "user",
			TargetId:   attempted,
			Outcome:    services.OutcomeDenied,
			Detail:     "too many failed sign-in attempts from this address",
			Metadata:   map[string]any{"method": method, "retryAfterSeconds": int(retry.Seconds())},
		})
	}
}

// loginMethodForUser derives the sign-in method from the account's binding. It exists for
// the post-second-factor completion, where the original request that started the challenge
// is long gone: labelling a directory user's MFA completion as a "local" sign-in would
// quietly misreport how they authenticate.
// recordFederatedLoginFailure audits a redirect-provider sign-in that was refused.
//
// It deliberately does NOT go through recordLoginFailure: that helper also advances the
// failed-login lockout, and a federated callback failure is not password guessing — the
// credential was checked at the IdP. Counting it would let a misconfigured provider (a
// clock skew, an unverified email, a rotated client secret) lock legitimate users out of
// an address they never guessed a password from.
//
// attempted may be empty when the failure happened before any identity could be resolved
// (a bad state, a failed code exchange); the provider key is always recorded, so the event
// still answers "which login surface was this?".
func (m *loginApi) recordFederatedLoginFailure(r *http.Request, providerKey, attempted, reason string) {
	m.recordAudit(r, services.AuditEntry{
		Action:     services.ActionLoginFailure,
		ActorEmail: attempted,
		TargetType: "user",
		TargetId:   attempted,
		Outcome:    services.OutcomeDenied,
		Detail:     reason,
		Metadata:   map[string]any{"method": federatedMethodForKey(providerKey), "provider": providerKey},
	})
}

// recordKerberosLoginFailure audits a SPNEGO sign-in that was refused.
//
// Like the redirect-provider equivalent it does NOT advance the failed-login lockout: a
// ticket is verified cryptographically against the keytab, not guessed, so there is no
// online guessing to slow down — and a keytab that goes stale after a machine-account
// password rotation would otherwise lock out every domain user at once.
func (m *loginApi) recordKerberosLoginFailure(r *http.Request, attempted, reason string) {
	m.recordAudit(r, services.AuditEntry{
		Action:     services.ActionLoginFailure,
		ActorEmail: attempted,
		TargetType: "user",
		TargetId:   attempted,
		Outcome:    services.OutcomeDenied,
		Detail:     reason,
		Metadata:   map[string]any{"method": services.MethodKerberos, "provider": login.KerberosProviderKey},
	})
}

// federatedMethodForKey classifies a redirect provider by its config key, mirroring
// loginMethodForUser but usable when no account was resolved.
func federatedMethodForKey(providerKey string) string {
	switch strings.ToLower(strings.TrimSpace(providerKey)) {
	case "", "google", "github":
		return services.MethodSocial
	default:
		// Everything else is a configured generic OIDC entry (login.oidc[].key).
		return services.MethodOIDC
	}
}

func loginMethodForUser(user *entities.UserLogin) string {
	if user == nil {
		return services.MethodLocal
	}
	switch provider := strings.ToLower(strings.TrimSpace(user.SsoProvider)); {
	case provider == login.LdapProviderKey:
		return services.MethodDirectory
	case provider == login.KerberosProviderKey:
		return services.MethodKerberos
	case strings.HasPrefix(provider, "oidc:"):
		return services.MethodOIDC
	case provider != "":
		return services.MethodSocial
	}
	return services.MethodLocal
}

// recordLoginSuccess is called once a session has actually been issued — not when the
// password merely verified, since a password-correct login that stops at the second-factor
// challenge has not signed anyone in.
func (m *loginApi) recordLoginSuccess(r *http.Request, user *entities.UserLogin, method string) {
	if user == nil {
		return
	}
	m.recordAudit(r, services.AuditEntry{
		Action:     services.ActionLoginSuccess,
		ActorId:    user.Id,
		ActorEmail: user.Email,
		ActorRole:  user.UserRoleId,
		TargetType: "user",
		TargetId:   strconv.FormatInt(user.Id, 10),
		Detail:     "signed in",
		Metadata:   map[string]any{"method": method},
	})
}

func guardLocked(guard *sharedapis.LoginGuard, r *http.Request, identifier string) (bool, time.Duration) {
	if guard == nil {
		return false, 0
	}
	return guard.Locked(loginGuardKeys(r, identifier)...)
}

// guardSuccess clears BOTH keys: a correct password proves this source is not spraying and
// that this account's owner is present, so neither counter should keep counting.
func guardSuccess(guard *sharedapis.LoginGuard, r *http.Request, identifier string) {
	if guard == nil {
		return
	}
	guard.RecordSuccess(loginGuardKeys(r, identifier)...)
}

// loginGuardKey is the per-SOURCE key: the connecting peer's IP from RemoteAddr, never a
// spoofable forwarded header. It throttles one machine trying many accounts.
func loginGuardKey(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return "ip:" + strings.TrimSpace(r.RemoteAddr)
	}
	return "ip:" + host
}

// loginGuardAccountKey is the per-ACCOUNT key. Without it, the lockout only ever saw one
// source at a time, so a password spray distributed across many addresses — the shape
// credential-stuffing actually takes — was completely unthrottled no matter how many
// attempts it made against a single account.
//
// The tradeoff is real and deliberate: an attacker who knows a username can now lock that
// user out by spraying it. That is a nuisance the user recovers from by waiting, whereas
// unlimited guessing against a known account is a compromise they do not recover from. The
// lockout is also cleared on any successful sign-in, so a legitimate user who still knows
// their password is not held out by someone else's failures once the window passes.
//
// Returns "" for an empty identifier, and callers skip empty keys — a failure with no
// username attached (a malformed body) must not be attributed to some arbitrary account.
func loginGuardAccountKey(identifier string) string {
	id := strings.ToLower(strings.TrimSpace(identifier))
	if id == "" {
		return ""
	}
	return "user:" + id
}

// loginGuardKeys is the pair every credential surface should throttle on.
func loginGuardKeys(r *http.Request, identifier string) []string {
	keys := []string{loginGuardKey(r)}
	if accountKey := loginGuardAccountKey(identifier); accountKey != "" {
		keys = append(keys, accountKey)
	}
	return keys
}

func writeLoginLockout(w http.ResponseWriter, retry time.Duration) {
	writeLockout(w, retry, "too many failed login attempts")
}

// Throttling the endpoints that re-check a SIGNED-IN caller's own password.
//
// The lockout was wired to the front doors — the local login, the directory login, the
// server-rendered form — and to nothing else. But several authenticated endpoints also take
// the account password and report whether it was right, which makes each of them a password
// oracle for whoever is holding the cookie: change-password (a correct guess sets a new
// password and takes the account permanently), removing a security key, and clearing one's
// own second factor. Step-up was the fourth and was fixed separately.
//
// An attacker with a stolen session cannot sign in — they do not have the password — but
// could ask any of these about candidates as fast as the network allowed. They now count
// against the same lockout the front door does, keyed on the SESSION's own account rather
// than a submitted username, so unlike the login door this counter cannot be aimed at
// somebody else by a stranger.

// selfThrottleLocked answers 429 when the signed-in caller's own account is locked out,
// before any credential work happens. Returns true when it has written the response.
func selfThrottleLocked(w http.ResponseWriter, r *http.Request, guard *sharedapis.LoginGuard, email string) bool {
	locked, retry := guardLocked(guard, r, email)
	if !locked {
		return false
	}
	writeLockout(w, retry, fmt.Sprintf(
		"too many failed attempts — try again in %d seconds", int(math.Ceil(retry.Seconds()))))
	return true
}

// selfThrottleFailure delays this response and counts one wrong password against the
// shared lockout. The delay matters as much as the counter: without it the attempts before
// the threshold are free, and free guesses against a password are worth a lot when the
// candidate list is a breach dump ordered by likelihood.
func selfThrottleFailure(guard *sharedapis.LoginGuard, r *http.Request, email string) {
	if guard == nil {
		return
	}
	time.Sleep(guard.FailedDelay())
	guard.RecordFailure(loginGuardKeys(r, email)...)
}

// writeLockout answers a throttled credential check: 429, a Retry-After header, and a body
// carrying both the wait and a message naming what was being attempted.
//
// The message is a parameter because the SPA surfaces it verbatim, and the same wording
// does not fit every surface. A step-up lockout telling an operator — who is signed in, and
// looking at a re-authentication prompt — that there were "too many failed LOGIN attempts"
// describes something they did not do, and sends them to check whether their account is
// under attack at the front door instead of waiting the minute out.
func writeLockout(w http.ResponseWriter, retry time.Duration, message string) {
	secs := int(math.Ceil(retry.Seconds()))
	if secs < 1 {
		secs = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(secs))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusTooManyRequests)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"message":           message,
		"retryAfterSeconds": secs,
	})
}

func ldapResultLabel(err error) string {
	switch {
	case errors.Is(err, login.ErrLdapInvalidCredential):
		return "invalid_credential"
	case errors.Is(err, login.ErrLdapUnreachable):
		return "unreachable"
	case errors.Is(err, login.ErrLdapAmbiguousUser):
		return "ambiguous"
	case errors.Is(err, login.ErrLdapNoEmail):
		return "no_email"
	case errors.Is(err, services.ErrFederatedIdentityConflict):
		return "identity_conflict"
	case errors.Is(err, services.ErrDirectoryDisabled):
		return "disabled"
	case errors.Is(err, services.ErrInactiveAccount):
		return "inactive"
	default:
		return "error"
	}
}

func (m *loginApi) providerLogin(w http.ResponseWriter, r *http.Request) {
	key := mux.Vars(r)["provider"]
	provider := m.providers.Get(key)
	if provider == nil {
		controllers.SendError(w, controllers.ErrLimitedAccess, key+" login is not configured")
		return
	}

	// Remember where to land after the OAuth round-trip (e.g. an /api/auth/authorize
	// URL when this login was reached via a relying-app SSO redirect).
	setOAuthContinue(w, r, provider.Key(), r.URL.Query().Get("continue"))
	provider.Login(w, r)
}

func (m *loginApi) providerCallback(w http.ResponseWriter, r *http.Request) {
	key := mux.Vars(r)["provider"]
	provider := m.providers.Get(key)
	if provider == nil {
		controllers.SendError(w, controllers.ErrLimitedAccess, key+" login is not configured")
		return
	}

	// Every rejection below is audited. A federated sign-in that was refused is exactly as
	// security-relevant as a refused password — "show me the failed sign-in attempts" must
	// not silently omit SSO — and these are the events that expose a tampered callback, an
	// IdP that stopped vouching for an address, or an account someone disabled.
	identity, err := provider.Callback(r)
	if err != nil {
		m.recordFederatedLoginFailure(r, key, "", err.Error())
		controllers.SendError(w, controllers.ErrStatusUnprocessableEntity, err.Error())
		return
	}
	if strings.TrimSpace(identity.Email) == "" {
		// e.g. a GitHub account with no public email — without an email there is no
		// account to show the operator and no valid Email claim to issue.
		m.recordFederatedLoginFailure(r, key, identity.Subject, provider.DisplayName()+" account email is not available")
		controllers.SendError(w, controllers.ErrStatusUnprocessableEntity, provider.DisplayName()+" account email is not available")
		return
	}

	user, err := m.admitRedirectIdentity(r, identity)
	if err != nil {
		m.recordFederatedLoginFailure(r, key, identity.Email, err.Error())
		switch {
		case errors.Is(err, services.ErrFederatedIdentityConflict),
			errors.Is(err, services.ErrInactiveAccount):
			controllers.SendError(w, controllers.ErrLimitedAccess, err.Error())
		case errors.Is(err, services.ErrFederatedIdentityInvalid):
			controllers.SendError(w, controllers.ErrStatusUnprocessableEntity, err.Error())
		default:
			controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		}
		return
	}

	if err := m.setOAuthSession(w, r, user, identity); err != nil {
		m.recordFederatedLoginFailure(r, key, identity.Email, "session issue failed: "+err.Error())
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	m.recordLoginSuccess(r, user, loginMethodForUser(user))
	http.Redirect(w, r, consumeOAuthContinue(w, r, provider.Key()), http.StatusFound)
}

func (m *loginApi) defaultLogin(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1048576)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	body := new(login.DefaultLoginRequestModel)
	err := dec.Decode(&body)
	if err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}

	// Checked AFTER decoding so the per-account key is available.
	if locked, retry := guardLocked(m.guard, r, body.Username); locked {
		writeLoginLockout(w, retry)
		return
	}

	user, err := m.userService.AuthenticateDefault(r.Context(), body.Username, body.Password)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrInvalidCredentialPayload):
			controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		case errors.Is(err, services.ErrInvalidCredential):
			// Only genuine credential failures count toward the lockout — payload
			// or server errors are not guessing.
			m.recordLoginFailure(w, r, body.Username, services.MethodLocal, "invalid username or password")
			controllers.SendError(w, controllers.ErrAuthFailed, err.Error())
		case errors.Is(err, services.ErrInactiveAccount):
			controllers.SendError(w, controllers.ErrLimitedAccess, err.Error())
		case errors.Is(err, services.ErrThirdPartyOnlyAccount):
			controllers.SendError(w, controllers.ErrLimitedAccess, err.Error())
		default:
			controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		}
		return
	}

	guardSuccess(m.guard, r, body.Username)
	m.completeLoginOrChallenge(w, r, user, services.MethodLocal)
}

// completeLoginOrChallenge is the fork every PASSWORD login takes after the
// credential check succeeds: if the account has a confirmed second factor, mint a
// pre-session challenge token and return {mfaRequired, mfaToken} with NO cookies;
// otherwise issue the session as before. Kerberos and OAuth deliberately do NOT
// route through here (their upstream IdP owns factor policy).
func (m *loginApi) completeLoginOrChallenge(w http.ResponseWriter, r *http.Request, user *entities.UserLogin, method string) {
	if user == nil {
		controllers.SendError(w, controllers.ErrAuthFailed, "invalid username or password")
		return
	}
	if m.mfa != nil {
		required, err := m.mfa.required(r.Context(), user.Id)
		if err != nil {
			controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
			return
		}
		if required {
			token, err := m.mfa.issue(r.Context(), r, user.Id)
			if err != nil {
				controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
				return
			}
			m.recordMfaChallenge("issued")
			// mfaMethods tells the client which factors this account can actually present,
			// so it prompts for a code, asks for a key, or offers a choice — rather than
			// assuming TOTP and stranding a user whose only factor is a security key.
			// Older clients ignore the field and keep working: the TOTP leg is unchanged.
			controllers.SendResult(w, map[string]any{
				"mfaRequired": true,
				"mfaToken":    token,
				"mfaMethods":  m.mfa.methods(r.Context(), user.Id),
			})
			return
		}
	}
	m.issueLocalSession(w, r, user, method)
}

// mfaLogin completes a challenged password login: it redeems the token + code and,
// on success, issues the session that completeLoginOrChallenge withheld. Failures
// count toward the per-IP lockout exactly like a bad password would.
func (m *loginApi) mfaLogin(w http.ResponseWriter, r *http.Request) {
	if m.mfa == nil {
		controllers.SendError(w, controllers.ErrLimitedAccess, "mfa is not configured")
		return
	}
	// Source key only: the MFA redemption presents a challenge token, not a username.
	if locked, retry := guardLocked(m.guard, r, ""); locked {
		writeLoginLockout(w, retry)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 65536)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	body := struct {
		MfaToken string `json:"mfaToken"`
		Code     string `json:"code"`
	}{}
	if err := dec.Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}

	userId, usedRecovery, err := m.mfa.redeem(r.Context(), r, body.MfaToken, body.Code)
	if err != nil {
		if errors.Is(err, services.ErrMfaBadCode) {
			m.recordMfaChallenge("failed")
			m.recordMfaChallengeFailure(r, "invalid second-factor code")
			controllers.SendError(w, controllers.ErrAuthFailed, "invalid verification code")
			return
		}
		// Unknown/expired/rebound token, or an internal error — either way the
		// client must restart the login. Do not distinguish, to avoid oracles.
		m.recordMfaChallenge("expired")
		m.recordAudit(r, services.AuditEntry{
			Action:     services.ActionMfaChallenge,
			TargetType: "user",
			Outcome:    services.OutcomeDenied,
			Detail:     "second-factor challenge expired, exhausted, or presented from a different client",
			Metadata:   map[string]any{"method": services.MethodLocal},
		})
		controllers.SendError(w, controllers.ErrAuthFailed, "your verification session expired — sign in again")
		return
	}

	// Reload the resolved account to sign a fresh session, and re-check it is still
	// active in case it was disabled during the challenge window.
	user, err := m.loadActiveUser(r.Context(), userId)
	if err != nil || user == nil {
		controllers.SendError(w, controllers.ErrAuthFailed, "account is not available")
		return
	}
	// Source key only: this step redeemed a challenge token, not a username.
	guardSuccess(m.guard, r, "")
	m.recordMfaChallenge("success")
	m.recordRecoveryLogin(r, user, usedRecovery)
	m.issueLocalSession(w, r, user, loginMethodForUser(user))
}

// recordRecoveryLogin notes a sign-in that cleared the second factor with a RECOVERY code
// rather than an authenticator.
//
// The sign-in itself is already recorded, and looks completely ordinary — which is the
// problem. Somebody using break-glass has either lost their authenticator or is not the
// person who owns it, and both are worth a second look on an identity server. The codes are
// also finite: without this the count silently walks down and the first anyone hears of it
// is when the last one is spent.
func (m *loginApi) recordRecoveryLogin(r *http.Request, user *entities.UserLogin, usedRecovery bool) {
	if !usedRecovery || user == nil {
		return
	}
	m.recordAudit(r, services.AuditEntry{
		Action:     services.ActionMfaRecovery,
		ActorId:    user.Id,
		ActorEmail: user.Email,
		TargetType: "user",
		TargetId:   strconv.FormatInt(user.Id, 10),
		Detail:     "signed in with a single-use recovery code instead of the authenticator",
		Metadata:   map[string]any{"method": services.MethodRecovery, "surface": "login"},
	})
}

// webauthnLoginBegin hands back an assertion challenge for a login already holding a
// pre-session MFA token. The token is PEEKED, not spent: the security-key ceremony needs two
// round trips against the same login attempt, so it is only consumed once the assertion
// actually verifies (webauthnLoginFinish). Peeking still re-checks the client binding, so a
// token lifted from one browser cannot be driven from another.
func (m *loginApi) webauthnLoginBegin(w http.ResponseWriter, r *http.Request) {
	if m.mfa == nil || m.webauthn == nil || !m.webauthn.Enabled() {
		controllers.SendError(w, controllers.ErrLimitedAccess, "security keys are not configured")
		return
	}
	if locked, retry := guardLocked(m.guard, r, ""); locked {
		writeLoginLockout(w, retry)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 65536)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	body := struct {
		MfaToken string `json:"mfaToken"`
	}{}
	if err := dec.Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}

	userId, err := m.mfa.peek(r.Context(), r, body.MfaToken)
	if err != nil {
		m.recordMfaChallenge("expired")
		controllers.SendError(w, controllers.ErrAuthFailed, "your verification session expired — sign in again")
		return
	}
	user, err := m.loadActiveUser(r.Context(), userId)
	if err != nil || user == nil {
		controllers.SendError(w, controllers.ErrAuthFailed, "account is not available")
		return
	}

	assertion, err := m.webauthn.BeginAssert(r.Context(), r, webauthnLoginStateKey(body.MfaToken), userId, user.Email, user.Email)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, assertion)
}

// webauthnLoginFinish verifies a security-key assertion and, on success, issues the session
// completeLoginOrChallenge withheld. This is the security-key twin of mfaLogin, and it keeps
// the same invariant: no kopiv2_access cookie exists until a second factor is proven.
func (m *loginApi) webauthnLoginFinish(w http.ResponseWriter, r *http.Request) {
	if m.mfa == nil || m.webauthn == nil || !m.webauthn.Enabled() {
		controllers.SendError(w, controllers.ErrLimitedAccess, "security keys are not configured")
		return
	}
	if locked, retry := guardLocked(m.guard, r, ""); locked {
		writeLoginLockout(w, retry)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxWebAuthnBody)
	body := struct {
		MfaToken   string          `json:"mfaToken"`
		Credential json.RawMessage `json:"credential"`
	}{}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	if len(body.Credential) == 0 {
		controllers.SendError(w, controllers.ErrBadRequest, "credential is required")
		return
	}

	userId, err := m.mfa.peek(r.Context(), r, body.MfaToken)
	if err != nil {
		m.recordMfaChallenge("expired")
		controllers.SendError(w, controllers.ErrAuthFailed, "your verification session expired — sign in again")
		return
	}
	user, err := m.loadActiveUser(r.Context(), userId)
	if err != nil || user == nil {
		controllers.SendError(w, controllers.ErrAuthFailed, "account is not available")
		return
	}

	ok, note, err := m.webauthn.FinishAssert(r.Context(), r, webauthnLoginStateKey(body.MfaToken),
		userId, user.Email, user.Email, bytes.NewReader(body.Credential))
	if err != nil || !ok {
		m.recordMfaChallenge("failed")
		// The password already verified to reach here, so the identifier is withheld for
		// the same reason the TOTP path withholds it.
		m.recordLoginFailure(w, r, "", services.MethodLocal, "security-key assertion refused")
		controllers.SendError(w, controllers.ErrAuthFailed, "that security key could not be verified")
		return
	}

	// A non-advancing signature counter is the clone signal. The sign-in is allowed (see
	// services/webauthn.go for why that is not treated as proof), so this entry is the only
	// durable trace — record it against the account that just used the key.
	if note != "" {
		m.recordAudit(r, services.AuditEntry{
			ActorId:    user.Id,
			ActorEmail: user.Email,
			Action:     services.ActionWebAuthnClone,
			Outcome:    services.OutcomeSuccess,
			TargetType: "user",
			TargetId:   strconv.FormatInt(user.Id, 10),
			Detail:     note,
		})
	}

	// Spend the challenge token only now that the factor is proven.
	m.mfa.consume(r.Context(), body.MfaToken)
	guardSuccess(m.guard, r, "")
	m.recordMfaChallenge("success")
	m.issueLocalSession(w, r, user, loginMethodForUser(user))
}

// webauthnLoginStateKey namespaces the in-flight assertion to ONE login attempt. Keying on
// the challenge token (never on the user id) means two concurrent sign-in attempts for the
// same account cannot consume each other's challenge.
func webauthnLoginStateKey(mfaToken string) string {
	return "login:" + mfaToken
}

// loadActiveUser fetches an account by id and returns it only if still active.
func (m *loginApi) loadActiveUser(ctx context.Context, userId int64) (*entities.UserLogin, error) {
	rows, _, err := m.userService.Get(ctx, 1, 0, []sqldataenums.Filter{
		{FieldName: "Id", Compare: sqldataenums.Equal, Value: userId},
	}, nil)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 || rows[0] == nil || rows[0].Id == 0 || !rows[0].IsActive {
		return nil, nil
	}
	return rows[0], nil
}

func (m *loginApi) recordMfaChallenge(result string) {
	if m.metrics == nil {
		return
	}
	m.metrics.Inc(MetricMfaChallengeTotal, telemetry.Labels{"result": result})
}

// forgotPassword accepts a public account-recovery request. It NEVER reveals whether
// the identifier matched an account: the response is identical in every case (the
// service silently records a queue entry and, when mail is enabled, emails a link
// only for a real local account). `mailEnabled` reflects global config, not account
// state, so it is safe to return — it just lets the UI say "check your email" versus
// "an administrator has been notified".
func (m *loginApi) forgotPassword(w http.ResponseWriter, r *http.Request) {
	// Source key only, and checked before parsing: a recovery request names an account
	// but never proves anything about it, so throttling per-account here would let anyone
	// lock a known user out of recovery by asking for it repeatedly.
	if locked, retry := guardLocked(m.guard, r, ""); locked {
		writeLoginLockout(w, retry)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 65536)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	body := struct {
		Username string `json:"username"`
	}{}
	if err := dec.Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	mailEnabled := false
	if m.reset != nil {
		mailEnabled = m.reset.MailEnabled()
		// A storage error is swallowed too — surfacing it would itself be an oracle.
		if err := m.reset.Request(r.Context(), body.Username, loginGuardKey(r), requestOrigin(r)); err != nil {
			log.Printf("password reset request error: %v", err)
		}
	}
	controllers.SendResult(w, map[string]any{"ok": true, "mailEnabled": mailEnabled})
}

// requestOrigin reconstructs the public scheme+host the client reached us on, used
// to build absolute self-service reset links. Host comes from the request (the user
// is standing on our login page); scheme is TLS-derived, honouring a terminating
// proxy's X-Forwarded-Proto.
func requestOrigin(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if proto := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); proto != "" {
		scheme = proto
	}
	return scheme + "://" + r.Host
}

func (m *loginApi) defaultRegister(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1048576)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	body := new(login.DefaultRegisterRequestModel)
	err := dec.Decode(&body)
	if err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}

	_, err = m.userService.RegisterLocal(r.Context(), entities.UserLogin{
		Email:      body.Username,
		Userpwd:    body.Password,
		FirstName:  body.FirstName,
		LastName:   body.LastName,
		UserRoleId: 0,
		IsActive:   true,
		CreatedBy:  0,
	})
	if err != nil {
		switch {
		case errors.Is(err, services.ErrInvalidCredentialPayload):
			controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		case errors.Is(err, services.ErrAccountAlreadyExists):
			controllers.SendError(w, controllers.ErrConflict, err.Error())
		case errors.Is(err, services.ErrThirdPartyOnlyAccount):
			controllers.SendError(w, controllers.ErrLimitedAccess, err.Error())
		default:
			controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		}
		return
	}

	user, err := m.userService.AuthenticateDefault(r.Context(), body.Username, body.Password)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}

	m.issueLocalSession(w, r, user, services.MethodLocal)
}

func (m *loginApi) defaultLogout(w http.ResponseWriter, r *http.Request) {
	// Read the identity straight off the cookie rather than from request context: this
	// route is deliberately public (signing out must work even with an expired session),
	// so it never passes through the auth middleware and the context carries no claims.
	// Recorded before the cookies are cleared, while there is still an identity to
	// attribute the event to.
	if claims, err := m.auth.ClaimsFromRequest(r); err == nil && claims != nil && claims.Id != 0 {
		label := strings.TrimSpace(claims.Email)
		if label == "" {
			label = strings.TrimSpace(claims.Name)
		}
		m.recordAudit(r, services.AuditEntry{
			Action:     services.ActionLogout,
			ActorId:    claims.Id,
			ActorEmail: label,
			ActorRole:  claims.RoleId,
			TargetType: "self",
			TargetId:   strconv.FormatInt(claims.Id, 10),
			Detail:     "signed out",
		})
	}
	m.auth.ClearAuthCookies(w, r)
	controllers.SendResult(w, map[string]bool{"ok": true})
}

func (m *loginApi) changePassword(w http.ResponseWriter, r *http.Request) {
	claims, _ := r.Context().Value(enumauth.Claims).(*models.JwtCustomClaims)
	if claims == nil {
		controllers.SendError(w, controllers.ErrPermission, "not authenticated")
		return
	}
	if selfThrottleLocked(w, r, m.guard, claims.Email) {
		return
	}
	var body struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1048576)).Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	if err := m.userService.ChangePassword(r.Context(), claims.Id, body.CurrentPassword, body.NewPassword); err != nil {
		switch {
		case errors.Is(err, services.ErrInvalidCredential):
			// A correct guess here does not merely reveal the password, it REPLACES it —
			// the one authenticated password check whose success hands the account over
			// permanently. Counted against the same lockout as a failed sign-in.
			selfThrottleFailure(m.guard, r, claims.Email)
			controllers.SendError(w, controllers.ErrAuthFailed, "current password is incorrect")
		case errors.Is(err, services.ErrThirdPartyOnlyAccount):
			controllers.SendError(w, controllers.ErrLimitedAccess, err.Error())
		default:
			controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		}
		return
	}
	// The password was right, so this caller is the account's owner: clear the counters
	// their own fat-fingering may have built up.
	guardSuccess(m.guard, r, claims.Email)
	controllers.SendResult(w, map[string]bool{"ok": true})
}

// admitRedirectIdentity resolves a redirect-provider identity to an account.
// Through the directory service when available, so provider-scoped group→role
// mappings (OIDC groups claim) can seed pending accounts; plain UpsertFederated
// otherwise (tests, minimal wiring) — social identities carry no groups anyway.
func (m *loginApi) admitRedirectIdentity(r *http.Request, identity *login.Identity) (*entities.UserLogin, error) {
	if m.directory != nil {
		return m.directory.AdmitExternalIdentity(r.Context(), *identity)
	}
	return m.userService.UpsertFederated(r.Context(), *identity)
}

// setOAuthSession issues the signed-in session cookies for a federated-login user. It
// does NOT write a response body, so the caller controls the outcome (a redirect to
// the pending `continue` target, or the dashboard) instead of the browser landing on
// a raw JSON payload after the OAuth round-trip.
func (m *loginApi) setOAuthSession(w http.ResponseWriter, r *http.Request, user *entities.UserLogin, identity *login.Identity) error {
	if user == nil || identity == nil {
		return errors.New("invalid user")
	}

	name := strings.TrimSpace(identity.Name)
	if name == "" {
		name = strings.TrimSpace(strings.TrimSpace(user.FirstName) + " " + strings.TrimSpace(user.LastName))
	}
	if name == "" {
		name = user.Email
	}

	claims := &models.JwtCustomClaims{
		Id:            user.Id,
		Name:          name,
		GivenName:     identity.GivenName,
		Email:         user.Email,
		VerifiedEmail: true,
		FamilyName:    identity.FamilyName,
		Picture:       identity.Picture,
		RoleId:        user.UserRoleId,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour * 72)),
		},
	}

	return m.auth.IssueAuthCookies(w, r, *claims)
}

func oauthContinueCookieName(provider string) string {
	return "oauth_continue_" + strings.ToLower(strings.TrimSpace(provider))
}

// setOAuthContinue stores a validated post-login return path for the social-login
// round-trip, scoped to the provider's callback path so it rides back with the OAuth
// redirect. Unsafe or absent values are ignored (the callback then defaults to "/").
// The value is base64-encoded because the return path carries query characters a raw
// cookie value cannot.
func setOAuthContinue(w http.ResponseWriter, r *http.Request, provider, continueTo string) {
	continueTo = cleanContinuePath(continueTo)
	if continueTo == "/" {
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     oauthContinueCookieName(provider),
		Value:    base64.RawURLEncoding.EncodeToString([]byte(continueTo)),
		Path:     "/api/callback/" + strings.ToLower(provider),
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int((5 * time.Minute).Seconds()),
	})
}

// consumeOAuthContinue reads and clears the return path set by setOAuthContinue,
// falling back to "/" when absent or invalid. Single-use: the cookie is expired here.
func consumeOAuthContinue(w http.ResponseWriter, r *http.Request, provider string) string {
	cookie, err := r.Cookie(oauthContinueCookieName(provider))
	if err != nil || cookie.Value == "" {
		return "/"
	}
	http.SetCookie(w, &http.Cookie{
		Name:     oauthContinueCookieName(provider),
		Value:    "",
		Path:     "/api/callback/" + strings.ToLower(provider),
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
	raw, err := base64.RawURLEncoding.DecodeString(cookie.Value)
	if err != nil {
		return "/"
	}
	return cleanContinuePath(string(raw))
}

func (m *loginApi) issueLocalSession(w http.ResponseWriter, r *http.Request, user *entities.UserLogin, method string) {
	if user == nil {
		controllers.SendError(w, controllers.ErrAuthFailed, "invalid username or password")
		return
	}

	if err := m.issueSessionCookies(w, r, user); err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	m.recordLoginSuccess(r, user, method)

	controllers.SendResult(w, map[string]bool{"ok": true})
}

// issueSessionCookies signs the session for an account and sets the auth/CSRF
// cookies WITHOUT writing a body — JSON logins follow with a result payload,
// browser-navigation logins (Kerberos) with a redirect.
func (m *loginApi) issueSessionCookies(w http.ResponseWriter, r *http.Request, user *entities.UserLogin) error {
	if user == nil {
		return errors.New("invalid user")
	}

	name := strings.TrimSpace(strings.TrimSpace(user.FirstName) + " " + strings.TrimSpace(user.LastName))
	if name == "" {
		name = user.Email
	}

	expiresAt := time.Now().Add(time.Hour * 72)
	claims := &models.JwtCustomClaims{
		Id:            user.Id,
		Name:          name,
		GivenName:     user.FirstName,
		FamilyName:    user.LastName,
		Email:         user.Email,
		VerifiedEmail: true,
		Picture:       user.PicUrl,
		RoleId:        user.UserRoleId,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	}

	// Pre-generate the session id so this app can index the session it is about to issue.
	// IssueAuthCookies mints one only when the field is empty and never reports it back,
	// so without this the caller cannot know which session it just created — and an
	// unindexed session is one no administrator can list or revoke.
	if m.sessions != nil {
		sessionId, err := newFederatedOpaqueToken()
		if err != nil {
			return err
		}
		claims.SessionId = sessionId
	}

	if err := m.auth.IssueAuthCookies(w, r, *claims); err != nil {
		return err
	}
	m.recordSession(r, user, claims.SessionId, expiresAt)
	return nil
}

// recordSession indexes an issued session so it can later be listed and revoked. The
// session is already live at this point, so a failure here must not undo it — the service
// swallows and logs its own write errors for that reason.
func (m *loginApi) recordSession(r *http.Request, user *entities.UserLogin, sessionId string, expiresAt time.Time) {
	if m.sessions == nil || user == nil || strings.TrimSpace(sessionId) == "" {
		return
	}
	ip, ua := auditContext(r, m.trustedProxies)
	m.sessions.Record(r.Context(), entities.UserSession{
		SessionId:   sessionId,
		UserLoginId: user.Id,
		ExpiresAt:   expiresAt.Unix(),
		IpAddress:   ip,
		UserAgent:   ua,
		CreatedBy:   user.Id,
	})
}
