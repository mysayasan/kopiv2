package services

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	sharedentities "github.com/mysayasan/kopiv2/domain/entities"
	sharedservices "github.com/mysayasan/kopiv2/domain/shared/services"
	"github.com/mysayasan/kopiv2/apps/myseliasan/entities"
	"github.com/mysayasan/kopiv2/infra/atrest"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// The control plane holds the only copy of state that CANNOT be rebuilt from anywhere
// else in the fleet: the fleet CA private key. Every adopted node trusts certificates
// signed by that key and authenticates its heartbeat with a per-node token stored here.
// Lose the database and the fleet is not degraded, it is orphaned — every node must be
// physically re-adopted with a fresh claim code.
//
// That failure is already deliberately LOUD rather than silent: boot refuses to continue
// when the at-rest key protecting the CA is missing, instead of quietly minting a new CA
// and resetting fleet trust underneath a running fleet (see app.go). This file is the
// other half of that decision — the loud failure now has an answer.
//
// The format and the section machinery deliberately mirror myidsan's .idbackup, down to
// the magic-plus-version envelope and the id-remapping restore, because a second backup
// format is a second thing to get subtly wrong about sealed columns.

const (
	// Logical, user-selectable sections. Order matters: it is also the order they are
	// collected and applied in, and later sections depend on ids remapped by earlier ones.

	// BackupSectionAccess is the RBAC core: roles and the per-endpoint permission matrix.
	BackupSectionAccess = "access"
	// BackupSectionUsers is control_user — local operators (password hashes included) and
	// the SSO-federated user rows. Depends on access: ControlUser.RoleId points at a role.
	BackupSectionUsers = "users"
	// BackupSectionFleetCA is the fleet trust root: the CA certificate and PRIVATE KEY, the
	// control plane's own leaf, the revocation list, and the fleet PSK. This is the section
	// that makes the difference between a restore and a re-adoption of every node, and it
	// is the reason the whole file exists.
	BackupSectionFleetCA = "fleetca"
	// BackupSectionFleet is the node registry and the per-role node access grants. Node
	// rows carry the heartbeat token, which is json:"-" on the entity — see backupNode.
	BackupSectionFleet = "fleet"
	// BackupSectionSites is the physical model: sites, their floors (INCLUDING the plan
	// images, which live encrypted on disk rather than in the database) and the node/camera
	// placements pinned onto those floors.
	BackupSectionSites = "sites"
	// BackupSectionRules is the fleet correlation rules.
	BackupSectionRules = "rules"
	// BackupSectionSettings is every remaining control_setting row — deployment mode, the
	// agent schedule, the first-run defaults snapshot. It deliberately EXCLUDES the
	// pairing.* keys, which belong to fleetca and must not be restorable without it.
	BackupSectionSettings = "settings"
	// BackupSectionAudit is the audit trail. It is append-only by design, so unlike every
	// other section it is never wiped — see restoreAudit.
	BackupSectionAudit = "audit"

	backupApp           = "myseliasan"
	backupSchemaVersion = 1
	backupFormatVersion = 1
	backupPageLimit     = 100000

	// RestoreModeReplace wipes the target tables of each selected section before
	// inserting; RestoreModeMerge appends without clearing.
	RestoreModeReplace = "replace"
	RestoreModeMerge   = "merge"
)

// backupMagic prefixes every .selbackup file so a non-backup upload — or a .idbackup
// from the identity server — is rejected before any passphrase work happens.
var backupMagic = []byte("SELB")

var backupAllSections = []string{
	BackupSectionAccess,
	BackupSectionUsers,
	BackupSectionFleetCA,
	BackupSectionFleet,
	BackupSectionSites,
	BackupSectionRules,
	BackupSectionSettings,
	BackupSectionAudit,
}

// fleetCASettingKeys are the control_setting rows owned by the fleetca section. Every
// other row in that table belongs to the settings section. The split matters in replace
// mode: "replace settings" must not silently drop the CA private key along with it.
var fleetCASettingKeys = []string{
	caCertKey,          // pairing.caCert
	caKeyKey,           // pairing.caKey       (sealed)
	parentCertKey,      // pairing.parentCert
	parentKeyKey,       // pairing.parentKey   (sealed)
	revokedKey,         // pairing.revoked
	fleetKeySettingKey, // pairing.fleetKey    (sealed)
}

// sealedSettingKeys are the control_setting rows whose Value is at-rest encrypted (see
// secret_store.go). They are UNSEALED on export and RE-SEALED on restore, which is what
// lets a backup move to a host with a different at-rest key.
//
// This is an explicit list rather than "seal everything": rows outside it are read with
// a plain repo call that never calls decodeSecret, so sealing one would hand its reader
// base64 ciphertext and break the feature it configures.
var sealedSettingKeys = []string{
	caKeyKey,
	parentKeyKey,
	fleetKeySettingKey,
	settingsDefaultsKey,
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

// backupNode re-exposes ManagedNode.Token, which is json:"-" on the entity so it never
// reaches a browser. It is not optional here: the heartbeat handler authenticates a node
// by comparing the presented token against this column, so a node restored without its
// token is adopted-but-unauthenticated and drops out of the fleet on its next beat.
type backupNode struct {
	entities.ManagedNode
	Token string `json:"token"`
}

// backupUser re-exposes ControlUser.PasswordHash, which is json:"-" on the entity so a
// hash never reaches a browser.
//
// Without this the field is silently dropped by the JSON encoding of the bundle, and a
// restore produces a control plane that NOBODY CAN SIGN IN TO: every local account comes
// back with an empty password. That is the worst possible outcome for a disaster-recovery
// feature — the operator restores after losing their server and is locked out of the
// result. Found by benching a real restore; the unit tests could not see it because an
// in-memory repo round-trips the struct without ever marshalling it.
//
// Exactly the same trap as ManagedNode.Token below. Any json:"-" field on a backed-up
// entity needs a wrapper like this one.
type backupUser struct {
	entities.ControlUser
	PasswordHash string `json:"passwordHash"`
}

// backupFloor carries a floor's plan images alongside its row. Both live on disk, not in
// the database, and both are ENCRYPTED there with the host's at-rest key — so like the
// sealed settings they are decrypted on export and re-encrypted on restore. Image holds
// the rendered plan (background plus drawn shapes); Bg holds the pristine uploaded
// background, empty for a plan drawn on a blank canvas.
//
// Plan images are the bulk of a typical .selbackup. They are included rather than
// referenced because they are operator-authored and not reproducible from anything else.
type backupFloor struct {
	entities.FloorPlan
	ImageB64 string `json:"imageB64,omitempty"`
	BgB64    string `json:"bgB64,omitempty"`
}

// backupSetting is a control_setting row whose Value has been UNSEALED for transport
// (for the keys in sealedSettingKeys). Sealed records which side of that line it was on,
// so restore re-seals exactly the rows that were sealed and leaves the rest alone.
type backupSetting struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	Sealed bool   `json:"sealed"`
}

type backupFile struct {
	Manifest    BackupManifest                       `json:"manifest"`
	Roles       []sharedentities.AccessRole          `json:"roles,omitempty"`
	Permissions []sharedentities.AccessRolePermission `json:"permissions,omitempty"`
	Users       []backupUser                         `json:"users,omitempty"`
	FleetCA     []backupSetting                      `json:"fleetCa,omitempty"`
	Nodes       []backupNode                         `json:"nodes,omitempty"`
	Grants      []entities.NodeAccessGrant           `json:"grants,omitempty"`
	Sites       []entities.Site                      `json:"sites,omitempty"`
	Floors      []backupFloor                        `json:"floors,omitempty"`
	Placements  []entities.NodePlacement             `json:"placements,omitempty"`
	Rules       []entities.FleetRule                 `json:"rules,omitempty"`
	Settings    []backupSetting                      `json:"settings,omitempty"`
	Audit       []entities.AuditLog                  `json:"audit,omitempty"`
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

// RestoreResult reports what was applied.
//
// RestartRequired is the field the UI must act on. The fleet CA is cached in memory by
// the running fleetCA service and only reloaded on construction, so restoring the
// fleetca section leaves this process still serving mTLS from the OLD CA while the
// database holds the new one — a split that would reject every node until the next
// restart. Restoring the CA therefore ends in a restart, not a hot swap.
type RestoreResult struct {
	Sections        []string       `json:"sections"`
	Restored        map[string]int `json:"restored"`
	Skipped         map[string]int `json:"skipped"`
	AppVersion      string         `json:"appVersion"`
	SchemaWarning   string         `json:"schemaWarning,omitempty"`
	RestartRequired bool           `json:"restartRequired"`
	RestartReason   string         `json:"restartReason,omitempty"`
	SetupCompleted  bool           `json:"setupCompleted"`
	SetupError      string         `json:"setupError,omitempty"`
}

// IBackupService exports and restores the passphrase-encrypted control-plane bundle.
type IBackupService interface {
	AvailableSections(ctx context.Context) ([]BackupSectionInfo, error)
	Export(ctx context.Context, req BackupRequest) ([]byte, error)
	Preview(ctx context.Context, data []byte, passphrase string) (BackupManifest, error)
	Restore(ctx context.Context, data []byte, req RestoreRequest) (RestoreResult, error)
}

type backupService struct {
	roles       dbsql.IGenericRepo[sharedentities.AccessRole]
	permissions dbsql.IGenericRepo[sharedentities.AccessRolePermission]
	users       dbsql.IGenericRepo[entities.ControlUser]
	settings    dbsql.IGenericRepo[entities.ControlSetting]
	nodes       dbsql.IGenericRepo[entities.ManagedNode]
	grants      dbsql.IGenericRepo[entities.NodeAccessGrant]
	sites       dbsql.IGenericRepo[entities.Site]
	floors      dbsql.IGenericRepo[entities.FloorPlan]
	placements  dbsql.IGenericRepo[entities.NodePlacement]
	rules       dbsql.IGenericRepo[entities.FleetRule]
	audit       dbsql.IGenericRepo[entities.AuditLog]

	// cipher seals/unseals the at-rest secrets — the CA private key, the fleet PSK, and
	// the on-disk floor plan images. Nil when encryption is disabled, in which case the
	// stored values are already plaintext and pass through untouched.
	cipher *atrest.Cipher
	// planDir is where encrypted floor plan images live on disk.
	planDir    string
	setup      sharedservices.ISetupStateService
	appVersion string
}

// NewBackupService builds the control-plane backup service. cipher and planDir must be
// the SAME ones the site service and fleet CA use, or exported images and secrets will
// not decrypt.
func NewBackupService(
	db dbsql.IDbCrud,
	cipher *atrest.Cipher,
	planDir string,
	setup sharedservices.ISetupStateService,
	appVersion string,
) IBackupService {
	return &backupService{
		roles:       dbsql.NewGenericRepo[sharedentities.AccessRole](db),
		permissions: dbsql.NewGenericRepo[sharedentities.AccessRolePermission](db),
		users:       dbsql.NewGenericRepo[entities.ControlUser](db),
		settings:    dbsql.NewGenericRepo[entities.ControlSetting](db),
		nodes:       dbsql.NewGenericRepo[entities.ManagedNode](db),
		grants:      dbsql.NewGenericRepo[entities.NodeAccessGrant](db),
		sites:       dbsql.NewGenericRepo[entities.Site](db),
		floors:      dbsql.NewGenericRepo[entities.FloorPlan](db),
		placements:  dbsql.NewGenericRepo[entities.NodePlacement](db),
		rules:       dbsql.NewGenericRepo[entities.FleetRule](db),
		audit:       dbsql.NewGenericRepo[entities.AuditLog](db),
		cipher:      cipher,
		planDir:     planDir,
		setup:       setup,
		appVersion:  appVersion,
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
	case BackupSectionUsers:
		return countRows(ctx, s.users)
	case BackupSectionFleetCA:
		rows, err := s.settingRows(ctx, true)
		return len(rows), err
	case BackupSectionFleet:
		return countRows(ctx, s.nodes)
	case BackupSectionSites:
		return countRows(ctx, s.sites)
	case BackupSectionRules:
		return countRows(ctx, s.rules)
	case BackupSectionSettings:
		rows, err := s.settingRows(ctx, false)
		return len(rows), err
	case BackupSectionAudit:
		return countRows(ctx, s.audit)
	}
	return 0, nil
}

// settingRows returns the control_setting rows on one side of the fleetca/settings split:
// fleetCA=true yields only the pairing.* keys, false yields everything else.
func (s *backupService) settingRows(ctx context.Context, fleetCA bool) ([]entities.ControlSetting, error) {
	all, err := allRows(ctx, s.settings)
	if err != nil {
		return nil, err
	}
	out := make([]entities.ControlSetting, 0, len(all))
	for _, row := range all {
		if containsString(fleetCASettingKeys, row.Key) == fleetCA {
			out = append(out, row)
		}
	}
	return out, nil
}

// --- export ----------------------------------------------------------------

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

	case BackupSectionUsers:
		users, err := allRows(ctx, s.users)
		if err != nil {
			return err
		}
		for _, u := range users {
			file.Users = append(file.Users, backupUser{ControlUser: u, PasswordHash: u.PasswordHash})
		}
		file.Manifest.Counts[section] = len(users)

	case BackupSectionFleetCA:
		rows, err := s.settingRows(ctx, true)
		if err != nil {
			return err
		}
		file.FleetCA = s.exportSettings(rows)
		file.Manifest.Counts[section] = len(file.FleetCA)

	case BackupSectionFleet:
		nodes, err := allRows(ctx, s.nodes)
		if err != nil {
			return err
		}
		for _, n := range nodes {
			file.Nodes = append(file.Nodes, backupNode{ManagedNode: n, Token: n.Token})
		}
		grants, err := allRows(ctx, s.grants)
		if err != nil {
			return err
		}
		file.Grants = grants
		file.Manifest.Counts[section] = len(nodes)

	case BackupSectionSites:
		sites, err := allRows(ctx, s.sites)
		if err != nil {
			return err
		}
		file.Sites = sites
		floors, err := allRows(ctx, s.floors)
		if err != nil {
			return err
		}
		for _, f := range floors {
			file.Floors = append(file.Floors, backupFloor{
				FloorPlan: f,
				ImageB64:  s.readPlanImage(f.ImagePath),
				BgB64:     s.readPlanImage(f.BgPath),
			})
		}
		placements, err := allRows(ctx, s.placements)
		if err != nil {
			return err
		}
		file.Placements = placements
		file.Manifest.Counts[section] = len(sites)

	case BackupSectionRules:
		rules, err := allRows(ctx, s.rules)
		if err != nil {
			return err
		}
		file.Rules = rules
		file.Manifest.Counts[section] = len(rules)

	case BackupSectionSettings:
		rows, err := s.settingRows(ctx, false)
		if err != nil {
			return err
		}
		file.Settings = s.exportSettings(rows)
		file.Manifest.Counts[section] = len(file.Settings)

	case BackupSectionAudit:
		entries, err := allRows(ctx, s.audit)
		if err != nil {
			return err
		}
		file.Audit = entries
		file.Manifest.Counts[section] = len(entries)
	}
	return nil
}

// exportSettings unseals the at-rest encrypted rows so the payload carries plaintext
// inside the passphrase-encrypted envelope, and records which rows were sealed so the
// restore can put exactly those back under the DESTINATION host's key.
func (s *backupService) exportSettings(rows []entities.ControlSetting) []backupSetting {
	out := make([]backupSetting, 0, len(rows))
	for _, row := range rows {
		sealed := containsString(sealedSettingKeys, row.Key)
		value := row.Value
		if sealed {
			value = decodeSecret(s.cipher, row.Value)
		}
		out = append(out, backupSetting{Key: row.Key, Value: value, Sealed: sealed})
	}
	return out
}

// readPlanImage returns the base64 of a floor image's PLAINTEXT bytes, or "" when the
// path is empty or unreadable. A missing image is not fatal: it costs a picture, and
// failing the whole backup over one would be a worse trade when the CA key is in the
// same file.
func (s *backupService) readPlanImage(path string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	data := raw
	if s.cipher != nil {
		dec, decErr := s.cipher.DecryptBytes(raw)
		if decErr != nil {
			return ""
		}
		data = dec
	}
	return base64.StdEncoding.EncodeToString(data)
}

// --- preview / decode ------------------------------------------------------

func (s *backupService) Preview(ctx context.Context, data []byte, passphrase string) (BackupManifest, error) {
	file, err := s.decode(data, passphrase)
	if err != nil {
		return BackupManifest{}, err
	}
	return file.Manifest, nil
}

func (s *backupService) decode(data []byte, passphrase string) (*backupFile, error) {
	if len(data) < len(backupMagic)+1 || !bytes.Equal(data[:len(backupMagic)], backupMagic) {
		return nil, errors.New("this is not a myseliasan backup file")
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
		return nil, errors.New("this backup was not created by myseliasan")
	}
	return &file, nil
}

// --- restore ---------------------------------------------------------------

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
	//   roles -> users  (ControlUser.RoleId points at a role)
	//         -> grants (NodeAccessGrant.RoleId)
	//   sites -> floors (FloorPlan.SiteId) -> placements (NodePlacement.FloorId)
	// Node ids are the node's own stable string id, so nothing about the fleet needs
	// remapping — which is exactly why a restored node still authenticates.
	roleIDs := map[int64]int64{}
	floorIDs := map[int64]int64{}
	siteIDs := map[int64]int64{}

	if containsString(sections, BackupSectionAccess) {
		if err := s.restoreAccess(ctx, file, mode, roleIDs, &res); err != nil {
			return res, err
		}
	}
	if containsString(sections, BackupSectionUsers) {
		if err := s.restoreUsers(ctx, file, mode, roleIDs, &res); err != nil {
			return res, err
		}
	}
	if containsString(sections, BackupSectionFleetCA) {
		if err := s.restoreSettingRows(ctx, file.FleetCA, mode, BackupSectionFleetCA, fleetCASettingKeys, &res); err != nil {
			return res, err
		}
		res.RestartRequired = true
		res.RestartReason = "the fleet certificate authority was restored; the control plane must restart before nodes can reconnect"
	}
	if containsString(sections, BackupSectionFleet) {
		if err := s.restoreFleet(ctx, file, mode, roleIDs, &res); err != nil {
			return res, err
		}
	}
	if containsString(sections, BackupSectionSites) {
		if err := s.restoreSites(ctx, file, mode, siteIDs, floorIDs, &res); err != nil {
			return res, err
		}
	}
	if containsString(sections, BackupSectionRules) {
		if err := s.restoreRules(ctx, file, mode, &res); err != nil {
			return res, err
		}
	}
	if containsString(sections, BackupSectionSettings) {
		if err := s.restoreSettingRows(ctx, file.Settings, mode, BackupSectionSettings, nil, &res); err != nil {
			return res, err
		}
	}
	if containsString(sections, BackupSectionAudit) {
		if err := s.restoreAudit(ctx, file, &res); err != nil {
			return res, err
		}
	}

	// A restored instance is already configured, so the first-run wizard must not
	// reappear. Best-effort — the restore itself has already succeeded by this point.
	if s.setup != nil {
		if _, err := s.setup.Complete(ctx); err != nil {
			res.SetupError = err.Error()
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

func (s *backupService) restoreUsers(ctx context.Context, file *backupFile, mode string, roleIDs map[int64]int64, res *RestoreResult) error {
	if mode == RestoreModeReplace {
		if err := wipeAll(ctx, s.users, func(u *entities.ControlUser) int64 { return u.Id }); err != nil {
			return err
		}
	}
	for _, user := range file.Users {
		row := user.ControlUser
		row.Id = 0
		// The hash arrived in the wrapper, not the embed — see backupUser. Without this
		// line every restored account has an empty password and cannot sign in.
		row.PasswordHash = user.PasswordHash
		// A user whose role did not come across keeps NO role rather than inheriting
		// whatever id happens to sit in that slot on this host. Pending-clearance is the
		// safe landing spot; silently mapping onto a stranger's role is not.
		if mapped, ok := roleIDs[row.RoleId]; ok {
			row.RoleId = mapped
		} else if row.RoleId != 0 {
			row.RoleId = 0
			res.Skipped[BackupSectionUsers]++
		}
		if _, err := s.users.Create(ctx, "", row); err != nil {
			return err
		}
		res.Restored[BackupSectionUsers]++
	}
	return nil
}

// restoreSettingRows applies one side of the control_setting split. keys bounds what
// replace mode is allowed to delete: restoring the fleetca section must not clear the
// deployment mode, and restoring settings must not clear the CA private key. A nil keys
// means "everything that is NOT a fleetca key".
func (s *backupService) restoreSettingRows(ctx context.Context, rows []backupSetting, mode, section string, keys []string, res *RestoreResult) error {
	if mode == RestoreModeReplace {
		existing, err := allRows(ctx, s.settings)
		if err != nil {
			return err
		}
		for _, row := range existing {
			inSection := containsString(keys, row.Key)
			if keys == nil {
				inSection = !containsString(fleetCASettingKeys, row.Key)
			}
			if !inSection {
				continue
			}
			if _, err := s.settings.DeleteById(ctx, "", uint64(row.Id)); err != nil {
				return err
			}
		}
	}
	now := time.Now().Unix()
	for _, row := range rows {
		value := row.Value
		if row.Sealed {
			// Re-seal under THIS host's key. This is the whole point of the unseal on
			// export: the backup is portable across at-rest keys precisely because the
			// secret travels as plaintext inside the passphrase-encrypted envelope and is
			// re-sealed on arrival.
			sealed, err := encodeSecret(s.cipher, row.Value)
			if err != nil {
				return err
			}
			value = sealed
		}
		if err := s.upsertSettingRow(ctx, row.Key, value, now); err != nil {
			return err
		}
		res.Restored[section]++
	}
	return nil
}

// upsertSettingRow writes one control_setting by key, updating in place when it already
// exists. Merge mode relies on this: the key column is unique, so a blind Create on an
// existing key fails rather than replacing it.
func (s *backupService) upsertSettingRow(ctx context.Context, key, value string, now int64) error {
	existing, err := s.settings.GetByUnique(ctx, "", "key", key)
	if err != nil && !isNoResultFoundErr(err) {
		return err
	}
	if err == nil && existing != nil {
		existing.Value = value
		existing.UpdatedAt = now
		_, uerr := s.settings.UpdateById(ctx, "", *existing)
		return uerr
	}
	_, cerr := s.settings.Create(ctx, "", entities.ControlSetting{
		Key: key, Value: value, CreatedAt: now, UpdatedAt: now,
	})
	return cerr
}

func (s *backupService) restoreFleet(ctx context.Context, file *backupFile, mode string, roleIDs map[int64]int64, res *RestoreResult) error {
	if mode == RestoreModeReplace {
		if err := wipeAll(ctx, s.grants, func(g *entities.NodeAccessGrant) int64 { return g.Id }); err != nil {
			return err
		}
		if err := wipeAll(ctx, s.nodes, func(n *entities.ManagedNode) int64 { return n.Id }); err != nil {
			return err
		}
	}
	for _, node := range file.Nodes {
		row := node.ManagedNode
		row.Id = 0
		// Token is json:"-" on the entity, so it arrived in the wrapper, not the embed.
		// Without it the node is adopted-but-unauthenticated on its next heartbeat.
		row.Token = node.Token
		if _, err := s.nodes.Create(ctx, "", row); err != nil {
			return err
		}
		res.Restored[BackupSectionFleet]++
	}
	for _, grant := range file.Grants {
		newRoleID, ok := roleIDs[grant.RoleId]
		if !ok {
			// Same rule as an orphaned permission: no grant beats a grant pointing at
			// whatever role now holds that id.
			res.Skipped[BackupSectionFleet]++
			continue
		}
		row := grant
		row.Id = 0
		row.RoleId = newRoleID
		if _, err := s.grants.Create(ctx, "", row); err != nil {
			return err
		}
	}
	return nil
}

func (s *backupService) restoreSites(ctx context.Context, file *backupFile, mode string, siteIDs, floorIDs map[int64]int64, res *RestoreResult) error {
	if mode == RestoreModeReplace {
		if err := wipeAll(ctx, s.placements, func(p *entities.NodePlacement) int64 { return p.Id }); err != nil {
			return err
		}
		// Drop the old plan images with their rows, or the directory accumulates
		// orphans that nothing will ever reference or clean up.
		oldFloors, err := allRows(ctx, s.floors)
		if err != nil {
			return err
		}
		for _, f := range oldFloors {
			removePlanImage(f.ImagePath)
			removePlanImage(f.BgPath)
		}
		if err := wipeAll(ctx, s.floors, func(f *entities.FloorPlan) int64 { return f.Id }); err != nil {
			return err
		}
		if err := wipeAll(ctx, s.sites, func(st *entities.Site) int64 { return st.Id }); err != nil {
			return err
		}
	}

	for _, site := range file.Sites {
		oldID := site.Id
		row := site
		row.Id = 0
		newID, err := s.sites.Create(ctx, "", row)
		if err != nil {
			return err
		}
		siteIDs[oldID] = int64(newID)
		res.Restored[BackupSectionSites]++
	}

	for _, floor := range file.Floors {
		newSiteID, ok := siteIDs[floor.SiteId]
		if !ok {
			res.Skipped[BackupSectionSites]++
			continue
		}
		oldID := floor.Id
		row := floor.FloorPlan
		row.Id = 0
		row.SiteId = newSiteID
		// Paths name the row id, which is not known until the insert. Clear them, insert,
		// then write the images and update — the same create-then-name dance the site
		// service does on upload.
		row.ImagePath = ""
		row.BgPath = ""
		newID, err := s.floors.Create(ctx, "", row)
		if err != nil {
			return err
		}
		row.Id = int64(newID)
		floorIDs[oldID] = row.Id

		imgPath, err := s.writePlanImage(fmt.Sprintf("floor-%d.img", row.Id), floor.ImageB64)
		if err != nil {
			return err
		}
		bgPath, err := s.writePlanImage(fmt.Sprintf("floor-%d.bg.img", row.Id), floor.BgB64)
		if err != nil {
			return err
		}
		row.ImagePath = imgPath
		row.BgPath = bgPath
		if _, err := s.floors.UpdateById(ctx, "", row); err != nil {
			return err
		}
	}

	for _, placement := range file.Placements {
		newFloorID, ok := floorIDs[placement.FloorId]
		if !ok {
			res.Skipped[BackupSectionSites]++
			continue
		}
		row := placement
		row.Id = 0
		row.FloorId = newFloorID
		if _, err := s.placements.Create(ctx, "", row); err != nil {
			return err
		}
	}
	return nil
}

// writePlanImage re-encrypts a floor image under THIS host's at-rest key and writes it
// under planDir. An empty payload yields an empty path, which is what a floor with no
// background legitimately has.
func (s *backupService) writePlanImage(name, b64 string) (string, error) {
	if strings.TrimSpace(b64) == "" {
		return "", nil
	}
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return "", fmt.Errorf("plan image %s is corrupt: %w", name, err)
	}
	if err := os.MkdirAll(s.planDir, 0o755); err != nil {
		return "", err
	}
	payload := raw
	if s.cipher != nil {
		enc, encErr := s.cipher.EncryptBytes(raw)
		if encErr != nil {
			return "", encErr
		}
		payload = enc
	}
	path := filepath.Join(s.planDir, name)
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func removePlanImage(path string) {
	if strings.TrimSpace(path) == "" {
		return
	}
	_ = os.Remove(path)
}

func (s *backupService) restoreRules(ctx context.Context, file *backupFile, mode string, res *RestoreResult) error {
	if mode == RestoreModeReplace {
		if err := wipeAll(ctx, s.rules, func(r *entities.FleetRule) int64 { return r.Id }); err != nil {
			return err
		}
	}
	for _, rule := range file.Rules {
		row := rule
		row.Id = 0
		if _, err := s.rules.Create(ctx, "", row); err != nil {
			return err
		}
		res.Restored[BackupSectionRules]++
	}
	return nil
}

// restoreAudit takes no mode. The audit trail is append-only — the service that writes it
// has no update or delete path precisely so the record cannot be edited after the fact —
// and honouring "replace" here would hand an operator a supported way to erase it by
// restoring an empty backup over it. Entries are appended; duplicates on a repeated
// restore are the lesser evil against a destroyable trail.
func (s *backupService) restoreAudit(ctx context.Context, file *backupFile, res *RestoreResult) error {
	for _, entry := range file.Audit {
		row := entry
		row.Id = 0
		if _, err := s.audit.Create(ctx, "", row); err != nil {
			return err
		}
		res.Restored[BackupSectionAudit]++
	}
	return nil
}

// --- helpers ---------------------------------------------------------------

func allRows[T any](ctx context.Context, repo dbsql.IGenericRepo[T]) ([]T, error) {
	rows, _, err := repo.Get(ctx, "", backupPageLimit, 0, nil, nil)
	if err != nil {
		if isNoResultFoundErr(err) {
			return []T{}, nil
		}
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
		if isNoResultFoundErr(err) {
			return nil
		}
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
