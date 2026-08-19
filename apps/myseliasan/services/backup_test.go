package services

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	sharedentities "github.com/mysayasan/kopiv2/domain/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	"github.com/mysayasan/kopiv2/apps/myseliasan/entities"
	"github.com/mysayasan/kopiv2/infra/atrest"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// memRepo is a minimal in-memory dbsql.IGenericRepo. Only the handful of methods the
// backup service actually calls are implemented; the rest panic, so a future change that
// starts using one is a loud test failure rather than a silent empty result.
//
// Ids are assigned and read through reflection on the entity's Id field, which every
// entity in this package has. That is what lets one fake stand in for eleven repos —
// writing eleven hand-rolled fakes is how a test suite stops being maintained.
type memRepo[T any] struct {
	mu   sync.Mutex
	rows []T
	next int64
	// unique maps a row to its unique-key value, for the one caller (control_setting)
	// that looks rows up by key. Nil means GetByUnique always reports not-found.
	unique func(*T) string
}

func newMemRepo[T any](unique func(*T) string) *memRepo[T] {
	return &memRepo[T]{next: 1, unique: unique}
}

func setID[T any](row *T, id int64) { reflect.ValueOf(row).Elem().FieldByName("Id").SetInt(id) }
func getID[T any](row *T) int64     { return reflect.ValueOf(row).Elem().FieldByName("Id").Int() }

func (r *memRepo[T]) Get(_ context.Context, _ string, _ uint64, _ uint64, _ []sqldataenums.Filter, _ []sqldataenums.Sorter) ([]*T, uint64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*T, 0, len(r.rows))
	for i := range r.rows {
		row := r.rows[i]
		out = append(out, &row)
	}
	return out, uint64(len(out)), nil
}

func (r *memRepo[T]) GetById(_ context.Context, _ string, id uint64) (*T, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.rows {
		if getID(&r.rows[i]) == int64(id) {
			row := r.rows[i]
			return &row, nil
		}
	}
	return nil, errNoResultFound
}

func (r *memRepo[T]) GetByUnique(_ context.Context, _ string, _ string, uids ...any) (*T, error) {
	if r.unique == nil || len(uids) == 0 {
		return nil, errNoResultFound
	}
	want, _ := uids[0].(string)
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.rows {
		if r.unique(&r.rows[i]) == want {
			row := r.rows[i]
			return &row, nil
		}
	}
	return nil, errNoResultFound
}

func (r *memRepo[T]) Create(_ context.Context, _ string, model T) (uint64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id := r.next
	r.next++
	setID(&model, id)
	r.rows = append(r.rows, model)
	return uint64(id), nil
}

func (r *memRepo[T]) UpdateById(_ context.Context, _ string, model T) (uint64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id := getID(&model)
	for i := range r.rows {
		if getID(&r.rows[i]) == id {
			r.rows[i] = model
			return 1, nil
		}
	}
	return 0, nil
}

func (r *memRepo[T]) DeleteById(_ context.Context, _ string, id uint64) (uint64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.rows {
		if getID(&r.rows[i]) == int64(id) {
			r.rows = append(r.rows[:i], r.rows[i+1:]...)
			return 1, nil
		}
	}
	return 0, nil
}

// Unused by the backup service. Panicking beats returning a plausible zero value: a
// future call site would otherwise get "no rows" and its test would pass for the wrong
// reason.
func (r *memRepo[T]) GetJoin(context.Context, string, any, uint64, uint64, []sqldataenums.Filter, []sqldataenums.Sorter, ...string) ([]map[string]any, uint64, error) {
	panic("memRepo: GetJoin not implemented")
}
func (r *memRepo[T]) GetJoinWithSpec(context.Context, string, any, uint64, uint64, []sqldataenums.Filter, []sqldataenums.Sorter, ...dbsql.JoinSpec) ([]map[string]any, uint64, error) {
	panic("memRepo: GetJoinWithSpec not implemented")
}
func (r *memRepo[T]) GetSingle(context.Context, string, []sqldataenums.Filter) (*T, error) {
	panic("memRepo: GetSingle not implemented")
}
func (r *memRepo[T]) GetByForeign(context.Context, string, string, ...any) ([]*T, error) {
	panic("memRepo: GetByForeign not implemented")
}
func (r *memRepo[T]) CreateMultiple(context.Context, string, []T) (uint64, error) {
	panic("memRepo: CreateMultiple not implemented")
}
func (r *memRepo[T]) UpdateByUnique(context.Context, string, string, T) (uint64, error) {
	panic("memRepo: UpdateByUnique not implemented")
}
func (r *memRepo[T]) UpdateByForeign(context.Context, string, string, T) (uint64, error) {
	panic("memRepo: UpdateByForeign not implemented")
}
func (r *memRepo[T]) Delete(context.Context, string, []sqldataenums.Filter) (uint64, error) {
	panic("memRepo: Delete not implemented")
}
func (r *memRepo[T]) DeleteByUnique(context.Context, string, string, ...any) (uint64, error) {
	panic("memRepo: DeleteByUnique not implemented")
}
func (r *memRepo[T]) DeleteByForeign(context.Context, string, string, ...any) (uint64, error) {
	panic("memRepo: DeleteByForeign not implemented")
}

// errNoResultFound must be recognised by isNoResultFoundErr, which is how the service
// distinguishes "no such row" from a real failure.
var errNoResultFound = noResultErr{}

type noResultErr struct{}

func (noResultErr) Error() string { return "no result found" }

// testBackupHarness is one fully-wired backup service over in-memory repos, plus a temp
// plan directory. cipher is what its at-rest secrets are sealed with.
type testBackupHarness struct {
	svc      *backupService
	settings *memRepo[entities.ControlSetting]
	roles    *memRepo[sharedentities.AccessRole]
	users    *memRepo[entities.ControlUser]
	nodes    *memRepo[entities.ManagedNode]
	sites    *memRepo[entities.Site]
	floors   *memRepo[entities.FloorPlan]
	audit    *memRepo[entities.AuditLog]
	planDir  string
}

func newTestBackupHarness(t *testing.T, cipher *atrest.Cipher) *testBackupHarness {
	t.Helper()
	h := &testBackupHarness{
		settings: newMemRepo[entities.ControlSetting](func(s *entities.ControlSetting) string { return s.Key }),
		roles:    newMemRepo[sharedentities.AccessRole](nil),
		users:    newMemRepo[entities.ControlUser](nil),
		nodes:    newMemRepo[entities.ManagedNode](nil),
		sites:    newMemRepo[entities.Site](nil),
		floors:   newMemRepo[entities.FloorPlan](nil),
		audit:    newMemRepo[entities.AuditLog](nil),
		planDir:  t.TempDir(),
	}
	h.svc = &backupService{
		roles:       h.roles,
		permissions: newMemRepo[sharedentities.AccessRolePermission](nil),
		users:       h.users,
		settings:    h.settings,
		nodes:       h.nodes,
		grants:      newMemRepo[entities.NodeAccessGrant](nil),
		sites:       h.sites,
		floors:      h.floors,
		placements:  newMemRepo[entities.NodePlacement](nil),
		rules:       newMemRepo[entities.FleetRule](nil),
		audit:       h.audit,
		cipher:      cipher,
		planDir:     h.planDir,
		appVersion:  "1.2.3",
	}
	return h
}

// seedSetting writes a control_setting the way the app would: sealed keys go through
// encodeSecret, everything else is stored verbatim.
func (h *testBackupHarness) seedSetting(t *testing.T, key, value string) {
	t.Helper()
	stored := value
	if containsString(sealedSettingKeys, key) {
		enc, err := encodeSecret(h.svc.cipher, value)
		if err != nil {
			t.Fatalf("encodeSecret %s: %v", key, err)
		}
		stored = enc
	}
	if _, err := h.settings.Create(context.Background(), "", entities.ControlSetting{Key: key, Value: stored}); err != nil {
		t.Fatalf("seed setting %s: %v", key, err)
	}
}

func (h *testBackupHarness) settingValue(key string) string {
	rows, _, _ := h.settings.Get(context.Background(), "", 0, 0, nil, nil)
	for _, row := range rows {
		if row.Key == key {
			return row.Value
		}
	}
	return ""
}

func testCipherWithSeed(t *testing.T, seed byte) *atrest.Cipher {
	t.Helper()
	key := make([]byte, atrest.KeySize)
	for i := range key {
		key[i] = seed + byte(i)
	}
	c, err := atrest.NewCipher(key)
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}
	return c
}

const testCAKeyPEM = "-----BEGIN EC PRIVATE KEY-----\nfleet-ca-private-material\n-----END EC PRIVATE KEY-----"

// TestBackupRestoreAcrossDifferentAtRestKeys is THE test this whole file exists for.
//
// A backup is worthless if it only restores onto the machine that made it. The disaster
// it has to survive is the one where the host is gone: a fresh install generates a NEW
// at-rest key, so the CA private key, the fleet PSK and the encrypted floor plan images
// all have to come back out from under the old key and go back under the new one. If that
// dance is wrong the restore reports success and the fleet still never reconnects — which
// is a worse outcome than no backup at all, because nobody finds out until they need it.
func TestBackupRestoreAcrossDifferentAtRestKeys(t *testing.T) {
	ctx := context.Background()
	sourceCipher := testCipherWithSeed(t, 1)
	src := newTestBackupHarness(t, sourceCipher)

	src.seedSetting(t, caCertKey, "-----BEGIN CERTIFICATE-----\nca-public\n-----END CERTIFICATE-----")
	src.seedSetting(t, caKeyKey, testCAKeyPEM)
	src.seedSetting(t, fleetKeySettingKey, "fleet-psk-abcdef123456")
	src.seedSetting(t, "deployment.mode", "appliance")

	// A floor with an encrypted plan image on disk, exactly as the site service writes it.
	planBytes := []byte("\x89PNG\r\n\x1a\n-pretend-floor-plan-pixels")
	siteID, err := src.sites.Create(ctx, "", entities.Site{Name: "HQ"})
	if err != nil {
		t.Fatalf("seed site: %v", err)
	}
	floorID, err := src.floors.Create(ctx, "", entities.FloorPlan{SiteId: int64(siteID), Name: "Ground"})
	if err != nil {
		t.Fatalf("seed floor: %v", err)
	}
	imgPath := filepath.Join(src.planDir, "floor-1.img")
	sealedImg, err := sourceCipher.EncryptBytes(planBytes)
	if err != nil {
		t.Fatalf("encrypt plan: %v", err)
	}
	if err := os.WriteFile(imgPath, sealedImg, 0o644); err != nil {
		t.Fatalf("write plan: %v", err)
	}
	if _, err := src.floors.UpdateById(ctx, "", entities.FloorPlan{
		Id: int64(floorID), SiteId: int64(siteID), Name: "Ground", ImagePath: imgPath, ContentType: "image/png",
	}); err != nil {
		t.Fatalf("attach plan: %v", err)
	}

	blob, err := src.svc.Export(ctx, BackupRequest{Passphrase: "correct-horse-battery"})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}

	// A DIFFERENT host: new at-rest key, empty database, empty plan directory.
	destCipher := testCipherWithSeed(t, 200)
	dst := newTestBackupHarness(t, destCipher)

	res, err := dst.svc.Restore(ctx, blob, RestoreRequest{Passphrase: "correct-horse-battery", Mode: RestoreModeReplace})
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if !res.RestartRequired {
		t.Error("restoring the fleet CA must require a restart: the running fleetCA caches the old CA in memory")
	}

	// The CA private key must be readable again — under the DESTINATION key.
	stored := dst.settingValue(caKeyKey)
	if stored == "" {
		t.Fatal("CA private key missing after restore")
	}
	if stored == testCAKeyPEM {
		t.Error("CA private key was stored as plaintext on the destination; it must be re-sealed")
	}
	if got := decodeSecret(destCipher, stored); got != testCAKeyPEM {
		t.Fatalf("CA private key did not survive the key change: got %q", got)
	}
	// And it must NOT be readable with the source key any more — proving it was genuinely
	// re-sealed rather than carried across still wrapped in the old key.
	if got := decodeSecret(sourceCipher, stored); got == testCAKeyPEM {
		t.Error("CA private key is still sealed under the SOURCE key; restore did not re-seal it")
	}

	if got := decodeSecret(destCipher, dst.settingValue(fleetKeySettingKey)); got != "fleet-psk-abcdef123456" {
		t.Errorf("fleet PSK did not survive the key change: got %q", got)
	}
	// A plaintext setting must stay plaintext — sealing it would hand its reader base64.
	if got := dst.settingValue("deployment.mode"); got != "appliance" {
		t.Errorf("plaintext setting was transformed: got %q want %q", got, "appliance")
	}

	// The floor plan image must be back on disk, re-encrypted under the destination key.
	floors, _, _ := dst.floors.Get(ctx, "", 0, 0, nil, nil)
	if len(floors) != 1 {
		t.Fatalf("expected 1 restored floor, got %d", len(floors))
	}
	if floors[0].ImagePath == "" {
		t.Fatal("restored floor has no plan image path")
	}
	onDisk, err := os.ReadFile(floors[0].ImagePath)
	if err != nil {
		t.Fatalf("read restored plan: %v", err)
	}
	roundTripped, err := destCipher.DecryptBytes(onDisk)
	if err != nil {
		t.Fatalf("restored plan does not decrypt with the destination key: %v", err)
	}
	if string(roundTripped) != string(planBytes) {
		t.Error("restored plan image bytes differ from the original")
	}
}

// TestBackupRestoreKeepsLocalPasswords guards the other json:"-" field, and the reason it
// exists is that the first real restore bench produced a control plane NOBODY COULD SIGN
// IN TO: every local account came back with an empty password, because ControlUser's hash
// is json:"-" and was silently dropped by the bundle's JSON encoding.
//
// The round-trip here deliberately goes through Export and Restore rather than the plan
// helpers, because marshalling is exactly where the field disappeared — a test that only
// exercises the repo layer round-trips the struct in memory and can never see it.
func TestBackupRestoreKeepsLocalPasswords(t *testing.T) {
	ctx := context.Background()
	src := newTestBackupHarness(t, testCipherWithSeed(t, 1))
	if _, err := src.users.Create(ctx, "", entities.ControlUser{
		Username: "admin", Kind: "local", Email: "admin@example.com",
		PasswordHash: "$2a$10$abcdefghijklmnopqrstuvwxyz0123456789ABCDEFGHIJKLMNOPQR",
	}); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	blob, err := src.svc.Export(ctx, BackupRequest{Sections: []string{BackupSectionUsers}, Passphrase: "passphrase-1"})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	dst := newTestBackupHarness(t, testCipherWithSeed(t, 99))
	if _, err := dst.svc.Restore(ctx, blob, RestoreRequest{Passphrase: "passphrase-1", Mode: RestoreModeReplace}); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	users, _, _ := dst.users.Get(ctx, "", 0, 0, nil, nil)
	if len(users) != 1 {
		t.Fatalf("expected 1 restored user, got %d", len(users))
	}
	if users[0].PasswordHash != "$2a$10$abcdefghijklmnopqrstuvwxyz0123456789ABCDEFGHIJKLMNOPQR" {
		t.Fatalf("the password hash did not survive the round-trip (got %q) — every restored account would be locked out", users[0].PasswordHash)
	}
	if users[0].Username != "admin" {
		t.Errorf("username = %q", users[0].Username)
	}
}

// TestBackupRestoreKeepsNodeToken guards a field that is invisible to JSON. ManagedNode.Token
// is json:"-" so it never reaches a browser, which means a naive round-trip drops it — and a
// node restored without its token is adopted-but-unauthenticated: it fails its next heartbeat
// and falls out of the fleet, with the registry still listing it as present.
func TestBackupRestoreKeepsNodeToken(t *testing.T) {
	ctx := context.Background()
	src := newTestBackupHarness(t, testCipherWithSeed(t, 1))
	if _, err := src.nodes.Create(ctx, "", entities.ManagedNode{
		NodeId: "node-alpha", Name: "Lobby NVR", Token: "tok-secret-value", Kind: "camera",
	}); err != nil {
		t.Fatalf("seed node: %v", err)
	}

	blob, err := src.svc.Export(ctx, BackupRequest{Sections: []string{BackupSectionFleet}, Passphrase: "passphrase-1"})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}

	dst := newTestBackupHarness(t, testCipherWithSeed(t, 99))
	if _, err := dst.svc.Restore(ctx, blob, RestoreRequest{Passphrase: "passphrase-1", Mode: RestoreModeReplace}); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	nodes, _, _ := dst.nodes.Get(ctx, "", 0, 0, nil, nil)
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	if nodes[0].Token != "tok-secret-value" {
		t.Fatalf("node token lost in backup round-trip: got %q", nodes[0].Token)
	}
	if nodes[0].NodeId != "node-alpha" {
		t.Fatalf("node id changed: got %q", nodes[0].NodeId)
	}
}

// TestRestoreSettingsLeavesFleetCAAlone covers the split between the two sections that
// share the control_setting table. Restoring "settings" in replace mode wipes that
// section first — and if the wipe is not scoped by key it takes the CA private key with
// it, silently destroying fleet trust as a side effect of restoring a display preference.
func TestRestoreSettingsLeavesFleetCAAlone(t *testing.T) {
	ctx := context.Background()
	src := newTestBackupHarness(t, testCipherWithSeed(t, 1))
	src.seedSetting(t, "deployment.mode", "cluster")

	blob, err := src.svc.Export(ctx, BackupRequest{Sections: []string{BackupSectionSettings}, Passphrase: "passphrase-1"})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}

	// The destination already has a working CA that is NOT in this backup.
	dst := newTestBackupHarness(t, testCipherWithSeed(t, 1))
	dst.seedSetting(t, caKeyKey, testCAKeyPEM)
	dst.seedSetting(t, "deployment.mode", "appliance")

	if _, err := dst.svc.Restore(ctx, blob, RestoreRequest{Passphrase: "passphrase-1", Mode: RestoreModeReplace}); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if got := decodeSecret(dst.svc.cipher, dst.settingValue(caKeyKey)); got != testCAKeyPEM {
		t.Fatal("restoring the settings section destroyed the fleet CA private key")
	}
	if got := dst.settingValue("deployment.mode"); got != "cluster" {
		t.Fatalf("settings section did not apply: got %q", got)
	}
}

// TestRestoreAuditNeverWipes: the audit trail is append-only by construction — the audit
// service has no update or delete path so the record cannot be edited after the fact.
// Honouring replace mode here would hand an operator a supported way to erase it, by
// restoring a backup whose audit section is empty over a populated one.
func TestRestoreAuditNeverWipes(t *testing.T) {
	ctx := context.Background()
	src := newTestBackupHarness(t, testCipherWithSeed(t, 1))
	if _, err := src.audit.Create(ctx, "", entities.AuditLog{Action: "node.adopt", Detail: "from backup"}); err != nil {
		t.Fatalf("seed audit: %v", err)
	}
	blob, err := src.svc.Export(ctx, BackupRequest{Sections: []string{BackupSectionAudit}, Passphrase: "passphrase-1"})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}

	dst := newTestBackupHarness(t, testCipherWithSeed(t, 1))
	if _, err := dst.audit.Create(ctx, "", entities.AuditLog{Action: "rbac.elevate", Detail: "pre-existing"}); err != nil {
		t.Fatalf("seed destination audit: %v", err)
	}
	if _, err := dst.svc.Restore(ctx, blob, RestoreRequest{Passphrase: "passphrase-1", Mode: RestoreModeReplace}); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	rows, _, _ := dst.audit.Get(ctx, "", 0, 0, nil, nil)
	if len(rows) != 2 {
		t.Fatalf("audit restore must APPEND even in replace mode: got %d rows, want 2", len(rows))
	}
	var sawPreExisting bool
	for _, row := range rows {
		if row.Detail == "pre-existing" {
			sawPreExisting = true
		}
	}
	if !sawPreExisting {
		t.Fatal("replace-mode restore erased an existing audit entry")
	}
}

// TestRestoreUsersDropsUnmappedRole: role ids are reassigned on restore, so a user whose
// role did not travel with the backup must end up with NO role rather than keeping a
// stale id — which on the destination host names a completely different role, possibly a
// superadmin one. Pending-clearance is the safe landing spot.
func TestRestoreUsersDropsUnmappedRole(t *testing.T) {
	ctx := context.Background()
	src := newTestBackupHarness(t, testCipherWithSeed(t, 1))
	if _, err := src.users.Create(ctx, "", entities.ControlUser{
		Username: "operator", Email: "op@example.com", RoleId: 42,
	}); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	// Users only — the roles section is deliberately absent, so nothing maps id 42.
	blob, err := src.svc.Export(ctx, BackupRequest{Sections: []string{BackupSectionUsers}, Passphrase: "passphrase-1"})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}

	dst := newTestBackupHarness(t, testCipherWithSeed(t, 1))
	res, err := dst.svc.Restore(ctx, blob, RestoreRequest{Passphrase: "passphrase-1", Mode: RestoreModeReplace})
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	users, _, _ := dst.users.Get(ctx, "", 0, 0, nil, nil)
	if len(users) != 1 {
		t.Fatalf("expected 1 user, got %d", len(users))
	}
	if users[0].RoleId != 0 {
		t.Fatalf("unmapped role id was carried across: got %d, want 0", users[0].RoleId)
	}
	if res.Skipped[BackupSectionUsers] != 1 {
		t.Errorf("dropping a role should be reported as skipped, got %v", res.Skipped)
	}
}

// TestBackupRejectsForeignAndCorruptFiles: the magic is checked before any passphrase
// work, so uploading a .idbackup — which uses the same envelope and would otherwise
// decrypt — fails fast with an answer the operator can act on.
func TestBackupRejectsForeignAndCorruptFiles(t *testing.T) {
	ctx := context.Background()
	h := newTestBackupHarness(t, testCipherWithSeed(t, 1))

	cases := []struct {
		name string
		data []byte
		want string
	}{
		{"empty", nil, "not a myseliasan backup"},
		{"idbackup magic", append([]byte("IDBK"), 1, 0, 0), "not a myseliasan backup"},
		{"truncated", []byte("SEL"), "not a myseliasan backup"},
		{"unsupported version", append(append([]byte{}, backupMagic...), 99), "unsupported backup format version"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := h.svc.Preview(ctx, tc.data, "passphrase-1")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want error containing %q", err, tc.want)
			}
		})
	}
}

func TestBackupRejectsWrongPassphrase(t *testing.T) {
	ctx := context.Background()
	h := newTestBackupHarness(t, testCipherWithSeed(t, 1))
	h.seedSetting(t, caKeyKey, testCAKeyPEM)

	blob, err := h.svc.Export(ctx, BackupRequest{Passphrase: "the-right-one"})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if _, err := h.svc.Preview(ctx, blob, "the-wrong-one"); err == nil {
		t.Fatal("a wrong passphrase must not open the backup")
	}
	if _, err := h.svc.Export(ctx, BackupRequest{Passphrase: "   "}); err == nil {
		t.Fatal("an empty passphrase must be refused at export")
	}
}

// TestExportedBlobHidesSecrets: the whole payload is passphrase-encrypted, so the CA
// private key must not be findable in the bytes on disk. A regression here — an
// unencrypted or partially-encrypted envelope — would put the fleet trust root in
// plaintext in whatever the operator emails the file through.
func TestExportedBlobHidesSecrets(t *testing.T) {
	ctx := context.Background()
	h := newTestBackupHarness(t, testCipherWithSeed(t, 1))
	h.seedSetting(t, caKeyKey, testCAKeyPEM)
	h.seedSetting(t, fleetKeySettingKey, "fleet-psk-abcdef123456")

	blob, err := h.svc.Export(ctx, BackupRequest{Passphrase: "correct-horse-battery"})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	for _, secret := range []string{"fleet-ca-private-material", "fleet-psk-abcdef123456"} {
		if strings.Contains(string(blob), secret) {
			t.Fatalf("exported backup leaks %q in plaintext", secret)
		}
	}
	if !strings.HasPrefix(string(blob), "SELB") {
		t.Error("exported backup is missing its magic prefix")
	}
}

// TestAvailableSectionsSplitsSettingRows proves the section counts an operator sees before
// exporting reflect the same fleetca/settings split the restore enforces.
func TestAvailableSectionsSplitsSettingRows(t *testing.T) {
	ctx := context.Background()
	h := newTestBackupHarness(t, testCipherWithSeed(t, 1))
	h.seedSetting(t, caCertKey, "cert")
	h.seedSetting(t, caKeyKey, testCAKeyPEM)
	h.seedSetting(t, "deployment.mode", "appliance")
	h.seedSetting(t, "agent.schedule", "daily")

	info, err := h.svc.AvailableSections(ctx)
	if err != nil {
		t.Fatalf("AvailableSections: %v", err)
	}
	counts := map[string]int{}
	for _, s := range info {
		counts[s.Id] = s.Count
	}
	if counts[BackupSectionFleetCA] != 2 {
		t.Errorf("fleetca count = %d, want 2 (caCert + caKey)", counts[BackupSectionFleetCA])
	}
	if counts[BackupSectionSettings] != 2 {
		t.Errorf("settings count = %d, want 2 (deployment.mode + agent.schedule)", counts[BackupSectionSettings])
	}
}

// TestBackupWithoutCipherRoundTrips covers the encryption-disabled install: values are
// already plaintext in the database, so export must not try to unseal them and restore
// must not seal them.
func TestBackupWithoutCipherRoundTrips(t *testing.T) {
	ctx := context.Background()
	src := newTestBackupHarness(t, nil)
	src.seedSetting(t, caKeyKey, testCAKeyPEM)

	blob, err := src.svc.Export(ctx, BackupRequest{Passphrase: "passphrase-1"})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	dst := newTestBackupHarness(t, nil)
	if _, err := dst.svc.Restore(ctx, blob, RestoreRequest{Passphrase: "passphrase-1", Mode: RestoreModeReplace}); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if got := dst.settingValue(caKeyKey); got != testCAKeyPEM {
		t.Fatalf("plaintext install round-trip changed the CA key: got %q", got)
	}
}

// TestRestoreMergeUpdatesExistingSetting: control_setting.Key is unique, so a merge-mode
// restore that blindly inserted would fail on every key the destination already has.
func TestRestoreMergeUpdatesExistingSetting(t *testing.T) {
	ctx := context.Background()
	src := newTestBackupHarness(t, testCipherWithSeed(t, 1))
	src.seedSetting(t, "deployment.mode", "cluster")
	blob, err := src.svc.Export(ctx, BackupRequest{Sections: []string{BackupSectionSettings}, Passphrase: "passphrase-1"})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}

	dst := newTestBackupHarness(t, testCipherWithSeed(t, 1))
	dst.seedSetting(t, "deployment.mode", "appliance")
	if _, err := dst.svc.Restore(ctx, blob, RestoreRequest{Passphrase: "passphrase-1", Mode: RestoreModeMerge}); err != nil {
		t.Fatalf("Restore(merge): %v", err)
	}
	rows, _, _ := dst.settings.Get(ctx, "", 0, 0, nil, nil)
	var hits int
	for _, row := range rows {
		if row.Key == "deployment.mode" {
			hits++
			if row.Value != "cluster" {
				t.Errorf("merge did not update the value: got %q", row.Value)
			}
		}
	}
	if hits != 1 {
		t.Fatalf("merge duplicated a unique key: %d rows for deployment.mode", hits)
	}
}

var _ dbsql.IGenericRepo[entities.ControlSetting] = (*memRepo[entities.ControlSetting])(nil)

// keep base64 imported for the harness helpers above even if a future edit drops the last
// direct use; the plan-image assertions rely on it.
var _ = base64.StdEncoding
