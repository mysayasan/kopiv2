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
	sharedservices "github.com/mysayasan/kopiv2/domain/shared/services"
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
	// BackupSectionMfa covers EVERY second factor, not only TOTP: user_mfa_factor (sealed
	// secrets) + recovery codes + user_webauthn_credential (security keys). Security keys
	// ride in this section rather than one of their own because an operator selecting "mfa"
	// means "the second factors", and leaving keys out would restore an account whose only
	// factor had silently vanished — a lockout under a required-MFA policy.
	BackupSectionMfa        = "mfa"
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

// backupWebauthnCredential re-exposes UserWebauthnCredential.PublicKey, which is json:"-"
// on the entity so it never rides along in a REST projection.
//
// The wrapper is needed for MARSHALLING, not for sealing — a distinction worth stating
// because conflating the two is exactly how this was got wrong once already. A WebAuthn
// public key needs no unseal/re-seal (it is not a secret and is not bound to a host key),
// and that correctly means no cipher work — but it does NOT mean the field survives
// json.Marshal. Without this wrapper the archive carried the credential id and dropped the
// key, so a restore reported success, the browser still offered the key, and every
// assertion then failed signature verification: the precise lockout the re-sealing logic
// exists to prevent, arriving by a different route.
type backupWebauthnCredential struct {
	myidsanentities.UserWebauthnCredential
	PublicKey string `json:"publicKey"`
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
	// WebAuthnCreds needs no unseal/re-seal dance, unlike MfaFactors above: a WebAuthn
	// credential is a PUBLIC key, so nothing in it is bound to the exporting host's at-rest
	// key. It DOES need the wrapper, because the public key is json:"-" on the entity — see
	// backupWebauthnCredential.
	//
	// It carries one host-coupling of a different kind: a credential is bound to the
	// Relying Party ID it was created under. Restored onto a host answering on a different
	// name (with webauthn.relyingPartyId left to derive), the browser will refuse these keys
	// — set relyingPartyId explicitly if a DR host will not share the original hostname.
	WebAuthnCreds []backupWebauthnCredential `json:"webauthnCreds,omitempty"`
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
	webauthn    dbsql.IGenericRepo[myidsanentities.UserWebauthnCredential]
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
	setup      sharedservices.ISetupStateService
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
	webauthnCreds dbsql.IGenericRepo[myidsanentities.UserWebauthnCredential],
	appRegistry dbsql.IGenericRepo[sharedentities.AppRegistry],
	appAuth dbsql.IGenericRepo[sharedentities.AppAuthConfig],
	appRedirect dbsql.IGenericRepo[sharedentities.AppRedirectUri],
	directory dbsql.IGenericRepo[myidsanentities.DirectoryConfig],
	groupMaps dbsql.IGenericRepo[myidsanentities.FederatedGroupMapping],
	ssoCa dbsql.IGenericRepo[myidsanentities.SsoCa],
	cipher *atrest.Cipher,
	store cache.Store,
	setup sharedservices.ISetupStateService,
	appVersion string,
) IBackupService {
	return &backupService{
		roles: roles, permissions: permissions, users: users, groups: groups,
		avatars: avatars, mfaFactors: mfaFactors, mfaCodes: mfaCodes, webauthn: webauthnCreds,
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
		// Security keys travel verbatim — public keys, nothing to unseal. The public key is
		// re-exposed through the wrapper because it is json:"-" on the entity and would
		// otherwise be silently dropped by json.Marshal, leaving a credential that restores
		// but can never verify.
		if s.webauthn != nil {
			creds, err := allRows(ctx, s.webauthn)
			if err != nil {
				return err
			}
			for _, c := range creds {
				file.WebAuthnCreds = append(file.WebAuthnCreds, backupWebauthnCredential{
					UserWebauthnCredential: c,
					PublicKey:              c.PublicKey,
				})
			}
		}
		// The count reported to the operator is every second factor in the archive, so a
		// user holding only a security key is not reported as zero.
		file.Manifest.Counts[section] = len(factors) + len(file.WebAuthnCreds)

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

	// A section can be restored WITHOUT the section it depends on — the UI lets the
	// operator tick "mfa" and nothing else, and "restore the second factors onto the
	// accounts already here" is a perfectly reasonable thing to ask for.
	//
	// The id maps above are filled only by the restore of the parent section, so when that
	// parent was not selected every child row found no mapping and was skipped. In REPLACE
	// mode that was destructive and silent: selecting only "mfa" wiped user_mfa_factor and
	// every recovery code, restored nothing (there was no user mapping to place them on),
	// and reported success with a skipped count. Every account on the server lost its
	// second factor — a fleet-wide lockout under a required-MFA policy, and a silent
	// security downgrade of every account otherwise.
	//
	// So an unselected parent is resolved against what is ALREADY on this host, by the same
	// natural key the restore uses everywhere else: an account is its email, a role is its
	// name. A child whose parent is absent from the backup AND from this host still skips,
	// which is the case the skipping was written for.
	if !containsString(sections, BackupSectionAccess) {
		if err := s.matchExistingRoles(ctx, file, roleIDs); err != nil {
			return res, err
		}
	}
	if !containsString(sections, BackupSectionIdentity) {
		if err := s.matchExistingUsers(ctx, file, userIDs); err != nil {
			return res, err
		}
	}

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

	roleKeys, err := newKeyIndex(ctx, s.roles, func(r *sharedentities.AccessRole) string {
		return normKey(r.Name)
	})
	if err != nil {
		return err
	}
	for _, role := range file.Roles {
		oldID := role.Id
		row := role
		row.Id = 0
		if !roleKeys.claim(normKey(row.Name)) {
			// A role of this name is already here. Keeping the target's is the safe half of
			// the choice: overwriting it would silently redefine what a live role GRANTS,
			// and every account already carrying it would change privilege without anybody
			// asking for that.
			res.Skipped[BackupSectionAccess]++
			continue
		}
		newID, err := s.roles.Create(ctx, "", row)
		if err != nil {
			return restoreFailure(BackupSectionAccess, res, err)
		}
		roleIDs[oldID] = int64(newID)
		res.Restored[BackupSectionAccess]++
	}

	permKeys, err := newKeyIndex(ctx, s.permissions, func(p *sharedentities.AccessRolePermission) string {
		return normKey(fmt.Sprint(p.RoleId), p.Path)
	})
	if err != nil {
		return err
	}
	for _, perm := range file.Permissions {
		newRoleID, ok := roleIDs[perm.RoleId]
		if !ok {
			// The permission's role was not in this backup, or was skipped as already
			// present; a dangling grant would be worse than a missing one.
			res.Skipped[BackupSectionAccess]++
			continue
		}
		row := perm
		row.Id = 0
		row.RoleId = newRoleID
		// (role, path) carries no database constraint, so a repeated merge would quietly
		// stack duplicate grants for the same endpoint — two rows that disagree about
		// `managed` are then indistinguishable to the permission matrix.
		if !permKeys.claim(normKey(fmt.Sprint(row.RoleId), row.Path)) {
			res.Skipped[BackupSectionAccess]++
			continue
		}
		if _, err := s.permissions.Create(ctx, "", row); err != nil {
			return restoreFailure(BackupSectionAccess, res, err)
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

	// user_group carries no unique constraint, so a merge genuinely can hold both — but
	// re-running the same merge would otherwise stack identical groups forever, so the
	// name is treated as the natural key here too.
	groupKeys, err := newKeyIndex(ctx, s.groups, func(g *sharedentities.UserGroup) string {
		return normKey(g.Title, fmt.Sprint(g.ParentId))
	})
	if err != nil {
		return err
	}
	for _, group := range file.Groups {
		row := group
		row.Id = 0
		if !groupKeys.claim(normKey(row.Title, fmt.Sprint(row.ParentId))) {
			res.Skipped[BackupSectionIdentity]++
			continue
		}
		if _, err := s.groups.Create(ctx, "", row); err != nil {
			return restoreFailure(BackupSectionIdentity, res, err)
		}
	}

	avatarByUser := map[int64]backupAvatar{}
	for _, a := range file.Avatars {
		avatarByUser[a.UserLoginId] = a
	}

	userKeys, err := newKeyIndex(ctx, s.users, func(u *sharedentities.UserLogin) string {
		return normKey(u.Email)
	})
	if err != nil {
		return err
	}
	for _, bu := range file.Users {
		oldID := bu.UserLogin.Id
		row := bu.UserLogin
		row.Id = 0
		if !userKeys.claim(normKey(row.Email)) {
			// An account with this email is already here. The target's password, role and
			// second factor stand: overwriting a live account from a backup is how a stale
			// file silently rolls someone's credentials back, and merge is the mode that
			// exists precisely to NOT do that. Nothing keys off this user afterwards
			// either — it is absent from userIDs, so its factors and avatar skip too,
			// rather than attaching to the account that is already here.
			res.Skipped[BackupSectionIdentity]++
			continue
		}
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
			return restoreFailure(BackupSectionIdentity, res, err)
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
		if s.webauthn != nil {
			if err := wipeAll(ctx, s.webauthn, func(c *myidsanentities.UserWebauthnCredential) int64 { return c.Id }); err != nil {
				return err
			}
		}
	}

	// One confirmed factor per (account, kind) — loadFactor takes the first row it finds,
	// so a second would make which factor gates a login depend on insert order.
	factorKeys, err := newKeyIndex(ctx, s.mfaFactors, func(f *myidsanentities.UserMfaFactor) string {
		return normKey(fmt.Sprint(f.UserLoginId), f.Kind)
	})
	if err != nil {
		return err
	}
	codeKeys, err := newKeyIndex(ctx, s.mfaCodes, func(c *myidsanentities.UserMfaRecoveryCode) string {
		return normKey(fmt.Sprint(c.UserLoginId), c.CodeHash)
	})
	if err != nil {
		return err
	}
	for _, bf := range file.MfaFactors {
		newUserID, ok := userIDs[bf.UserLoginId]
		if !ok {
			// An orphaned factor would gate a login for a user that no longer exists — and
			// in merge mode this is also the path a factor takes when its account was
			// already present and therefore skipped, which is right: the account that is
			// here keeps the second factor it already has.
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
		if !factorKeys.claim(normKey(fmt.Sprint(row.UserLoginId), row.Kind)) {
			// This account already holds a factor of this kind — reached when the accounts
			// were matched rather than restored (a merge, or an mfa-only restore). Leaving
			// the live one in place is the safe half: it is the one the person actually has
			// on their phone.
			res.Skipped[BackupSectionMfa]++
			continue
		}
		if _, err := s.mfaFactors.Create(ctx, "", row); err != nil {
			return restoreFailure(BackupSectionMfa, res, err)
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
		if !codeKeys.claim(normKey(fmt.Sprint(row.UserLoginId), row.CodeHash)) {
			res.Skipped[BackupSectionMfa]++
			continue
		}
		if _, err := s.mfaCodes.Create(ctx, "", row); err != nil {
			return restoreFailure(BackupSectionMfa, res, err)
		}
	}

	// Security keys. Restored verbatim apart from the foreign-key remap — no re-sealing,
	// because the stored public key is not sealed to begin with.
	if s.webauthn != nil {
		credKeys, err := newKeyIndex(ctx, s.webauthn, func(c *myidsanentities.UserWebauthnCredential) string {
			return normKey(c.CredentialId)
		})
		if err != nil {
			return err
		}
		for _, cred := range file.WebAuthnCreds {
			newUserID, ok := userIDs[cred.UserLoginId]
			if !ok {
				// An orphaned credential would still occupy its globally-unique
				// CredentialId, which would then refuse to re-enrol that physical key.
				res.Skipped[BackupSectionMfa]++
				continue
			}
			row := cred.UserWebauthnCredential
			row.Id = 0
			row.UserLoginId = newUserID
			// Restore the public key from the WRAPPER field: unmarshalling the embedded
			// entity leaves PublicKey empty (json:"-"), and a credential without its key
			// verifies nothing.
			row.PublicKey = cred.PublicKey
			// CredentialId is what the browser presents to say which key it is using, and it
			// is globally unique — two rows for one physical key is not a state to create.
			if !credKeys.claim(normKey(row.CredentialId)) {
				res.Skipped[BackupSectionMfa]++
				continue
			}
			if _, err := s.webauthn.Create(ctx, "", row); err != nil {
				return restoreFailure(BackupSectionMfa, res, err)
			}
			res.Restored[BackupSectionMfa]++
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

	// app_registry carries TWO separate unique constraints — code and audience — so a
	// record can collide on either one alone. Both are claimed, and either collision skips.
	appCodeKeys, err := newKeyIndex(ctx, s.appRegistry, func(a *sharedentities.AppRegistry) string {
		return normKey(a.Code)
	})
	if err != nil {
		return err
	}
	appAudKeys, err := newKeyIndex(ctx, s.appRegistry, func(a *sharedentities.AppRegistry) string {
		return normKey(a.Audience)
	})
	if err != nil {
		return err
	}
	for _, app := range file.AppRegistry {
		oldID := app.Id
		row := app
		row.Id = 0
		if !appCodeKeys.claim(normKey(row.Code)) || !appAudKeys.claim(normKey(row.Audience)) {
			// This relying app is already registered here. Its client secret and redirect
			// allow-list are live configuration that other running apps depend on; a backup
			// must not silently redefine them, and the ids below never map, so its auth
			// config and redirect URIs skip with it rather than attaching to the app that
			// is already registered.
			res.Skipped[BackupSectionApps]++
			continue
		}
		newID, err := s.appRegistry.Create(ctx, "", row)
		if err != nil {
			return restoreFailure(BackupSectionApps, res, err)
		}
		appIDs[oldID] = int64(newID)
		res.Restored[BackupSectionApps]++
	}

	authKeys, err := newKeyIndex(ctx, s.appAuth, func(a *sharedentities.AppAuthConfig) string {
		return normKey(fmt.Sprint(a.AppRegistryId), a.ClientId)
	})
	if err != nil {
		return err
	}
	uriKeys, err := newKeyIndex(ctx, s.appRedirect, func(u *sharedentities.AppRedirectUri) string {
		return normKey(fmt.Sprint(u.AppAuthConfigId), u.RedirectUri)
	})
	if err != nil {
		return err
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
		if !authKeys.claim(normKey(fmt.Sprint(row.AppRegistryId), row.ClientId)) {
			res.Skipped[BackupSectionApps]++
			continue
		}
		newID, err := s.appAuth.Create(ctx, "", row)
		if err != nil {
			return restoreFailure(BackupSectionApps, res, err)
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
		if !uriKeys.claim(normKey(fmt.Sprint(row.AppAuthConfigId), row.RedirectUri)) {
			res.Skipped[BackupSectionApps]++
			continue
		}
		if _, err := s.appRedirect.Create(ctx, "", row); err != nil {
			return restoreFailure(BackupSectionApps, res, err)
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

	dirKeys, err := newKeyIndex(ctx, s.directory, func(d *myidsanentities.DirectoryConfig) string {
		return normKey(d.Name)
	})
	if err != nil {
		return err
	}
	mapKeys, err := newKeyIndex(ctx, s.groupMaps, func(m *myidsanentities.FederatedGroupMapping) string {
		return normKey(m.Provider, m.GroupName)
	})
	if err != nil {
		return err
	}
	for _, bd := range file.Directory {
		row := bd.DirectoryConfig
		row.Id = 0
		sealed, err := encodeSecret(s.cipher, bd.BindPassword)
		if err != nil {
			return err
		}
		row.BindPassword = sealed
		if !dirKeys.claim(normKey(row.Name)) {
			res.Skipped[BackupSectionFederation]++
			continue
		}
		if _, err := s.directory.Create(ctx, "", row); err != nil {
			return restoreFailure(BackupSectionFederation, res, err)
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
		// (provider, group) is the natural key — matching is case-insensitive on the group
		// name, so two mappings differing only in case would both match one login and the
		// tie-break would decide someone's role by row id.
		if !mapKeys.claim(normKey(row.Provider, row.GroupName)) {
			res.Skipped[BackupSectionFederation]++
			continue
		}
		if _, err := s.groupMaps.Create(ctx, "", row); err != nil {
			return restoreFailure(BackupSectionFederation, res, err)
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
	caKeys, err := newKeyIndex(ctx, s.ssoCa, func(c *myidsanentities.SsoCa) string {
		return normKey(c.Name)
	})
	if err != nil {
		return err
	}
	for _, ca := range file.SsoCa {
		row := ca
		row.Id = 0
		if !caKeys.claim(normKey(row.Name)) {
			// The target already has a CA under this name. Replacing it would invalidate
			// every node certificate this server has already issued, which is a far more
			// destructive outcome than declining to import a second one.
			res.Skipped[BackupSectionSsoCa]++
			continue
		}
		if _, err := s.ssoCa.Create(ctx, "", row); err != nil {
			return restoreFailure(BackupSectionSsoCa, res, err)
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

// --- collision guards ------------------------------------------------------
//
// MERGE mode adds a backup's records alongside whatever the target already holds. Every
// table below carries a UNIQUE constraint on a natural key — a role name, an account
// email, an app code — and every myidsan install seeds the same stock role names and the
// same bootstrap admin. So a merge restore of any backup containing the access or identity
// section into any live server used to hit that constraint on its very first row and abort,
// surfacing the driver's own text ("UNIQUE constraint failed: access_role.name (2067)" on
// sqlite, something else entirely on postgres) to an operator mid-recovery.
//
// Two things were wrong with that. The mode the UI offers as "Keep both" could not work
// against a real target at all. And because the restore walks tables row by row with no
// enclosing transaction, a collision partway through leaves everything before it written —
// a half-restored identity store, reported as a constraint error that says nothing about
// what landed.
//
// keyIndex fixes the first by letting each section ask "is this record already here?"
// BEFORE inserting. Deciding in Go rather than by parsing a driver error keeps the answer
// portable, and lets the skip be COUNTED and reported like every other skip.

// matchExistingRoles maps the backup's role ids onto roles ALREADY on this host, by name,
// for a restore that did not select the access section.
func (s *backupService) matchExistingRoles(ctx context.Context, file *backupFile, roleIDs map[int64]int64) error {
	if len(file.Roles) == 0 {
		return nil
	}
	existing, err := allRows(ctx, s.roles)
	if err != nil {
		return err
	}
	byName := map[string]int64{}
	for _, r := range existing {
		byName[normKey(r.Name)] = r.Id
	}
	for _, r := range file.Roles {
		if id, ok := byName[normKey(r.Name)]; ok {
			roleIDs[r.Id] = id
		}
	}
	return nil
}

// matchExistingUsers is the same for accounts, keyed on email — the column that is unique
// and the identifier people actually sign in with.
func (s *backupService) matchExistingUsers(ctx context.Context, file *backupFile, userIDs map[int64]int64) error {
	if len(file.Users) == 0 {
		return nil
	}
	existing, err := allRows(ctx, s.users)
	if err != nil {
		return err
	}
	byEmail := map[string]int64{}
	for _, u := range existing {
		byEmail[normKey(u.Email)] = u.Id
	}
	for _, u := range file.Users {
		if id, ok := byEmail[normKey(u.Email)]; ok {
			userIDs[u.UserLogin.Id] = id
		}
	}
	return nil
}

// keyIndex is the set of natural keys already present in a table.
type keyIndex map[string]bool

// newKeyIndex reads a table once and indexes its rows by a natural-key function. Built
// AFTER any replace-mode wipe, so in replace mode it starts empty and nothing is skipped.
func newKeyIndex[T any](ctx context.Context, repo dbsql.IGenericRepo[T], key func(*T) string) (keyIndex, error) {
	rows, _, err := repo.Get(ctx, "", backupPageLimit, 0, nil, nil)
	if err != nil {
		if isNotFoundErr(err) {
			return keyIndex{}, nil
		}
		return nil, err
	}
	idx := keyIndex{}
	for _, row := range rows {
		if row != nil {
			idx[key(row)] = true
		}
	}
	return idx, nil
}

// claim reports whether k is new, and records it. Rows are claimed as they are inserted, so
// the guard also catches a backup that carries two records with the same key — which would
// otherwise fail the same way even in replace mode, against an empty table.
func (k keyIndex) claim(key string) bool {
	if k[key] {
		return false
	}
	k[key] = true
	return true
}

// normKey lower-cases and trims the parts of a natural key. Case folding matters because
// the constraints these guards stand in for are on identifiers people type — an account
// email, a role name — and "Admin" colliding with "admin" is a collision the operator
// means, whatever the database's own collation happens to do with it.
func normKey(parts ...string) string {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		out = append(out, strings.ToLower(strings.TrimSpace(p)))
	}
	return strings.Join(out, "\x00")
}

// restoreFailure wraps a mid-restore error with what had already been applied.
//
// The restore is not atomic — the repo layer exposes no transaction that spans these
// tables — so an operator whose recovery stops halfway needs to know that it DID stop
// halfway, and where. The bare driver message told them neither, which on a disaster
// recovery is the difference between "re-run it" and "this server is now in a state
// nobody has described".
func restoreFailure(section string, res *RestoreResult, err error) error {
	applied := make([]string, 0, len(res.Restored))
	for _, s := range backupAllSections {
		if n := res.Restored[s]; n > 0 {
			applied = append(applied, fmt.Sprintf("%s=%d", s, n))
		}
	}
	summary := "nothing"
	if len(applied) > 0 {
		summary = strings.Join(applied, ", ")
	}
	return fmt.Errorf(
		"restore stopped while applying the %q section; this server is now PARTIALLY restored "+
			"(already applied: %s). Re-run the restore from the same file in \"replace\" mode to "+
			"reach a known state. Underlying error: %w", section, summary, err)
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
