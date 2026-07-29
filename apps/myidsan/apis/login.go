package apis

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
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
	reset       services.IPasswordResetService
	audit       services.IAuditService
	sessions    services.ISessionService
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
	var challenger *mfaChallenger
	if opts.Mfa != nil && opts.Store != nil {
		challenger = newMfaChallenger(opts.Store, opts.Mfa)
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
		reset:          opts.Reset,
		audit:          opts.Audit,
		sessions:       opts.Sessions,
		trustedProxies: middlewares.ParseTrustedProxies(opts.TrustedProxies),
	}

	// Create api sub-router
	loginGroup := router.PathPrefix("/login").Subrouter()
	callbackGroup := router.PathPrefix("/callback").Subrouter()

	// Public: which social-login providers are actually configured, so the SPA shows
	// only the buttons that work (no dead Google/GitHub links).
	loginGroup.HandleFunc("/providers", handler.listProviders).Methods("GET")
	loginGroup.HandleFunc("/default", handler.defaultLogin).Methods("POST")
	loginGroup.HandleFunc("/default/register", handler.defaultRegister).Methods("POST")
	loginGroup.HandleFunc("/default/logout", handler.defaultLogout).Methods("POST")
	// Pre-session second-factor exchange: swap a challenge token + code for the
	// session cookies. PUBLIC (there is no session yet) — the token itself is the
	// short-lived, single-use, client-bound authorization to complete this login.
	loginGroup.HandleFunc("/mfa", handler.mfaLogin).Methods("POST")
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
		m.redirectKerberosFailure(w, r, continueTo)
		return
	}

	user, err := m.resolveKerberosUser(r, principal)
	if err != nil {
		log.Printf("kerberos principal %s@%s not admitted: %v", principal.Username, principal.Realm, err)
		m.recordFederatedLogin(login.KerberosProviderKey, kerberosResultLabel(err))
		m.redirectKerberosFailure(w, r, continueTo)
		return
	}

	if err := m.issueSessionCookies(w, r, user); err != nil {
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

	if locked, retry := guardLocked(m.guard, r); locked {
		writeLoginLockout(w, retry)
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

	guardSuccess(m.guard, r)
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
	m.recordAudit(r, services.AuditEntry{
		Action:     services.ActionLoginFailure,
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
	if lockedNow, retry := m.guard.RecordFailure(loginGuardKey(r)); lockedNow {
		log.Printf("login lockout engaged ip=%s retryAfter=%s", loginGuardKey(r), retry)
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

func guardLocked(guard *sharedapis.LoginGuard, r *http.Request) (bool, time.Duration) {
	if guard == nil {
		return false, 0
	}
	return guard.Locked(loginGuardKey(r))
}

func guardSuccess(guard *sharedapis.LoginGuard, r *http.Request) {
	if guard == nil {
		return
	}
	guard.RecordSuccess(loginGuardKey(r))
}

// loginGuardKey mirrors the shared login guard's keying: the connecting peer's IP
// from RemoteAddr, never a spoofable forwarded header.
func loginGuardKey(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return "ip:" + strings.TrimSpace(r.RemoteAddr)
	}
	return "ip:" + host
}

func writeLoginLockout(w http.ResponseWriter, retry time.Duration) {
	secs := int(math.Ceil(retry.Seconds()))
	if secs < 1 {
		secs = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(secs))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusTooManyRequests)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"message":           "too many failed login attempts",
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

	identity, err := provider.Callback(r)
	if err != nil {
		controllers.SendError(w, controllers.ErrStatusUnprocessableEntity, err.Error())
		return
	}
	if strings.TrimSpace(identity.Email) == "" {
		// e.g. a GitHub account with no public email — without an email there is no
		// account to show the operator and no valid Email claim to issue.
		controllers.SendError(w, controllers.ErrStatusUnprocessableEntity, provider.DisplayName()+" account email is not available")
		return
	}

	user, err := m.admitRedirectIdentity(r, identity)
	if err != nil {
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
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	m.recordLoginSuccess(r, user, loginMethodForUser(user))
	http.Redirect(w, r, consumeOAuthContinue(w, r, provider.Key()), http.StatusFound)
}

func (m *loginApi) defaultLogin(w http.ResponseWriter, r *http.Request) {
	if locked, retry := guardLocked(m.guard, r); locked {
		writeLoginLockout(w, retry)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1048576)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	body := new(login.DefaultLoginRequestModel)
	err := dec.Decode(&body)
	if err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
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

	guardSuccess(m.guard, r)
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
			controllers.SendResult(w, map[string]any{"mfaRequired": true, "mfaToken": token})
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
	if locked, retry := guardLocked(m.guard, r); locked {
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

	userId, err := m.mfa.redeem(r.Context(), r, body.MfaToken, body.Code)
	if err != nil {
		if errors.Is(err, services.ErrMfaBadCode) {
			m.recordMfaChallenge("failed")
			// The password already verified to reach this point, so the identifier is
			// withheld here: the challenge token, not a username, is what was presented.
			m.recordLoginFailure(w, r, "", services.MethodLocal, "invalid second-factor code")
			controllers.SendError(w, controllers.ErrAuthFailed, "invalid verification code")
			return
		}
		// Unknown/expired/rebound token, or an internal error — either way the
		// client must restart the login. Do not distinguish, to avoid oracles.
		m.recordMfaChallenge("expired")
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
	guardSuccess(m.guard, r)
	m.recordMfaChallenge("success")
	m.issueLocalSession(w, r, user, loginMethodForUser(user))
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
	if locked, retry := guardLocked(m.guard, r); locked {
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
			controllers.SendError(w, controllers.ErrAuthFailed, "current password is incorrect")
		case errors.Is(err, services.ErrThirdPartyOnlyAccount):
			controllers.SendError(w, controllers.ErrLimitedAccess, err.Error())
		default:
			controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		}
		return
	}
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
