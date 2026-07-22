package services

import (
	"context"
	"encoding/base64"
	"errors"
	"log"
	"sort"
	"strings"
	"time"

	myidsanentities "github.com/mysayasan/kopiv2/apps/myidsan/entities"
	"github.com/mysayasan/kopiv2/domain/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	"github.com/mysayasan/kopiv2/infra/atrest"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	"github.com/mysayasan/kopiv2/infra/login"
)

const directoryConfigName = "default"

// ErrDirectoryDisabled: LDAP login was attempted while no enabled directory is
// configured.
var ErrDirectoryDisabled = errors.New("directory login is not enabled")

// DirectoryConfigView is the admin read model — everything except the bind
// password, which is write-only (HasBindPassword says whether one is stored).
type DirectoryConfigView struct {
	Enabled         bool   `json:"enabled"`
	DisplayLabel    string `json:"displayLabel"`
	Host            string `json:"host"`
	Port            int64  `json:"port"`
	UseStartTls     bool   `json:"useStartTls"`
	CaCertPem       string `json:"caCertPem"`
	BindDn          string `json:"bindDn"`
	HasBindPassword bool   `json:"hasBindPassword"`
	BaseDn          string `json:"baseDn"`
	UserFilter      string `json:"userFilter"`
	GroupAttr       string `json:"groupAttr"`
	SubjectAttr     string `json:"subjectAttr"`
	Authoritative   bool   `json:"authoritative"`
}

// DirectoryConfigPayload is the admin write model. A blank BindPassword keeps the
// stored one, so saving unrelated edits never requires re-entering the secret.
type DirectoryConfigPayload struct {
	Enabled       bool   `json:"enabled"`
	DisplayLabel  string `json:"displayLabel"`
	Host          string `json:"host"`
	Port          int64  `json:"port"`
	UseStartTls   bool   `json:"useStartTls"`
	CaCertPem     string `json:"caCertPem"`
	BindDn        string `json:"bindDn"`
	BindPassword  string `json:"bindPassword"`
	BaseDn        string `json:"baseDn"`
	UserFilter    string `json:"userFilter"`
	GroupAttr     string `json:"groupAttr"`
	SubjectAttr   string `json:"subjectAttr"`
	Authoritative bool   `json:"authoritative"`
}

// DirectoryTestPayload lets the admin test edits BEFORE saving them: the submitted
// settings are used as-is, except a blank bind password falls back to the stored
// one (so testing after a config tweak doesn't require retyping the secret).
type DirectoryTestPayload struct {
	DirectoryConfigPayload
	SampleUsername string `json:"sampleUsername"`
}

// IDirectoryService interface
type IDirectoryService interface {
	GetView(ctx context.Context) (*DirectoryConfigView, error)
	Save(ctx context.Context, payload DirectoryConfigPayload, actorId int64) (*DirectoryConfigView, error)
	Test(ctx context.Context, payload DirectoryTestPayload) *login.LdapTestResult
	// LoginOption reports whether directory login should be offered and under what
	// label — consumed by the providers endpoint and both login pages.
	LoginOption(ctx context.Context) (enabled bool, label string)
	// AuthenticateLdap authenticates the credential pair against the directory,
	// resolves the account via UpsertFederated, and applies group→role mapping.
	AuthenticateLdap(ctx context.Context, username string, password string) (*entities.UserLogin, error)
	// ResolveDirectoryUser resolves an ALREADY-AUTHENTICATED username (Kerberos
	// SPNEGO proved the ticket) to its account: directory lookup WITHOUT any user
	// bind, then the same UpsertFederated + group→role tail as password logins —
	// so a person gets ONE account regardless of how they signed in. Callers MUST
	// have verified the username themselves; this performs no credential check.
	ResolveDirectoryUser(ctx context.Context, username string) (*entities.UserLogin, error)
}

type directoryService struct {
	repo     dbsql.IGenericRepo[myidsanentities.DirectoryConfig]
	mappings dbsql.IGenericRepo[myidsanentities.FederatedGroupMapping]
	users    IUserLoginService
	cipher   *atrest.Cipher
}

// Create new IDirectoryService. cipher may be nil (encryption-at-rest disabled);
// the bind password is then stored as-is, mirroring the suite-wide atrest fallback.
func NewDirectoryService(
	repo dbsql.IGenericRepo[myidsanentities.DirectoryConfig],
	mappings dbsql.IGenericRepo[myidsanentities.FederatedGroupMapping],
	users IUserLoginService,
	cipher *atrest.Cipher,
) IDirectoryService {
	return &directoryService{repo: repo, mappings: mappings, users: users, cipher: cipher}
}

func (m *directoryService) load(ctx context.Context) (*myidsanentities.DirectoryConfig, error) {
	cfg, err := m.repo.GetByUnique(ctx, "", "ukey1", directoryConfigName)
	if err != nil {
		if isNotFoundErr(err) {
			return nil, nil
		}
		return nil, err
	}
	if cfg == nil || cfg.Id == 0 {
		return nil, nil
	}
	return cfg, nil
}

func (m *directoryService) GetView(ctx context.Context) (*DirectoryConfigView, error) {
	cfg, err := m.load(ctx)
	if err != nil {
		return nil, err
	}
	if cfg == nil {
		// Unconfigured: hand the UI a zero view rather than a 404 so the form
		// renders empty and the first save creates the row.
		return &DirectoryConfigView{}, nil
	}
	return &DirectoryConfigView{
		Enabled:         cfg.Enabled,
		DisplayLabel:    cfg.DisplayLabel,
		Host:            cfg.Host,
		Port:            cfg.Port,
		UseStartTls:     cfg.UseStartTls,
		CaCertPem:       cfg.CaCertPem,
		BindDn:          cfg.BindDn,
		HasBindPassword: strings.TrimSpace(cfg.BindPassword) != "",
		BaseDn:          cfg.BaseDn,
		UserFilter:      cfg.UserFilter,
		GroupAttr:       cfg.GroupAttr,
		SubjectAttr:     cfg.SubjectAttr,
		Authoritative:   cfg.Authoritative,
	}, nil
}

func (m *directoryService) Save(ctx context.Context, payload DirectoryConfigPayload, actorId int64) (*DirectoryConfigView, error) {
	if payload.Enabled {
		settings := ldapSettingsFromPayload(payload)
		if err := settings.Validate(); err != nil {
			return nil, err
		}
	}

	existing, err := m.load(ctx)
	if err != nil {
		return nil, err
	}

	now := time.Now().Unix()
	row := myidsanentities.DirectoryConfig{
		Name:          directoryConfigName,
		Enabled:       payload.Enabled,
		DisplayLabel:  strings.TrimSpace(payload.DisplayLabel),
		Host:          strings.TrimSpace(payload.Host),
		Port:          payload.Port,
		UseStartTls:   payload.UseStartTls,
		CaCertPem:     strings.TrimSpace(payload.CaCertPem),
		BindDn:        strings.TrimSpace(payload.BindDn),
		BaseDn:        strings.TrimSpace(payload.BaseDn),
		UserFilter:    strings.TrimSpace(payload.UserFilter),
		GroupAttr:     strings.TrimSpace(payload.GroupAttr),
		SubjectAttr:   strings.TrimSpace(payload.SubjectAttr),
		Authoritative: payload.Authoritative,
		UpdatedBy:     actorId,
		UpdatedAt:     now,
	}

	if pwd := payload.BindPassword; strings.TrimSpace(pwd) != "" {
		enc, err := encodeDirectorySecret(m.cipher, pwd)
		if err != nil {
			return nil, err
		}
		row.BindPassword = enc
	} else if existing != nil {
		row.BindPassword = existing.BindPassword
	}

	if existing != nil {
		row.Id = existing.Id
		row.CreatedBy = existing.CreatedBy
		row.CreatedAt = existing.CreatedAt
		if _, err := m.repo.UpdateById(ctx, "", row); err != nil {
			return nil, err
		}
	} else {
		row.CreatedBy = actorId
		row.CreatedAt = now
		if _, err := m.repo.Create(ctx, "", row); err != nil {
			return nil, err
		}
	}

	return m.GetView(ctx)
}

func (m *directoryService) Test(ctx context.Context, payload DirectoryTestPayload) *login.LdapTestResult {
	settings := ldapSettingsFromPayload(payload.DirectoryConfigPayload)
	if strings.TrimSpace(payload.BindPassword) == "" {
		if existing, err := m.load(ctx); err == nil && existing != nil {
			settings.BindPassword = decodeDirectorySecret(m.cipher, existing.BindPassword)
		}
	}
	return login.LdapTest(ctx, settings, payload.SampleUsername)
}

func (m *directoryService) LoginOption(ctx context.Context) (bool, string) {
	cfg, err := m.load(ctx)
	if err != nil || cfg == nil || !cfg.Enabled {
		return false, ""
	}
	label := strings.TrimSpace(cfg.DisplayLabel)
	if label == "" {
		label = "Domain account"
	}
	return true, label
}

func (m *directoryService) AuthenticateLdap(ctx context.Context, username string, password string) (*entities.UserLogin, error) {
	cfg, settings, err := m.loadEnabled(ctx)
	if err != nil {
		return nil, err
	}
	identity, err := login.LdapAuthenticate(ctx, settings, username, password)
	if err != nil {
		return nil, err
	}
	return m.admitIdentity(ctx, cfg, identity)
}

func (m *directoryService) ResolveDirectoryUser(ctx context.Context, username string) (*entities.UserLogin, error) {
	cfg, settings, err := m.loadEnabled(ctx)
	if err != nil {
		return nil, err
	}
	identity, err := login.LdapLookup(ctx, settings, username)
	if err != nil {
		return nil, err
	}
	return m.admitIdentity(ctx, cfg, identity)
}

func (m *directoryService) loadEnabled(ctx context.Context) (*myidsanentities.DirectoryConfig, login.LdapSettings, error) {
	cfg, err := m.load(ctx)
	if err != nil {
		return nil, login.LdapSettings{}, err
	}
	if cfg == nil || !cfg.Enabled {
		return nil, login.LdapSettings{}, ErrDirectoryDisabled
	}
	settings := ldapSettingsFromConfig(cfg)
	settings.BindPassword = decodeDirectorySecret(m.cipher, cfg.BindPassword)
	return cfg, settings, nil
}

// admitIdentity is the shared tail of every directory-backed login: resolve the
// account via strict UpsertFederated, then apply group→role mapping.
func (m *directoryService) admitIdentity(ctx context.Context, cfg *myidsanentities.DirectoryConfig, identity *login.Identity) (*entities.UserLogin, error) {
	user, err := m.users.UpsertFederated(ctx, *identity)
	if err != nil {
		return nil, err
	}

	roleId, matched := m.resolveRole(ctx, identity.Provider, identity.Groups)
	// A mapping applies on every login when the directory is authoritative, and
	// only to still-pending accounts otherwise (manual assignments then stick).
	if matched && user.UserRoleId != roleId && (cfg.Authoritative || user.UserRoleId == 0) {
		if err := m.users.AssignRole(ctx, user.Id, roleId); err != nil {
			return nil, err
		}
		log.Printf("directory group mapping set role: user=%s role=%d authoritative=%v", user.Email, roleId, cfg.Authoritative)
		user.UserRoleId = roleId
	}

	return user, nil
}

func (m *directoryService) resolveRole(ctx context.Context, provider string, groups []string) (int64, bool) {
	if len(groups) == 0 {
		return 0, false
	}
	filters := []sqldataenums.Filter{
		{FieldName: "Provider", Compare: sqldataenums.Equal, Value: provider},
	}
	rows, _, err := m.mappings.Get(ctx, "", 1000, 0, filters, nil)
	if err != nil {
		if !isNotFoundErr(err) {
			log.Printf("directory group mapping lookup failed: %v", err)
		}
		return 0, false
	}
	return ResolveMappedRole(rows, groups)
}

// ResolveMappedRole picks the role for a login's group memberships: matching is
// case-insensitive on the full group name/DN; the highest Priority wins, ties break
// to the lowest Id so the outcome is deterministic. No match → (0, false): the
// caller must NOT touch the account's role.
func ResolveMappedRole(mappings []*myidsanentities.FederatedGroupMapping, groups []string) (int64, bool) {
	if len(mappings) == 0 || len(groups) == 0 {
		return 0, false
	}
	memberOf := make(map[string]bool, len(groups))
	for _, g := range groups {
		memberOf[strings.ToLower(strings.TrimSpace(g))] = true
	}

	matched := make([]*myidsanentities.FederatedGroupMapping, 0, 2)
	for _, mapping := range mappings {
		if mapping == nil || mapping.RoleId <= 0 {
			continue
		}
		if memberOf[strings.ToLower(strings.TrimSpace(mapping.GroupName))] {
			matched = append(matched, mapping)
		}
	}
	if len(matched) == 0 {
		return 0, false
	}
	sort.Slice(matched, func(i, j int) bool {
		if matched[i].Priority != matched[j].Priority {
			return matched[i].Priority > matched[j].Priority
		}
		return matched[i].Id < matched[j].Id
	})
	return matched[0].RoleId, true
}

func ldapSettingsFromPayload(p DirectoryConfigPayload) login.LdapSettings {
	return login.LdapSettings{
		Host:         strings.TrimSpace(p.Host),
		Port:         int(p.Port),
		UseStartTLS:  p.UseStartTls,
		CaCertPem:    p.CaCertPem,
		BindDn:       strings.TrimSpace(p.BindDn),
		BindPassword: p.BindPassword,
		BaseDn:       strings.TrimSpace(p.BaseDn),
		UserFilter:   strings.TrimSpace(p.UserFilter),
		GroupAttr:    strings.TrimSpace(p.GroupAttr),
		SubjectAttr:  strings.TrimSpace(p.SubjectAttr),
	}
}

func ldapSettingsFromConfig(cfg *myidsanentities.DirectoryConfig) login.LdapSettings {
	return login.LdapSettings{
		Host:        strings.TrimSpace(cfg.Host),
		Port:        int(cfg.Port),
		UseStartTLS: cfg.UseStartTls,
		CaCertPem:   cfg.CaCertPem,
		BindDn:      strings.TrimSpace(cfg.BindDn),
		BaseDn:      strings.TrimSpace(cfg.BaseDn),
		UserFilter:  strings.TrimSpace(cfg.UserFilter),
		GroupAttr:   strings.TrimSpace(cfg.GroupAttr),
		SubjectAttr: strings.TrimSpace(cfg.SubjectAttr),
	}
}

// encodeDirectorySecret / decodeDirectorySecret mirror the suite's at-rest secret
// convention (see apps/myseliasan/services/secret_store.go): AES-256-GCM via atrest,
// base64-wrapped for the TEXT column, with legacy/plaintext passthrough on read so
// enabling encryption later never locks out an existing config.
func encodeDirectorySecret(cipher *atrest.Cipher, plaintext string) (string, error) {
	if cipher == nil || plaintext == "" {
		return plaintext, nil
	}
	enc, err := cipher.EncryptBytes([]byte(plaintext))
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(enc), nil
}

func decodeDirectorySecret(cipher *atrest.Cipher, stored string) string {
	if stored == "" {
		return stored
	}
	raw, err := base64.StdEncoding.DecodeString(stored)
	if err != nil || !atrest.IsEncrypted(raw) {
		return stored
	}
	if cipher == nil {
		return stored
	}
	pt, err := cipher.DecryptBytes(raw)
	if err != nil {
		return stored
	}
	return string(pt)
}
