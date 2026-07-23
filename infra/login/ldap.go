package login

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/go-ldap/ldap/v3"
)

// LdapProviderKey is the Identity.Provider value for directory-backed logins. It is
// deliberately shared by every way of proving a directory identity (password bind
// today, Kerberos later), so one person always resolves to one account.
const LdapProviderKey = "ldap"

const (
	defaultLdapPort       = 636
	defaultLdapTimeout    = 10 * time.Second
	defaultLdapUserFilter = "(&(|(objectClass=user)(objectClass=inetOrgPerson))(|(sAMAccountName=%s)(uid=%s)(mail=%s)))"
	defaultLdapGroupAttr  = "memberOf"
	attrObjectGUID        = "objectGUID"
	attrEntryUUID         = "entryUUID"
	attrMail              = "mail"
	attrUserPrincipalName = "userPrincipalName"
	attrDisplayName       = "displayName"
	attrCommonName        = "cn"
	attrGivenName         = "givenName"
	attrSurname           = "sn"
)

var (
	// ErrLdapInvalidCredential: the username/password pair was rejected (no matching
	// user, or the user bind failed). Deliberately indistinguishable to the caller.
	ErrLdapInvalidCredential = errors.New("invalid directory username or password")
	// ErrLdapAmbiguousUser: the user filter matched more than one entry — a
	// misconfigured filter must fail closed, never bind "one of them".
	ErrLdapAmbiguousUser = errors.New("directory user filter matched more than one entry")
	// ErrLdapUnreachable: the directory server could not be reached or the service
	// bind failed — an operational problem, not a credential problem.
	ErrLdapUnreachable = errors.New("directory server is unreachable")
	// ErrLdapNoEmail: the matched entry carries no usable email; the suite's auth
	// middleware requires an Email claim, so the login cannot proceed.
	ErrLdapNoEmail = errors.New("directory account has no email attribute")
)

// LdapSettings is everything needed to authenticate against one directory server
// (Active Directory, Samba AD, FreeIPA, OpenLDAP, 389-ds). Transport is always TLS:
// implicit TLS (ldaps) by default, or StartTLS on a plaintext port when UseStartTLS
// is set. There is deliberately no insecure mode.
type LdapSettings struct {
	Host         string
	Port         int    // 0 -> 636 (ldaps) / 389 (StartTLS)
	UseStartTLS  bool   // plaintext connect + StartTLS upgrade instead of implicit TLS
	CaCertPem    string // optional pinned CA bundle (PEM); system roots when empty
	BindDn       string // read-only service account used to find the user entry
	BindPassword string
	BaseDn       string
	// UserFilter is an LDAP filter template; every %s is replaced with the
	// filter-escaped username. Must match EXACTLY one entry to authenticate.
	UserFilter string
	// GroupAttr is the user-entry attribute holding group memberships
	// (memberOf on AD; FreeIPA/OpenLDAP deployments may differ).
	GroupAttr string
	// SubjectAttr overrides the stable-id attribute. Empty means auto:
	// objectGUID (AD), then entryUUID (RFC 4530), then the DN as a last resort.
	SubjectAttr string
	Timeout     time.Duration
}

func (s LdapSettings) withDefaults() LdapSettings {
	if s.Port <= 0 {
		if s.UseStartTLS {
			s.Port = 389
		} else {
			s.Port = defaultLdapPort
		}
	}
	if strings.TrimSpace(s.UserFilter) == "" {
		s.UserFilter = defaultLdapUserFilter
	}
	if strings.TrimSpace(s.GroupAttr) == "" {
		s.GroupAttr = defaultLdapGroupAttr
	}
	if s.Timeout <= 0 {
		s.Timeout = defaultLdapTimeout
	}
	return s
}

func (s LdapSettings) Validate() error {
	if strings.TrimSpace(s.Host) == "" {
		return errors.New("directory host is required")
	}
	if strings.TrimSpace(s.BaseDn) == "" {
		return errors.New("directory base DN is required")
	}
	if f := strings.TrimSpace(s.UserFilter); f != "" && !strings.Contains(f, "%s") {
		return errors.New("directory user filter must contain %s for the username")
	}
	return nil
}

// LdapTestResult is what the admin "Test connection" button reports back.
type LdapTestResult struct {
	Ok      bool   `json:"ok"`
	Message string `json:"message"`
	// Populated when a sample username was supplied and matched.
	MatchedDn    string   `json:"matchedDn,omitempty"`
	Subject      string   `json:"subject,omitempty"`
	Email        string   `json:"email,omitempty"`
	GroupCount   int      `json:"groupCount,omitempty"`
	SampleGroups []string `json:"sampleGroups,omitempty"`
}

// LdapAuthenticate performs the full credential check: service bind, single-entry
// search, then a bind AS THE USER with the supplied password. The empty-password
// refusal is load-bearing: many servers treat an empty password as a successful
// anonymous/unauthenticated bind (RFC 4513 §5.1.2), which would wave anyone through.
func LdapAuthenticate(ctx context.Context, settings LdapSettings, username, password string) (*Identity, error) {
	username = strings.TrimSpace(username)
	if username == "" || strings.TrimSpace(password) == "" || strings.ContainsRune(password, 0) {
		return nil, ErrLdapInvalidCredential
	}
	if err := settings.Validate(); err != nil {
		return nil, err
	}
	s := settings.withDefaults()

	conn, err := ldapConnect(ctx, s)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrLdapUnreachable, err)
	}
	defer conn.Close()

	entry, err := ldapFindUser(conn, s, username)
	if err != nil {
		return nil, err
	}

	if err := conn.Bind(entry.DN, password); err != nil {
		if ldap.IsErrorWithCode(err, ldap.LDAPResultInvalidCredentials) {
			return nil, ErrLdapInvalidCredential
		}
		return nil, fmt.Errorf("directory user bind failed: %w", err)
	}

	return ldapIdentityFromEntry(entry, s, username)
}

// LdapLookup resolves a username to its directory identity WITHOUT any user bind:
// service bind + single-entry search only. This is the resolution path for callers
// that have already authenticated the user by other means — Kerberos SPNEGO proves
// who the user is but carries no email/groups, so the directory fills those in.
// NEVER call this on an unauthenticated username: it performs no credential check.
func LdapLookup(ctx context.Context, settings LdapSettings, username string) (*Identity, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return nil, ErrLdapInvalidCredential
	}
	if err := settings.Validate(); err != nil {
		return nil, err
	}
	s := settings.withDefaults()

	conn, err := ldapConnect(ctx, s)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrLdapUnreachable, err)
	}
	defer conn.Close()

	entry, err := ldapFindUser(conn, s, username)
	if err != nil {
		return nil, err
	}
	return ldapIdentityFromEntry(entry, s, username)
}

// LdapTest verifies the service bind (and, when sampleUsername is given, runs the
// same lookup Authenticate would) WITHOUT ever binding as the sample user — it
// never needs, takes, or checks a user password.
func LdapTest(ctx context.Context, settings LdapSettings, sampleUsername string) *LdapTestResult {
	if err := settings.Validate(); err != nil {
		return &LdapTestResult{Ok: false, Message: err.Error()}
	}
	s := settings.withDefaults()

	conn, err := ldapConnect(ctx, s)
	if err != nil {
		return &LdapTestResult{Ok: false, Message: "connect/service bind failed: " + err.Error()}
	}
	defer conn.Close()

	sampleUsername = strings.TrimSpace(sampleUsername)
	if sampleUsername == "" {
		// No sample user: prove the base DN is readable and stop there.
		req := ldap.NewSearchRequest(s.BaseDn, ldap.ScopeBaseObject, ldap.NeverDerefAliases, 1, int(s.Timeout.Seconds()), false, "(objectClass=*)", []string{"1.1"}, nil)
		if _, err := conn.Search(req); err != nil {
			return &LdapTestResult{Ok: false, Message: "base DN search failed: " + err.Error()}
		}
		return &LdapTestResult{Ok: true, Message: "service bind and base DN lookup succeeded"}
	}

	entry, err := ldapFindUser(conn, s, sampleUsername)
	if err != nil {
		return &LdapTestResult{Ok: false, Message: err.Error()}
	}
	identity, err := ldapIdentityFromEntry(entry, s, sampleUsername)
	if err != nil {
		return &LdapTestResult{Ok: false, MatchedDn: entry.DN, Message: err.Error()}
	}
	groups := identity.Groups
	sample := groups
	if len(sample) > 5 {
		sample = sample[:5]
	}
	return &LdapTestResult{
		Ok:           true,
		Message:      "user found; identity resolves",
		MatchedDn:    entry.DN,
		Subject:      identity.Subject,
		Email:        identity.Email,
		GroupCount:   len(groups),
		SampleGroups: sample,
	}
}

func ldapConnect(ctx context.Context, s LdapSettings) (*ldap.Conn, error) {
	tlsCfg := &tls.Config{ServerName: s.Host}
	if pem := strings.TrimSpace(s.CaCertPem); pem != "" {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM([]byte(pem)) {
			return nil, errors.New("directory CA certificate PEM could not be parsed")
		}
		tlsCfg.RootCAs = pool
	}

	addr := net.JoinHostPort(s.Host, strconv.Itoa(s.Port))
	dialer := &net.Dialer{Timeout: s.Timeout}

	var conn *ldap.Conn
	var err error
	if s.UseStartTLS {
		conn, err = ldap.DialURL("ldap://"+addr, ldap.DialWithDialer(dialer))
		if err == nil {
			if err = conn.StartTLS(tlsCfg); err != nil {
				conn.Close()
				return nil, fmt.Errorf("StartTLS failed: %w", err)
			}
		}
	} else {
		conn, err = ldap.DialURL("ldaps://"+addr, ldap.DialWithDialer(dialer), ldap.DialWithTLSConfig(tlsCfg))
	}
	if err != nil {
		return nil, err
	}
	conn.SetTimeout(s.Timeout)

	if strings.TrimSpace(s.BindDn) != "" {
		if err := conn.Bind(s.BindDn, s.BindPassword); err != nil {
			conn.Close()
			return nil, fmt.Errorf("service bind failed: %w", err)
		}
	}
	// No service account configured: proceed unauthenticated only if the server
	// permits anonymous search of the base DN; the search below will fail otherwise.
	_ = ctx
	return conn, nil
}

func ldapFindUser(conn *ldap.Conn, s LdapSettings, username string) (*ldap.Entry, error) {
	attrs := []string{
		attrMail, attrUserPrincipalName, attrDisplayName, attrCommonName,
		attrGivenName, attrSurname, attrObjectGUID, attrEntryUUID, s.GroupAttr,
	}
	if a := strings.TrimSpace(s.SubjectAttr); a != "" {
		attrs = append(attrs, a)
	}

	// Size limit 2: one over what is acceptable, so ambiguity is detectable without
	// pulling the whole directory.
	req := ldap.NewSearchRequest(
		s.BaseDn, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases,
		2, int(s.Timeout.Seconds()), false,
		BuildLdapUserFilter(s.UserFilter, username), attrs, nil,
	)
	res, err := conn.Search(req)
	if err != nil {
		if ldap.IsErrorWithCode(err, ldap.LDAPResultSizeLimitExceeded) {
			return nil, ErrLdapAmbiguousUser
		}
		return nil, fmt.Errorf("directory search failed: %w", err)
	}
	switch len(res.Entries) {
	case 0:
		return nil, ErrLdapInvalidCredential
	case 1:
		return res.Entries[0], nil
	default:
		return nil, ErrLdapAmbiguousUser
	}
}

// BuildLdapUserFilter substitutes the filter-escaped username for every %s in the
// template. Escaping is not optional: the username is attacker-controlled input
// inside an LDAP filter (injection).
func BuildLdapUserFilter(template, username string) string {
	if strings.TrimSpace(template) == "" {
		template = defaultLdapUserFilter
	}
	return strings.ReplaceAll(template, "%s", ldap.EscapeFilter(username))
}

func ldapIdentityFromEntry(entry *ldap.Entry, s LdapSettings, username string) (*Identity, error) {
	subject := ldapSubject(entry, s.SubjectAttr)
	if subject == "" {
		// Last resort only: a DN changes when the account moves OUs, silently
		// splitting the user into a new local account. Real deployments should have
		// objectGUID/entryUUID; if this fires, the admin should set SubjectAttr.
		subject = "dn:" + strings.ToLower(entry.DN)
	}

	email := strings.TrimSpace(entry.GetAttributeValue(attrMail))
	if email == "" {
		if upn := strings.TrimSpace(entry.GetAttributeValue(attrUserPrincipalName)); strings.Contains(upn, "@") {
			email = upn
		}
	}
	if email == "" {
		return nil, ErrLdapNoEmail
	}

	name := strings.TrimSpace(entry.GetAttributeValue(attrDisplayName))
	if name == "" {
		name = strings.TrimSpace(entry.GetAttributeValue(attrCommonName))
	}
	if name == "" {
		name = username
	}

	return &Identity{
		Provider: LdapProviderKey,
		Subject:  subject,
		Email:    email,
		// The directory is the org's own system of record.
		EmailVerified: true,
		Name:          name,
		GivenName:     strings.TrimSpace(entry.GetAttributeValue(attrGivenName)),
		FamilyName:    strings.TrimSpace(entry.GetAttributeValue(attrSurname)),
		Groups:        entry.GetAttributeValues(s.GroupAttr),
	}, nil
}

func ldapSubject(entry *ldap.Entry, override string) string {
	if a := strings.TrimSpace(override); a != "" {
		if strings.EqualFold(a, attrObjectGUID) {
			return decodeObjectGUID(entry.GetRawAttributeValue(attrObjectGUID))
		}
		return strings.TrimSpace(entry.GetAttributeValue(a))
	}
	if guid := decodeObjectGUID(entry.GetRawAttributeValue(attrObjectGUID)); guid != "" {
		return guid
	}
	return strings.TrimSpace(entry.GetAttributeValue(attrEntryUUID))
}

// decodeObjectGUID renders AD's 16-byte objectGUID in the canonical mixed-endian
// GUID text form (first three groups little-endian, per MS-DTYP), matching what AD
// tooling displays — so operators can correlate the stored subject with their DC.
func decodeObjectGUID(raw []byte) string {
	if len(raw) != 16 {
		return ""
	}
	return fmt.Sprintf("%02x%02x%02x%02x-%02x%02x-%02x%02x-%02x%02x-%02x%02x%02x%02x%02x%02x",
		raw[3], raw[2], raw[1], raw[0],
		raw[5], raw[4],
		raw[7], raw[6],
		raw[8], raw[9],
		raw[10], raw[11], raw[12], raw[13], raw[14], raw[15])
}
