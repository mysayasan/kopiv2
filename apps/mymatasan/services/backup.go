package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	"github.com/mysayasan/kopiv2/infra/atrest"
)

// Backup & Restore produces a portable, passphrase-encrypted snapshot of the app's
// configuration so a fresh install can be brought up without re-entering cameras,
// AI rules, notification destinations, or settings. It deliberately captures the
// json:"-" secrets (camera passwords, notification tokens) that the normal API
// strips, which is safe only because the whole file is encrypted with a user
// passphrase (see atrest.EncryptWithPassphrase — portable, not machine-bound).
//
// Machine identity (the at-rest key, node pairing, certificates, config.json) is
// intentionally never included: cloning it onto a second host would duplicate
// credentials. A restore lets those regenerate.
const (
	// Logical, user-selectable sections.
	BackupSectionCameras       = "cameras"       // camera + camera_onvif (creds) + recording_config
	BackupSectionAI            = "ai"            // detection_class registry + detection_rule
	BackupSectionNotifications = "notifications" // runtime_setting["notification"] (destinations + secrets)
	BackupSectionSettings      = "settings"      // runtime_setting: decoder/vision/capture + camera & machine health

	// schemaVersion versions the payload shape; formatVersion versions the on-disk
	// magic framing. Bump when the structure changes incompatibly.
	backupApp           = "mymatasan"
	backupSchemaVersion = 1
	backupFormatVersion = 1
	backupPageLimit     = 100000

	// RestoreModeReplace wipes the target tables of each selected section before
	// inserting; RestoreModeMerge appends (new IDs) without clearing.
	RestoreModeReplace = "replace"
	RestoreModeMerge   = "merge"
)

// backupMagic prefixes every .mmbackup file so a non-backup upload is rejected
// before any passphrase work.
var backupMagic = []byte("MMBK")

// backupAllSections is the canonical order sections are collected and applied in.
var backupAllSections = []string{BackupSectionCameras, BackupSectionAI, BackupSectionNotifications, BackupSectionSettings}

// BackupManifest is the human-readable header carried inside the encrypted payload;
// Preview surfaces it so the UI/wizard can show what a file contains before applying.
type BackupManifest struct {
	App           string         `json:"app"`
	AppVersion    string         `json:"appVersion"`
	SchemaVersion int            `json:"schemaVersion"`
	CreatedAt     int64          `json:"createdAt"`
	Sections      []string       `json:"sections"`
	Counts        map[string]int `json:"counts"`
}

// backupCamera re-exposes Camera.TalkPassword, which is json:"-" on the entity (the
// normal API never emits it). The outer field shadows the embedded one during
// (un)marshal so the secret is captured in — and only in — the encrypted backup.
type backupCamera struct {
	entities.Camera
	TalkPassword string `json:"talkPassword"`
}

// backupCameraOnvif likewise re-exposes the json:"-" ONVIF Password.
type backupCameraOnvif struct {
	entities.CameraOnvif
	Password string `json:"password"`
}

// backupFile is the decrypted payload. Settings holds runtime_setting rows verbatim
// (key -> Value); their embedded secrets are already inside the JSON Value.
type backupFile struct {
	Manifest         BackupManifest             `json:"manifest"`
	Cameras          []backupCamera             `json:"cameras,omitempty"`
	CameraOnvif      []backupCameraOnvif        `json:"cameraOnvif,omitempty"`
	RecordingConfigs []entities.RecordingConfig `json:"recordingConfigs,omitempty"`
	DetectionClasses []entities.DetectionClass  `json:"detectionClasses,omitempty"`
	DetectionRules   []entities.DetectionRule   `json:"detectionRules,omitempty"`
	Settings         map[string]string          `json:"settings,omitempty"`
}

// BackupRequest selects what to export and the passphrase to protect it with.
type BackupRequest struct {
	Sections   []string `json:"sections"`
	Passphrase string   `json:"passphrase"`
}

// RestoreRequest selects which of the present sections to apply. An empty Sections
// applies every section found in the file.
type RestoreRequest struct {
	Sections   []string `json:"sections"`
	Passphrase string   `json:"passphrase"`
	Mode       string   `json:"mode"`
}

// BackupSectionInfo reports how many rows a section currently holds, so the export
// UI can show counts and disable empty sections.
type BackupSectionInfo struct {
	Id    string `json:"id"`
	Count int    `json:"count"`
}

// RestoreResult reports, per section, how many rows were applied and how many were
// skipped (e.g. AI rules whose camera was not part of the restore).
type RestoreResult struct {
	Sections      []string       `json:"sections"`
	Restored      map[string]int `json:"restored"`
	Skipped       map[string]int `json:"skipped"`
	AppVersion    string         `json:"appVersion"`
	SchemaWarning string         `json:"schemaWarning,omitempty"`
}

// IBackupService exports and restores the passphrase-encrypted configuration bundle.
type IBackupService interface {
	AvailableSections(ctx context.Context) ([]BackupSectionInfo, error)
	Export(ctx context.Context, req BackupRequest) ([]byte, error)
	Preview(ctx context.Context, data []byte, passphrase string) (BackupManifest, error)
	Restore(ctx context.Context, data []byte, req RestoreRequest) (RestoreResult, error)
}

type backupService struct {
	cameras          dbsql.IGenericRepo[entities.Camera]
	cameraOnvif      dbsql.IGenericRepo[entities.CameraOnvif]
	recordingConfigs dbsql.IGenericRepo[entities.RecordingConfig]
	detectionClasses dbsql.IGenericRepo[entities.DetectionClass]
	detectionRules   dbsql.IGenericRepo[entities.DetectionRule]
	settings         dbsql.IGenericRepo[entities.RuntimeSetting]
	appVersion       string
}

// NewBackupService wires the repositories the bundle spans. appVersion is stamped
// into the manifest so a restore can warn on cross-version files.
func NewBackupService(
	cameras dbsql.IGenericRepo[entities.Camera],
	cameraOnvif dbsql.IGenericRepo[entities.CameraOnvif],
	recordingConfigs dbsql.IGenericRepo[entities.RecordingConfig],
	detectionClasses dbsql.IGenericRepo[entities.DetectionClass],
	detectionRules dbsql.IGenericRepo[entities.DetectionRule],
	settings dbsql.IGenericRepo[entities.RuntimeSetting],
	appVersion string,
) IBackupService {
	return &backupService{
		cameras:          cameras,
		cameraOnvif:      cameraOnvif,
		recordingConfigs: recordingConfigs,
		detectionClasses: detectionClasses,
		detectionRules:   detectionRules,
		settings:         settings,
		appVersion:       appVersion,
	}
}

// settingKeys maps a runtime_setting-backed section to the keys it owns.
func settingKeysForSection(section string) []string {
	switch section {
	case BackupSectionNotifications:
		return []string{notificationSettingsKey}
	case BackupSectionSettings:
		return []string{runtimeSettingsKey, healthSettingsKey, machineHealthSettingsKey}
	default:
		return nil
	}
}

func (s *backupService) AvailableSections(ctx context.Context) ([]BackupSectionInfo, error) {
	out := make([]BackupSectionInfo, 0, len(backupAllSections))

	_, camTotal, err := s.cameras.Get(ctx, "", 1, 0, nil, nil)
	if err != nil {
		return nil, err
	}
	out = append(out, BackupSectionInfo{Id: BackupSectionCameras, Count: int(camTotal)})

	_, ruleTotal, err := s.detectionRules.Get(ctx, "", 1, 0, nil, nil)
	if err != nil {
		return nil, err
	}
	out = append(out, BackupSectionInfo{Id: BackupSectionAI, Count: int(ruleTotal)})

	out = append(out, BackupSectionInfo{Id: BackupSectionNotifications, Count: s.countSettingKeys(ctx, BackupSectionNotifications)})
	out = append(out, BackupSectionInfo{Id: BackupSectionSettings, Count: s.countSettingKeys(ctx, BackupSectionSettings)})
	return out, nil
}

func (s *backupService) countSettingKeys(ctx context.Context, section string) int {
	count := 0
	for _, key := range settingKeysForSection(section) {
		row, err := s.settings.GetByUnique(ctx, "", "key", key)
		if err != nil {
			continue
		}
		if row != nil && strings.TrimSpace(row.Value) != "" {
			count++
		}
	}
	return count
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
		switch section {
		case BackupSectionCameras:
			if err := s.collectCameras(ctx, &file); err != nil {
				return nil, err
			}
		case BackupSectionAI:
			if err := s.collectAI(ctx, &file); err != nil {
				return nil, err
			}
		case BackupSectionNotifications, BackupSectionSettings:
			if err := s.collectSettings(ctx, &file, section); err != nil {
				return nil, err
			}
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

func (s *backupService) collectCameras(ctx context.Context, file *backupFile) error {
	cams, _, err := s.cameras.Get(ctx, "", backupPageLimit, 0, nil, nil)
	if err != nil {
		return err
	}
	for _, c := range cams {
		file.Cameras = append(file.Cameras, backupCamera{Camera: *c, TalkPassword: c.TalkPassword})
	}
	onvifs, _, err := s.cameraOnvif.Get(ctx, "", backupPageLimit, 0, nil, nil)
	if err != nil {
		return err
	}
	for _, o := range onvifs {
		file.CameraOnvif = append(file.CameraOnvif, backupCameraOnvif{CameraOnvif: *o, Password: o.Password})
	}
	rcs, _, err := s.recordingConfigs.Get(ctx, "", backupPageLimit, 0, nil, nil)
	if err != nil {
		return err
	}
	for _, rc := range rcs {
		file.RecordingConfigs = append(file.RecordingConfigs, *rc)
	}
	file.Manifest.Counts[BackupSectionCameras] = len(file.Cameras)
	return nil
}

func (s *backupService) collectAI(ctx context.Context, file *backupFile) error {
	classes, _, err := s.detectionClasses.Get(ctx, "", backupPageLimit, 0, nil, nil)
	if err != nil {
		return err
	}
	for _, c := range classes {
		file.DetectionClasses = append(file.DetectionClasses, *c)
	}
	rules, _, err := s.detectionRules.Get(ctx, "", backupPageLimit, 0, nil, nil)
	if err != nil {
		return err
	}
	for _, r := range rules {
		file.DetectionRules = append(file.DetectionRules, *r)
	}
	file.Manifest.Counts[BackupSectionAI] = len(file.DetectionRules)
	return nil
}

func (s *backupService) collectSettings(ctx context.Context, file *backupFile, section string) error {
	if file.Settings == nil {
		file.Settings = map[string]string{}
	}
	count := 0
	for _, key := range settingKeysForSection(section) {
		row, err := s.settings.GetByUnique(ctx, "", "key", key)
		if err != nil {
			if isNoResultFoundErr(err) {
				continue
			}
			return err
		}
		if row != nil && strings.TrimSpace(row.Value) != "" {
			file.Settings[key] = row.Value
			count++
		}
	}
	file.Manifest.Counts[section] = count
	return nil
}

func (s *backupService) Preview(ctx context.Context, data []byte, passphrase string) (BackupManifest, error) {
	file, err := s.decode(data, passphrase)
	if err != nil {
		return BackupManifest{}, err
	}
	return file.Manifest, nil
}

// decode validates the framing, decrypts with the passphrase, and unmarshals the
// payload. A wrong passphrase surfaces as an authentication error from atrest.
func (s *backupService) decode(data []byte, passphrase string) (*backupFile, error) {
	if len(data) < len(backupMagic)+1 || !bytes.Equal(data[:len(backupMagic)], backupMagic) {
		return nil, errors.New("this is not a mymatasan backup file")
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
		return nil, errors.New("this backup was not created by mymatasan")
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
		res.SchemaWarning = fmt.Sprintf("backup schema v%d differs from this app's v%d; restore proceeded best-effort", file.Manifest.SchemaVersion, backupSchemaVersion)
	}

	// Cameras first: it builds the old→new id remap that AI rules depend on.
	camIDMap := map[int64]int64{}
	if containsString(sections, BackupSectionCameras) {
		if err := s.restoreCameras(ctx, file, mode, camIDMap, &res); err != nil {
			return res, err
		}
	}
	if containsString(sections, BackupSectionAI) {
		if err := s.restoreAI(ctx, file, mode, camIDMap, &res); err != nil {
			return res, err
		}
	}
	if containsString(sections, BackupSectionNotifications) {
		if err := s.restoreSettings(ctx, file, BackupSectionNotifications, &res); err != nil {
			return res, err
		}
	}
	if containsString(sections, BackupSectionSettings) {
		if err := s.restoreSettings(ctx, file, BackupSectionSettings, &res); err != nil {
			return res, err
		}
	}
	return res, nil
}

func (s *backupService) restoreCameras(ctx context.Context, file *backupFile, mode string, idMap map[int64]int64, res *RestoreResult) error {
	if mode == RestoreModeReplace {
		// Children first to avoid dangling references while wiping.
		if err := wipeAll(ctx, s.recordingConfigs, func(r *entities.RecordingConfig) int64 { return r.Id }); err != nil {
			return err
		}
		if err := wipeAll(ctx, s.cameraOnvif, func(o *entities.CameraOnvif) int64 { return o.Id }); err != nil {
			return err
		}
		if err := wipeAll(ctx, s.cameras, func(c *entities.Camera) int64 { return c.Id }); err != nil {
			return err
		}
	}

	onvifByCam := map[int64]backupCameraOnvif{}
	for _, o := range file.CameraOnvif {
		onvifByCam[o.CameraId] = o
	}
	rcByCam := map[int64][]entities.RecordingConfig{}
	for _, rc := range file.RecordingConfigs {
		rcByCam[rc.CameraId] = append(rcByCam[rc.CameraId], rc)
	}

	count := 0
	for _, bc := range file.Cameras {
		oldID := bc.Camera.Id
		cam := bc.Camera
		cam.Id = 0
		cam.TalkPassword = bc.TalkPassword // re-attach the shadowed secret
		newID, err := s.cameras.Create(ctx, "", cam)
		if err != nil {
			return err
		}
		idMap[oldID] = int64(newID)

		if bo, ok := onvifByCam[oldID]; ok {
			onv := bo.CameraOnvif
			onv.Id = 0
			onv.CameraId = int64(newID)
			onv.Password = bo.Password
			if _, err := s.cameraOnvif.Create(ctx, "", onv); err != nil {
				return err
			}
		}
		for _, rc := range rcByCam[oldID] {
			rc.Id = 0
			rc.CameraId = int64(newID)
			if _, err := s.recordingConfigs.Create(ctx, "", rc); err != nil {
				return err
			}
		}
		count++
	}
	res.Restored[BackupSectionCameras] = count
	return nil
}

func (s *backupService) restoreAI(ctx context.Context, file *backupFile, mode string, idMap map[int64]int64, res *RestoreResult) error {
	// The class registry is a global, name-keyed lookup shared with the built-ins
	// this host seeds on boot, so upsert by name rather than wipe-and-insert.
	for _, class := range file.DetectionClasses {
		if err := s.upsertDetectionClass(ctx, class); err != nil {
			return err
		}
	}

	// Rules are camera-scoped; on replace, clear the existing set first.
	if mode == RestoreModeReplace {
		if err := wipeAll(ctx, s.detectionRules, func(r *entities.DetectionRule) int64 { return r.Id }); err != nil {
			return err
		}
	}
	count, skipped := 0, 0
	for _, rule := range file.DetectionRules {
		newCam, ok := idMap[rule.CameraId]
		if !ok {
			// The rule's camera was not restored in this operation, so it cannot be
			// re-linked; skip rather than create a dangling rule.
			skipped++
			continue
		}
		rule.Id = 0
		rule.CameraId = newCam
		if _, err := s.detectionRules.Create(ctx, "", rule); err != nil {
			return err
		}
		count++
	}
	res.Restored[BackupSectionAI] = count
	if skipped > 0 {
		res.Skipped[BackupSectionAI] = skipped
	}
	return nil
}

func (s *backupService) upsertDetectionClass(ctx context.Context, class entities.DetectionClass) error {
	existing, err := s.detectionClasses.GetByUnique(ctx, "", "name", class.Name)
	if err != nil && !isNoResultFoundErr(err) {
		return err
	}
	if existing != nil && existing.Id > 0 && strings.EqualFold(existing.Name, class.Name) {
		class.Id = existing.Id
		_, err := s.detectionClasses.UpdateById(ctx, "", class)
		return err
	}
	class.Id = 0
	_, err = s.detectionClasses.Create(ctx, "", class)
	return err
}

func (s *backupService) restoreSettings(ctx context.Context, file *backupFile, section string, res *RestoreResult) error {
	now := time.Now().UTC().Unix()
	count := 0
	for _, key := range settingKeysForSection(section) {
		value, ok := file.Settings[key]
		if !ok {
			continue
		}
		if err := s.upsertSetting(ctx, key, value, now); err != nil {
			return err
		}
		count++
	}
	res.Restored[section] = count
	return nil
}

func (s *backupService) upsertSetting(ctx context.Context, key, value string, now int64) error {
	row, err := s.settings.GetByUnique(ctx, "", "key", key)
	if err != nil {
		if isNoResultFoundErr(err) {
			_, cerr := s.settings.Create(ctx, "", entities.RuntimeSetting{Key: key, Value: value, CreatedAt: now, UpdatedAt: now})
			return cerr
		}
		return err
	}
	row.Value = value
	row.UpdatedAt = now
	_, err = s.settings.UpdateById(ctx, "", *row)
	return err
}

// wipeAll deletes every row of a table. IDs come from the caller-supplied accessor
// so the helper stays generic without reflection. Row counts are tiny (cameras,
// rules) so a single page is enough.
func wipeAll[T any](ctx context.Context, repo dbsql.IGenericRepo[T], id func(*T) int64) error {
	rows, _, err := repo.Get(ctx, "", backupPageLimit, 0, nil, nil)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if _, err := repo.DeleteById(ctx, "", uint64(id(row))); err != nil {
			return err
		}
	}
	return nil
}

// normalizeSections lower-cases, de-dupes, and reorders the requested sections into
// the canonical order, dropping anything unrecognized.
func normalizeSections(in []string) []string {
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

// restoreSections intersects the requested sections with those present in the file.
// An empty request means "everything present".
func restoreSections(requested, present []string) []string {
	presentSet := map[string]bool{}
	for _, s := range present {
		presentSet[strings.ToLower(strings.TrimSpace(s))] = true
	}
	req := normalizeSections(requested)
	out := make([]string, 0, len(backupAllSections))
	for _, s := range backupAllSections {
		if !presentSet[s] {
			continue
		}
		if len(req) == 0 || containsString(req, s) {
			out = append(out, s)
		}
	}
	return out
}
