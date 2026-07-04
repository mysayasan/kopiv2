package services

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// fakeEntityRepo is a minimal in-memory IGenericRepo covering only the methods the
// backup engine touches. IDs are read/written through the supplied accessors so one
// generic fake serves every entity type; match implements GetByUnique for the few
// callers that need it (the detection-class name lookup).
type fakeEntityRepo[T any] struct {
	dbsql.IGenericRepo[T]
	rows   []*T
	nextID uint64
	getID  func(*T) int64
	setID  func(*T, int64)
	match  func(*T, string, ...any) bool
}

func (f *fakeEntityRepo[T]) Get(_ context.Context, _ string, limit uint64, offset uint64, _ []sqldataenums.Filter, _ []sqldataenums.Sorter) ([]*T, uint64, error) {
	total := uint64(len(f.rows))
	start := offset
	if start > total {
		start = total
	}
	end := total
	if limit > 0 && start+limit < end {
		end = start + limit
	}
	page := make([]*T, 0, end-start)
	for i := start; i < end; i++ {
		cp := *f.rows[i]
		page = append(page, &cp)
	}
	return page, total, nil
}

func (f *fakeEntityRepo[T]) Create(_ context.Context, _ string, model T) (uint64, error) {
	f.nextID++
	f.setID(&model, int64(f.nextID))
	cp := model
	f.rows = append(f.rows, &cp)
	return f.nextID, nil
}

func (f *fakeEntityRepo[T]) DeleteById(_ context.Context, _ string, id uint64) (uint64, error) {
	for i, r := range f.rows {
		if uint64(f.getID(r)) == id {
			f.rows = append(f.rows[:i], f.rows[i+1:]...)
			return 1, nil
		}
	}
	return 0, nil
}

func (f *fakeEntityRepo[T]) GetByUnique(_ context.Context, _ string, keyGroup string, uids ...any) (*T, error) {
	if f.match != nil {
		for _, r := range f.rows {
			if f.match(r, keyGroup, uids...) {
				cp := *r
				return &cp, nil
			}
		}
	}
	return nil, errors.New("no result found")
}

func (f *fakeEntityRepo[T]) UpdateById(_ context.Context, _ string, model T) (uint64, error) {
	mid := f.getID(&model)
	for _, r := range f.rows {
		if f.getID(r) == mid {
			*r = model
			return 1, nil
		}
	}
	return 0, nil
}

type backupTestBundle struct {
	svc              IBackupService
	cameras          *fakeEntityRepo[entities.Camera]
	cameraOnvif      *fakeEntityRepo[entities.CameraOnvif]
	recordingConfigs *fakeEntityRepo[entities.RecordingConfig]
	detectionClasses *fakeEntityRepo[entities.DetectionClass]
	detectionRules   *fakeEntityRepo[entities.DetectionRule]
	settings         *fakeRuntimeSettingRepo
}

func newBackupTestBundle() *backupTestBundle {
	cams := &fakeEntityRepo[entities.Camera]{getID: func(c *entities.Camera) int64 { return c.Id }, setID: func(c *entities.Camera, id int64) { c.Id = id }}
	onvif := &fakeEntityRepo[entities.CameraOnvif]{getID: func(o *entities.CameraOnvif) int64 { return o.Id }, setID: func(o *entities.CameraOnvif, id int64) { o.Id = id }}
	rc := &fakeEntityRepo[entities.RecordingConfig]{getID: func(r *entities.RecordingConfig) int64 { return r.Id }, setID: func(r *entities.RecordingConfig, id int64) { r.Id = id }}
	classes := &fakeEntityRepo[entities.DetectionClass]{
		getID: func(c *entities.DetectionClass) int64 { return c.Id },
		setID: func(c *entities.DetectionClass, id int64) { c.Id = id },
		match: func(c *entities.DetectionClass, keyGroup string, uids ...any) bool {
			if keyGroup != "name" || len(uids) == 0 {
				return false
			}
			name, _ := uids[0].(string)
			return strings.EqualFold(c.Name, name)
		},
	}
	rules := &fakeEntityRepo[entities.DetectionRule]{getID: func(r *entities.DetectionRule) int64 { return r.Id }, setID: func(r *entities.DetectionRule, id int64) { r.Id = id }}
	settings := &fakeRuntimeSettingRepo{}
	svc := NewBackupService(cams, onvif, rc, classes, rules, settings, "test-1.0")
	return &backupTestBundle{svc: svc, cameras: cams, cameraOnvif: onvif, recordingConfigs: rc, detectionClasses: classes, detectionRules: rules, settings: settings}
}

const allBackupSectionsPw = "correct horse battery staple"

func seedSource(t *testing.T, b *backupTestBundle) int64 {
	t.Helper()
	ctx := context.Background()
	camID, err := b.cameras.Create(ctx, "", entities.Camera{Name: "Front Door", Host: "10.0.0.5", TalkPassword: "talk-secret"})
	if err != nil {
		t.Fatalf("seed camera: %v", err)
	}
	if _, err := b.cameraOnvif.Create(ctx, "", entities.CameraOnvif{CameraId: int64(camID), Username: "admin", Password: "onvif-secret", XAddr: "http://10.0.0.5/onvif"}); err != nil {
		t.Fatalf("seed onvif: %v", err)
	}
	if _, err := b.recordingConfigs.Create(ctx, "", entities.RecordingConfig{CameraId: int64(camID), Enabled: true, RetentionDays: 7}); err != nil {
		t.Fatalf("seed recording: %v", err)
	}
	if _, err := b.detectionClasses.Create(ctx, "", entities.DetectionClass{Name: "vehicle", Kind: "object", Members: `["car"]`}); err != nil {
		t.Fatalf("seed class: %v", err)
	}
	if _, err := b.detectionRules.Create(ctx, "", entities.DetectionRule{CameraId: int64(camID), Name: "Driveway", DetectionType: "object"}); err != nil {
		t.Fatalf("seed rule: %v", err)
	}
	if _, err := b.settings.Create(ctx, "", entities.RuntimeSetting{Key: notificationSettingsKey, Value: `{"telegram":{"botToken":"tok-secret"}}`}); err != nil {
		t.Fatalf("seed notif: %v", err)
	}
	if _, err := b.settings.Create(ctx, "", entities.RuntimeSetting{Key: runtimeSettingsKey, Value: `{"decoder":{"threads":4}}`}); err != nil {
		t.Fatalf("seed runtime: %v", err)
	}
	return int64(camID)
}

func TestBackupRoundTripRemapsIDsAndKeepsSecrets(t *testing.T) {
	ctx := context.Background()
	src := newBackupTestBundle()
	seedSource(t, src)

	blob, err := src.svc.Export(ctx, BackupRequest{
		Sections:   []string{BackupSectionCameras, BackupSectionAI, BackupSectionNotifications, BackupSectionSettings},
		Passphrase: allBackupSectionsPw,
	})
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	// Preview reflects the manifest counts without applying anything.
	man, err := src.svc.Preview(ctx, blob, allBackupSectionsPw)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if man.App != "mymatasan" || man.Counts[BackupSectionCameras] != 1 || man.Counts[BackupSectionAI] != 1 {
		t.Fatalf("unexpected manifest: %#v", man)
	}

	// Restore into a target that already has a camera, so the restored camera must
	// get a new id and its children must be remapped to it.
	dst := newBackupTestBundle()
	if _, err := dst.cameras.Create(ctx, "", entities.Camera{Name: "Existing", Host: "10.0.0.9"}); err != nil {
		t.Fatalf("preseed: %v", err)
	}
	res, err := dst.svc.Restore(ctx, blob, RestoreRequest{Passphrase: allBackupSectionsPw, Mode: RestoreModeMerge})
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if res.Restored[BackupSectionCameras] != 1 || res.Restored[BackupSectionAI] != 1 {
		t.Fatalf("unexpected restore result: %#v", res)
	}
	// A restore configures the machine, so first-run setup is marked complete and the
	// wizard won't reappear.
	if !res.SetupCompleted {
		t.Fatalf("restore should mark setup complete: %#v", res)
	}
	if row, err := dst.settings.GetByUnique(ctx, "", "key", setupStateKey); err != nil || !strings.Contains(row.Value, "\"completed\":true") {
		t.Fatalf("setup.state not persisted as completed: %v %#v", err, row)
	}

	// The restored "Front Door" camera got a fresh id (2, after the pre-existing one).
	var restored *entities.Camera
	for _, c := range dst.cameras.rows {
		if c.Name == "Front Door" {
			restored = c
		}
	}
	if restored == nil {
		t.Fatal("Front Door camera was not restored")
	}
	if restored.Id == 1 {
		t.Fatalf("expected a remapped id, got the pre-existing id %d", restored.Id)
	}
	if restored.TalkPassword != "talk-secret" {
		t.Fatalf("talk password not preserved: %q", restored.TalkPassword)
	}

	if len(dst.cameraOnvif.rows) != 1 {
		t.Fatalf("expected 1 onvif row, got %d", len(dst.cameraOnvif.rows))
	}
	onv := dst.cameraOnvif.rows[0]
	if onv.CameraId != restored.Id {
		t.Fatalf("onvif not remapped: CameraId=%d want %d", onv.CameraId, restored.Id)
	}
	if onv.Password != "onvif-secret" {
		t.Fatalf("onvif password not preserved: %q", onv.Password)
	}

	if len(dst.detectionRules.rows) != 1 || dst.detectionRules.rows[0].CameraId != restored.Id {
		t.Fatalf("rule not remapped to restored camera: %#v", dst.detectionRules.rows)
	}

	notif, err := dst.settings.GetByUnique(ctx, "", "key", notificationSettingsKey)
	if err != nil || !strings.Contains(notif.Value, "tok-secret") {
		t.Fatalf("notification secret not restored: %v %#v", err, notif)
	}
}

func TestBackupReplaceModeWipesExisting(t *testing.T) {
	ctx := context.Background()
	src := newBackupTestBundle()
	seedSource(t, src)
	blob, err := src.svc.Export(ctx, BackupRequest{Sections: []string{BackupSectionCameras}, Passphrase: allBackupSectionsPw})
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	dst := newBackupTestBundle()
	dst.cameras.Create(ctx, "", entities.Camera{Name: "Old A", Host: "1.1.1.1"})
	dst.cameras.Create(ctx, "", entities.Camera{Name: "Old B", Host: "1.1.1.2"})

	if _, err := dst.svc.Restore(ctx, blob, RestoreRequest{Passphrase: allBackupSectionsPw, Mode: RestoreModeReplace}); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if len(dst.cameras.rows) != 1 || dst.cameras.rows[0].Name != "Front Door" {
		t.Fatalf("replace should leave exactly the backed-up camera, got %#v", dst.cameras.rows)
	}
}

func TestBackupWrongPassphraseAndBadFileRejected(t *testing.T) {
	ctx := context.Background()
	src := newBackupTestBundle()
	seedSource(t, src)
	blob, err := src.svc.Export(ctx, BackupRequest{Sections: []string{BackupSectionCameras}, Passphrase: allBackupSectionsPw})
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	if _, err := src.svc.Preview(ctx, blob, "wrong"); err == nil {
		t.Fatal("expected wrong passphrase to fail preview")
	}
	if _, err := src.svc.Preview(ctx, []byte("not a backup at all"), allBackupSectionsPw); err == nil {
		t.Fatal("expected non-backup bytes to be rejected")
	}
	if _, err := src.svc.Export(ctx, BackupRequest{Sections: []string{BackupSectionCameras}, Passphrase: ""}); err == nil {
		t.Fatal("expected missing passphrase to be rejected")
	}
	if _, err := src.svc.Export(ctx, BackupRequest{Sections: nil, Passphrase: allBackupSectionsPw}); err == nil {
		t.Fatal("expected empty section selection to be rejected")
	}
}
