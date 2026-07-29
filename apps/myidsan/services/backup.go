package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	myidsanentities "github.com/mysayasan/kopiv2/apps/myidsan/entities"
	sharedentities "github.com/mysayasan/kopiv2/domain/entities"
	"github.com/mysayasan/kopiv2/infra/atrest"
	"github.com/mysayasan/kopiv2/infra/cache"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// Backup & Restore produces a portable, passphrase-encrypted snapshot of the identity
// store so a dead host can be rebuilt. myidsan is the one app in the suite where losing
// the database locks every employee out of every other app at once — it holds every user,
// every role, every registered SSO client, the SSO CA private key, all TOTP secrets, and
// the LDAP bind password — and until now the documented recovery procedure was "copy the
// database and secret/atrest.key yourself, as a pair".
//
// # How the at-rest secrets travel
//
// This is the design decision that makes the difference between a backup and an inert
// file. TOTP secrets and the LDAP bind password are sealed with the host's at-rest key
// (infra/atrest). Copying the sealed bytes into the archive would produce a backup that
// restores "successfully" onto a fresh host and then fails every second-factor check,
// because the new host has a different key — the worst possible outcome, since it is
// only discovered when someone tries to log in.
//
// So the sealed columns are UNSEALED on export (into the archive, which is itself
// encrypted with the operator's passphrase) and RE-SEALED with the destination host's own
// key on restore. The at-rest key itself is never included, which keeps the rule
// mymatasan's backup follows: secrets travel inside the encrypted archive, machine
// identity does not.
//
// # What is deliberately excluded
//
// Host-local state: config.json, TLS certificates, the at-rest key, api_log/runtime_log,
// password_reset_request (a transient operator queue; restoring stale reset requests
// would hand out temporary passwords nobody asked for), and user_session (sessions are
// cache-backed and are explicitly invalidated after a restore — see Restore).
const (
	// Logical, user-selectable sections. Order matters: it is also the order they are
	// collected and applied in, and later sections depend on ids remapped by earlier ones.
	BackupSectionAccess     = "access"     // access_role + access_role_permission
	BackupSectionIdentity   = "identity"   // user_login (password hashes) + user_group + user_avatar
	BackupSectionMfa        = "mfa"        // user_mfa_factor (TOTP secrets) + recovery codes
	BackupSectionApps       = "apps"       // app_registry + app_auth_config + app_redirect_uri
	BackupSectionFederation = "federation" // directory_config (bind password) + federated_group_mapping
	BackupSectionSsoCa      = "ssoca"      // sso_ca (CA certificate AND private key)

	backupApp           = "myidsan"
	backupSchemaVersion = 1
	backupFormatVersion = 1
	backupPageLimit     = 100000

	// RestoreModeReplace wipes the target tables of each selected section before
	// inserting; RestoreModeMerge appends without clearing.
	RestoreModeReplace = "replace"
	RestoreModeMerge   = "merge"

	// sessionCachePrefix must match the key middlewares/auth.go writes sessions under.
	sessionCachePrefix = "sso:session:"
)

// backupMagic prefixes every .idbackup file so a non-backup upload is rejected before any
// passphrase work happens.
var backupMagic = []byte("IDBK")

var backupAllSections = []string{
	BackupSectionAccess,
	BackupSectionIdentity,
	BackupSectionMfa,
	BackupSectionApps,
	BackupSectionFederation,
	BackupSectionSsoCa,
}

// BackupManifest is the human-readable header inside the encrypted payload; Preview
// surfaces it so the UI can show what a file holds before anything is applied.
type BackupManifest struct {
	App           string         `json:"app"`
	AppVersion    string         `json:"appVersion"`
	SchemaVersion int            `json:"schemaVersion"`
	CreatedAt     int64          `json:"createdAt"`
	Sections      []string       `json:"sections"`
	Counts        map[string]int `json:"counts"`
}

// backupUser re-exposes nothing extra — Userpwd is already a normal JSON field — but it
// exists so the shape stays stable if UserLogin ever hides the hash.
type backupUser struct {
	sharedentities.UserLogin
}

// backupAvatar re-exposes UserAvatar.DataB64, which is json:"-" on the entity.
type backupAvatar struct {
	myidsanentities.UserAvatar
	DataB64 string `json:"dataB64"`
}

// backupMfaFactor carries the TOTP secret in PLAINTEXT (unsealed on export, re-sealed on
// restore). SecretEnc is json:"-" on the entity and is deliberately not carried: the
// sealed bytes are useless on any other host.
type backupMfaFactor struct {
	myidsanentities.UserMfaFactor
	Secret string `json:"secret"`
}

// backupRecoveryCode re-exposes the json:"-" bcrypt CodeHash. The hash is portable as-is
// (bcrypt is not host-bound), so it is carried verbatim and unused codes stay usable.
type backupRecoveryCode struct {
	myidsanentities.UserMfaRecoveryCode
	CodeHash string `json:"codeHash"`
}

// backupDirectoryConfig carries the LDAP bind password in plaintext for the same reason
// as the TOTP secret.
type backupDirectoryConfig struct {
	myidsanentities.DirectoryConfig
	BindPassword string `json:"bindPassword"`
}

// backupFile is the decrypted payload.
type backupFile struct {
	Manifest    BackupManifest                          `json:"manifest"`
	Roles       []sharedentities.AccessRole             `json:"roles,omitempty"`
	Permissions []sharedentities.AccessRolePermission   `json:"permissions,omitempty"`
	Users       []backupUser                            `json:"users,omitempty"`
	Groups      []sharedentities.UserGroup              `json:"groups,omitempty"`
	Avatars     []backupAvatar                          `json:"avatars,omitempty"`
	MfaFactors  []backupMfaFactor                       `json:"mfaFactors,omitempty"`
	MfaCodes    []backupRecoveryCode                    `json:"mfaCodes,omitempty"`
	AppRegistry []sharedentities.AppRegistry            `json:"appRegistry,omitempty"`
	AppAuth     []sharedentities.AppAuthConfig          `json:"appAuth,omitempty"`
	AppRedirect []sharedentities.AppRedirectUri         `json:"appRedirect,omitempty"`
	Directory   []backupDirectoryConfig                 `json:"directory,omitempty"`
	GroupMaps   []myidsanentities.FederatedGroupMapping `json:"groupMaps,omitempty"`
	SsoCa       []myidsanentities.SsoCa                 `json:"ssoCa,omitempty"`
}

type BackupRequest struct {
	Sections   []string `json:"sections"`
	Passphrase string   `json:"passphrase"`
}

type RestoreRequest struct {
	Sections   []string `json:"sections"`
	Passphrase string   `json:"passphrase"`
	Mode       string   `json:"mode"`
}

type BackupSectionInfo struct {
	Id    string `json:"id"`
	Count int    `json:"count"`
}

// RestoreResult reports what was applied. SessionsInvalidated flags that every live SSO
// session was dropped, which is why the operator is signed out immediately afterwards.
type RestoreResult struct {
	Sections            []string       `json:"sections"`
	Restored            map[string]int `json:"restored"`
	Skipped             map[string]int `json:"skipped"`
	AppVersion          string         `json:"appVersion"`
	SchemaWarning       string         `json:"schemaWarning,omitempty"`
	SessionsInvalidated bool           `json:"sessionsInvalidated"`
	SessionWarning      string         `json:"sessionWarning,omitempty"`
	SetupCompleted      bool           `json:"setupCompleted"`
	SetupCompleteError  string         `json:"setupCompleteError,omitempty"`
}

// IBackupService exports and restores the passphrase-encrypted identity bundle.
type IBackupService interface {
	AvailableSections(ctx context.Context) ([]BackupSectionInfo, error)
	Export(ctx context.Context, req BackupRequest) ([]byte, error)
	Preview(ctx context.Context, data []byte, passphrase string) (BackupManifest, error)
	Restore(ctx context.Context, data []byte, req RestoreRequest) (RestoreResult, error)
}

type backupService struct {
	roles       dbsql.IGenericRepo[sharedentities.AccessRole]
	permissions dbsql.IGenericRepo[sharedentities.AccessRolePermission]
	users       dbsql.IGenericRepo[sharedentities.UserLogin]
	groups      dbsql.IGenericRepo[sharedentities.UserGroup]
	avatars     dbsql.IGenericRepo[myidsanentities.UserAvatar]
	mfaFactors  dbsql.IGenericRepo[myidsanentities.UserMfaFactor]
	mfaCodes    dbsql.IGenericRepo[myidsanentities.UserMfaRecoveryCode]
	appRegistry dbsql.IGenericRepo[sharedentities.AppRegistry]
	appAuth     dbsql.IGenericRepo[sharedentities.AppAuthConfig]
	appRedirect dbsql.IGenericRepo[sharedentities.AppRedirectUri]
	directory   dbsql.IGenericRepo[myidsanentities.DirectoryConfig]
	groupMaps   dbsql.IGenericRepo[myidsanentities.FederatedGroupMapping]
	ssoCa       dbsql.IGenericRepo[myidsanentities.SsoCa]

	// cipher seals/unseals the at-rest columns. Nil when encryption is disabled, in
	// which case the stored values are already plaintext and pass through untouched.
	cipher *atrest.Cipher
	// store is the session cache, wiped after a restore.
	store      cache.Store
	setup      ISetupStateService
	appVersion string
}

func NewBackupService(
	roles dbsql.IGenericRepo[sharedentities.AccessRole],
	permissions dbsql.IGenericRepo[sharedentities.AccessRolePermission],
	users dbsql.IGenericRepo[sharedentities.UserLogin],
	groups dbsql.IGenericRepo[sharedentities.UserGroup],
	avatars dbsql.IGenericRepo[myidsanentities.UserAvatar],
	mfaFactors dbsql.IGenericRepo[myidsanentities.UserMfaFactor],
	mfaCodes dbsql.IGenericRepo[myidsanentities.UserMfaRecoveryCode],
	appRegistry dbsql.IGenericRepo[sharedentities.AppRegistry],
	appAuth dbsql.IGenericRepo[sharedentities.AppAuthConfig],
	appRedirect dbsql.IGenericRepo[sharedentities.AppRedirectUri],
	directory dbsql.IGenericRepo[myidsanentities.DirectoryConfig],
	groupMaps dbsql.IGenericRepo[myidsanentities.FederatedGroupMapping],
	ssoCa dbsql.IGenericRepo[myidsanentities.SsoCa],
	cipher *atrest.Cipher,
	store cache.Store,
	setup ISetupStateService,
	appVersion string,
) IBackupService {
	return &backupService{
		roles: roles, permissions: permissions, users: users, groups: groups,
		avatars: avatars, mfaFactors: mfaFactors, mfaCodes: mfaCodes,
		appRegistry: appRegistry, appAuth: appAuth, appRedirect: appRedirect,
		directory: directory, groupMaps: groupMaps, ssoCa: ssoCa,
		cipher: cipher, store: store, setup: setup, appVersion: appVersion,
	}
}

func (s *backupService) AvailableSections(ctx context.Context) ([]BackupSectionInfo, error) {
	out := make([]BackupSectionInfo, 0, len(backupAllSections))
	for _, section := range backupAllSections {
		count, err := s.countSection(ctx, section)
		if err != nil {
			return nil, err
		}
		out = append(out, BackupSectionInfo{Id: section, Count: count})
	}
	return out, nil
}

func (s *backupService) countSection(ctx context.Context, section string) (int, error) {
	switch section {
	case BackupSectionAccess:
		return countRows(ctx, s.roles)
	case BackupSectionIdentity:
		return countRows(ctx, s.users)
	case BackupSectionMfa:
		return countRows(ctx, s.mfaFactors)
	case BackupSectionApps:
		return countRows(ctx, s.appRegistry)
	case BackupSectionFederation:
		return countRows(ctx, s.groupMaps)
	case BackupSectionSsoCa:
		return countRows(ctx, s.ssoCa)
	}
	return 0, nil
}

func (s *backupService) Export(ctx context.Context, req BackupRequest) ([]byte, error) {
	sections := normalizeSections(req.Sections)
	if len(sections) == 0 {
		return nil, errors.New("select at least one section to back up")
	}
	if strings.TrimSpace(req.Passphrase) == "" {
		return nil, errors.New("a passphrase is required to protect the backup")
	}

	file := backupFile{
		Manifest: BackupManifest{
			App:           backupApp,
			AppVersion:    s.appVersion,
			SchemaVersion: backupSchemaVersion,
			CreatedAt:     time.Now().UTC().Unix(),
			Sections:      sections,
			Counts:        map[string]int{},
		},
	}

	for _, section := range sections {
		if err := s.collect(ctx, &file, section); err != nil {
			return nil, err
		}
	}

	plain, err := json.Marshal(file)
	if err != nil {
		return nil, err
	}
	enc, err := atrest.EncryptWithPassphrase(req.Passphrase, plain)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, len(backupMagic)+1+len(enc))
	out = append(out, backupMagic...)
	out = append(out, backupFormatVersion)
	out = append(out, enc...)
	return out, nil
}

func (s *backupService) collect(ctx context.Context, file *backupFile, section string) error {
	switch section {
	case BackupSectionAccess:
		roles, err := allRows(ctx, s.roles)
		if err != nil {
			return err
		}
		file.Roles = roles
		perms, err := allRows(ctx, s.permissions)
		if err != nil {
			return err
		}
		file.Permissions = perms
		file.Manifest.Counts[section] = len(roles)

	case BackupSectionIdentity:
		users, err := allRows(ctx, s.users)
		if err != nil {
			return err
		}
		for _, u := range users {
			file.Users = append(file.Users, backupUser{UserLogin: u})
		}
		groups, err := allRows(ctx, s.groups)
		if err != nil {
			return err
		}
		file.Groups = groups
		avatars, err := allRows(ctx, s.avatars)
		if err != nil {
			return err
		}
		for _, a := range avatars {
			file.Avatars = append(file.Avatars, backupAvatar{UserAvatar: a, DataB64: a.DataB64})
		}
		file.Manifest.Counts[section] = len(users)

	case BackupSectionMfa:
		factors, err := allRows(ctx, s.mfaFactors)
		if err != nil {
			return err
		}
		for _, f := range factors {
			// Unseal here: the sealed bytes are bound to THIS host's at-rest key and
			// would be undecryptable after a restore onto a new host.
			file.MfaFactors = append(file.MfaFactors, backupMfaFactor{
				UserMfaFactor: f,
				Secret:        decodeSecret(s.cipher, f.SecretEnc),
			})
		}
		codes, err := allRows(ctx, s.mfaCodes)
		if err != nil {
			return err
		}
		for _, c := range codes {
			file.MfaCodes = append(file.MfaCodes, backupRecoveryCode{UserMfaRecoveryCode: c, CodeHash: c.CodeHash})
		}
		file.Manifest.Counts[section] = len(factors)

	case BackupSectionApps:
		reg, err := allRows(ctx, s.appRegistry)
		if err != nil {
			return err
		}
		file.AppRegistry = reg
		auth, err := allRows(ctx, s.appAuth)
		if err != nil {
			return err
		}
		file.AppAuth = auth
		redir, err := allRows(ctx, s.appRedirect)
		if err != nil {
			return err
		}
		file.AppRedirect = redir
		file.Manifest.Counts[section] = len(reg)

	case BackupSectionFederation:
		dirs, err := allRows(ctx, s.directory)
		if err != nil {
			return err
		}
		for _, d := range dirs {
			file.Directory = append(file.Directory, backupDirectoryConfig{
				DirectoryConfig: d,
				BindPassword:    decodeSecret(s.cipher, d.BindPassword),
			})
		}
		maps, err := allRows(ctx, s.groupMaps)
		if err != nil {
			return err
		}
		file.GroupMaps = maps
		file.Manifest.Counts[section] = len(maps)

	case BackupSectionSsoCa:
		cas, err := allRows(ctx, s.ssoCa)
		if err != nil {
			return err
		}
		file.SsoCa = cas
		file.Manifest.Counts[section] = len(cas)
	}
	return nil
}

func (s *backupService) Preview(ctx context.Context, data []byte, passphrase string) (BackupManifest, error) {
	file, err := s.decode(data, passphrase)
	if err != nil {
		return BackupManifest{}, err
	}
	return file.Manifest, nil
}

func (s *backupService) decode(data []byte, passphrase string) (*backupFile, error) {
	if len(data) < len(backupMagic)+1 || !bytes.Equal(data[:len(backupMagic)], backupMagic) {
		return nil, errors.New("this is not a myidsan backup file")
	}
	if data[len(backupMagic)] != backupFormatVersion {
		return nil, fmt.Errorf("unsupported backup format version %d", data[len(backupMagic)])
	}
	if strings.TrimSpace(passphrase) == "" {
		return nil, errors.New("a passphrase is required to open the backup")
	}
	plain, err := atrest.DecryptWithPassphrase(passphrase, data[len(backupMagic)+1:])
	if err != nil {
		return nil, err
	}
	var file backupFile
	if err := json.Unmarshal(plain, &file); err != nil {
		return nil, fmt.Errorf("backup contents are corrupt: %w", err)
	}
	if file.Manifest.App != backupApp {
		return nil, errors.New("this backup was not created by myidsan")
	}
	return &file, nil
}

func (s *backupService) Restore(ctx context.Context, data []byte, req RestoreRequest) (RestoreResult, error) {
	file, err := s.decode(data, req.Passphrase)
	if err != nil {
		return RestoreResult{}, err
	}
	sections := restoreSections(req.Sections, file.Manifest.Sections)
	if len(sections) == 0 {
		return RestoreResult{}, errors.New("none of the selected sections are present in this backup")
	}
	mode := strings.ToLower(strings.TrimSpace(req.Mode))
	if mode != RestoreModeMerge {
		mode = RestoreModeReplace
	}

	res := RestoreResult{
		Sections:   sections,
		Restored:   map[string]int{},
		Skipped:    map[string]int{},
		AppVersion: file.Manifest.AppVersion,
	}
	if file.Manifest.SchemaVersion != backupSchemaVersion {
		res.SchemaWarning = fmt.Sprintf(
			"backup schema v%d differs from this app's v%d; restore proceeded best-effort",
			file.Manifest.SchemaVersion, backupSchemaVersion)
	}

	// Ordering is a hard dependency, not a preference:
	//   roles -> users (UserRoleId points at a role)
	//         -> mfa/avatars (UserLoginId points at a user)
	//   apps  -> auth configs -> redirect URIs
	//   roles -> federated group mappings (RoleId)
	roleIDs := map[int64]int64{}
	userIDs := map[int64]int64{}
	appIDs := map[int64]int64{}
	authIDs := map[int64]int64{}

	if containsString(sections, BackupSectionAccess) {
		if err := s.restoreAccess(ctx, file, mode, roleIDs, &res); err != nil {
			return res, err
		}
	}
	if containsString(sections, BackupSectionIdentity) {
		if err := s.restoreIdentity(ctx, file, mode, roleIDs, userIDs, &res); err != nil {
			return res, err
		}
	}
	if containsString(sections, BackupSectionMfa) {
		if err := s.restoreMfa(ctx, file, mode, userIDs, &res); err != nil {
			return res, err
		}
	}
	if containsString(sections, BackupSectionApps) {
		if err := s.restoreApps(ctx, file, mode, appIDs, authIDs, &res); err != nil {
			return res, err
		}
	}
	if containsString(sections, BackupSectionFederation) {
		if err := s.restoreFederation(ctx, file, mode, roleIDs, &res); err != nil {
			return res, err
		}
	}
	if containsString(sections, BackupSectionSsoCa) {
		if err := s.restoreSsoCa(ctx, file, mode, &res); err != nil {
			return res, err
		}
	}

	// Every live session now refers to user ids and roles that may no longer mean what
	// they did a moment ago. Sessions are cache-backed and are NOT part of the backup, so
	// drop them all: a stale session that still resolves would carry pre-restore authority.
	if s.store != nil {
		if err := s.store.DeleteByPrefix(ctx, sessionCachePrefix); err != nil {
			res.SessionWarning = err.Error()
		} else {
			res.SessionsInvalidated = true
		}
	}

	// A restored instance is already configured, so the first-run wizard must not
	// reappear. Best-effort — the restore itself has already succeeded by this point.
	if s.setup != nil {
		if _, err := s.setup.Complete(ctx); err != nil {
			res.SetupCompleteError = err.Error()
		} else {
			res.SetupCompleted = true
		}
	}
	return res, nil
}

func (s *backupService) restoreAccess(ctx context.Context, file *backupFile, mode string, roleIDs map[int64]int64, res *RestoreResult) error {
	if mode == RestoreModeReplace {
		// Permissions reference roles, so clear the children first.
		if err := wipeAll(ctx, s.permissions, func(p *sharedentities.AccessRolePermission) int64 { return p.Id }); err != nil {
			return err
		}
		if err := wipeAll(ctx, s.roles, func(r *sharedentities.AccessRole) int64 { return r.Id }); err != nil {
			return err
		}
	}

	for _, role := range file.Roles {
		oldID := role.Id
		row := role
		row.Id = 0
		newID, err := s.roles.Create(ctx, "", row)
		if err != nil {
			return err
		}
		roleIDs[oldID] = int64(newID)
		res.Restored[BackupSectionAccess]++
	}

	for _, perm := range file.Permissions {
		newRoleID, ok := roleIDs[perm.RoleId]
		if !ok {
			// The permission's role was not in this backup; a dangling grant would be
			// worse than a missing one.
			res.Skipped[BackupSectionAccess]++
			continue
		}
		row := perm
		row.Id = 0
		row.RoleId = newRoleID
		if _, err := s.permissions.Create(ctx, "", row); err != nil {
			return err
		}
	}
	return nil
}

func (s *backupService) restoreIdentity(ctx context.Context, file *backupFile, mode string, roleIDs, userIDs map[int64]int64, res *RestoreResult) error {
	if mode == RestoreModeReplace {
		if err := wipeAll(ctx, s.avatars, func(a *myidsanentities.UserAvatar) int64 { return a.Id }); err != nil {
			return err
		}
		if err := wipeAll(ctx, s.users, func(u *sharedentities.UserLogin) int64 { return u.Id }); err != nil {
			return err
		}
		if err := wipeAll(ctx, s.groups, func(g *sharedentities.UserGroup) int64 { return g.Id }); err != nil {
			return err
		}
	}

	for _, group := range file.Groups {
		row := group
		row.Id = 0
		if _, err := s.groups.Create(ctx, "", row); err != nil {
			return err
		}
	}

	avatarByUser := map[int64]backupAvatar{}
	for _, a := range file.Avatars {
		avatarByUser[a.UserLoginId] = a
	}

	for _, bu := range file.Users {
		oldID := bu.UserLogin.Id
		row := bu.UserLogin
		row.Id = 0
		// Remap the role reference. A user whose role is absent lands role-less rather
		// than inheriting whatever role happens to occupy that id on the new host —
		// silently granting someone another role is the worse failure.
		if row.UserRoleId != 0 {
			if mapped, ok := roleIDs[row.UserRoleId]; ok {
				row.UserRoleId = mapped
			} else {
				row.UserRoleId = 0
				res.Skipped[BackupSectionIdentity]++
			}
		}
		newID, err := s.users.Create(ctx, "", row)
		if err != nil {
			return err
		}
		userIDs[oldID] = int64(newID)
		res.Restored[BackupSectionIdentity]++

		if av, ok := avatarByUser[oldID]; ok {
			arow := av.UserAvatar
			arow.Id = 0
			arow.UserLoginId = int64(newID)
			arow.DataB64 = av.DataB64
			if _, err := s.avatars.Create(ctx, "", arow); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *backupService) restoreMfa(ctx context.Context, file *backupFile, mode string, userIDs map[int64]int64, res *RestoreResult) error {
	if mode == RestoreModeReplace {
		if err := wipeAll(ctx, s.mfaCodes, func(c *myidsanentities.UserMfaRecoveryCode) int64 { return c.Id }); err != nil {
			return err
		}
		if err := wipeAll(ctx, s.mfaFactors, func(f *myidsanentities.UserMfaFactor) int64 { return f.Id }); err != nil {
			return err
		}
	}

	for _, bf := range file.MfaFactors {
		newUserID, ok := userIDs[bf.UserLoginId]
		if !ok {
			// An orphaned factor would gate a login for a user that no longer exists.
			res.Skipped[BackupSectionMfa]++
			continue
		}
		row := bf.UserMfaFactor
		row.Id = 0
		row.UserLoginId = newUserID
		// Re-seal with THIS host's key. This is the step that makes the restore usable.
		sealed, err := encodeSecret(s.cipher, bf.Secret)
		if err != nil {
			return err
		}
		row.SecretEnc = sealed
		if _, err := s.mfaFactors.Create(ctx, "", row); err != nil {
			return err
		}
		res.Restored[BackupSectionMfa]++
	}

	for _, bc := range file.MfaCodes {
		newUserID, ok := userIDs[bc.UserLoginId]
		if !ok {
			res.Skipped[BackupSectionMfa]++
			continue
		}
		row := bc.UserMfaRecoveryCode
		row.Id = 0
		row.UserLoginId = newUserID
		row.CodeHash = bc.CodeHash
		if _, err := s.mfaCodes.Create(ctx, "", row); err != nil {
			return err
		}
	}
	return nil
}

func (s *backupService) restoreApps(ctx context.Context, file *backupFile, mode string, appIDs, authIDs map[int64]int64, res *RestoreResult) error {
	if mode == RestoreModeReplace {
		if err := wipeAll(ctx, s.appRedirect, func(r *sharedentities.AppRedirectUri) int64 { return r.Id }); err != nil {
			return err
		}
		if err := wipeAll(ctx, s.appAuth, func(a *sharedentities.AppAuthConfig) int64 { return a.Id }); err != nil {
			return err
		}
		if err := wipeAll(ctx, s.appRegistry, func(a *sharedentities.AppRegistry) int64 { return a.Id }); err != nil {
			return err
		}
	}

	for _, app := range file.AppRegistry {
		oldID := app.Id
		row := app
		row.Id = 0
		newID, err := s.appRegistry.Create(ctx, "", row)
		if err != nil {
			return err
		}
		appIDs[oldID] = int64(newID)
		res.Restored[BackupSectionApps]++
	}

	for _, auth := range file.AppAuth {
		newAppID, ok := appIDs[auth.AppRegistryId]
		if !ok {
			res.Skipped[BackupSectionApps]++
			continue
		}
		oldID := auth.Id
		row := auth
		row.Id = 0
		row.AppRegistryId = newAppID
		newID, err := s.appAuth.Create(ctx, "", row)
		if err != nil {
			return err
		}
		authIDs[oldID] = int64(newID)
	}

	for _, uri := range file.AppRedirect {
		newAuthID, ok := authIDs[uri.AppAuthConfigId]
		if !ok {
			// A redirect URI attached to nothing is not just useless: redirect URIs are
			// the allow-list the authorize endpoint checks, so a dangling one is a
			// security-relevant leftover.
			res.Skipped[BackupSectionApps]++
			continue
		}
		row := uri
		row.Id = 0
		row.AppAuthConfigId = newAuthID
		if _, err := s.appRedirect.Create(ctx, "", row); err != nil {
			return err
		}
	}
	return nil
}

func (s *backupService) restoreFederation(ctx context.Context, file *backupFile, mode string, roleIDs map[int64]int64, res *RestoreResult) error {
	if mode == RestoreModeReplace {
		if err := wipeAll(ctx, s.groupMaps, func(m *myidsanentities.FederatedGroupMapping) int64 { return m.Id }); err != nil {
			return err
		}
		if err := wipeAll(ctx, s.directory, func(d *myidsanentities.DirectoryConfig) int64 { return d.Id }); err != nil {
			return err
		}
	}

	for _, bd := range file.Directory {
		row := bd.DirectoryConfig
		row.Id = 0
		sealed, err := encodeSecret(s.cipher, bd.BindPassword)
		if err != nil {
			return err
		}
		row.BindPassword = sealed
		if _, err := s.directory.Create(ctx, "", row); err != nil {
			return err
		}
		res.Restored[BackupSectionFederation]++
	}

	for _, m := range file.GroupMaps {
		newRoleID, ok := roleIDs[m.RoleId]
		if !ok {
			// Mapping a directory group onto an unknown role would grant whatever role
			// now sits at that id — exactly the privilege-escalation shape to avoid.
			res.Skipped[BackupSectionFederation]++
			continue
		}
		row := m
		row.Id = 0
		row.RoleId = newRoleID
		if _, err := s.groupMaps.Create(ctx, "", row); err != nil {
			return err
		}
		res.Restored[BackupSectionFederation]++
	}
	return nil
}

func (s *backupService) restoreSsoCa(ctx context.Context, file *backupFile, mode string, res *RestoreResult) error {
	if mode == RestoreModeReplace {
		if err := wipeAll(ctx, s.ssoCa, func(c *myidsanentities.SsoCa) int64 { return c.Id }); err != nil {
			return err
		}
	}
	for _, ca := range file.SsoCa {
		row := ca
		row.Id = 0
		if _, err := s.ssoCa.Create(ctx, "", row); err != nil {
			return err
		}
		res.Restored[BackupSectionSsoCa]++
	}
	return nil
}

// --- helpers ---------------------------------------------------------------

func allRows[T any](ctx context.Context, repo dbsql.IGenericRepo[T]) ([]T, error) {
	rows, _, err := repo.Get(ctx, "", backupPageLimit, 0, nil, nil)
	if err != nil {
		return nil, err
	}
	out := make([]T, 0, len(rows))
	for _, row := range rows {
		if row != nil {
			out = append(out, *row)
		}
	}
	return out, nil
}

func countRows[T any](ctx context.Context, repo dbsql.IGenericRepo[T]) (int, error) {
	rows, err := allRows(ctx, repo)
	if err != nil {
		return 0, err
	}
	return len(rows), nil
}

func wipeAll[T any](ctx context.Context, repo dbsql.IGenericRepo[T], id func(*T) int64) error {
	rows, _, err := repo.Get(ctx, "", backupPageLimit, 0, nil, nil)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if row == nil {
			continue
		}
		if _, err := repo.DeleteById(ctx, "", uint64(id(row))); err != nil {
			return err
		}
	}
	return nil
}

// normalizeSections lower-cases, de-dupes, and reorders the requested sections into the
// canonical order, dropping anything unrecognized. An empty request means "everything".
func normalizeSections(in []string) []string {
	if len(in) == 0 {
		return append([]string(nil), backupAllSections...)
	}
	want := map[string]bool{}
	for _, s := range in {
		want[strings.ToLower(strings.TrimSpace(s))] = true
	}
	out := make([]string, 0, len(backupAllSections))
	for _, s := range backupAllSections {
		if want[s] {
			out = append(out, s)
		}
	}
	return out
}

// restoreSections intersects what the caller asked for with what the file actually holds,
// preserving canonical order.
func restoreSections(requested, present []string) []string {
	presentSet := map[string]bool{}
	for _, s := range present {
		presentSet[strings.ToLower(strings.TrimSpace(s))] = true
	}
	candidates := normalizeSections(requested)
	out := make([]string, 0, len(candidates))
	for _, s := range candidates {
		if presentSet[s] {
			out = append(out, s)
		}
	}
	return out
}

func containsString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
