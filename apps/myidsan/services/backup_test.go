package services

import (
	"context"
	"strings"
	"testing"

	myidsanentities "github.com/mysayasan/kopiv2/apps/myidsan/entities"
	sharedentities "github.com/mysayasan/kopiv2/domain/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	"github.com/mysayasan/kopiv2/infra/atrest"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// fakeBackupRepo is a minimal in-memory IGenericRepo covering only what the backup
// engine touches. One generic fake serves every entity via the id accessors.
type fakeBackupRepo[T any] struct {
	dbsql.IGenericRepo[T]
	rows   []*T
	nextID uint64
	getID  func(*T) int64
	setID  func(*T, int64)
}

func (f *fakeBackupRepo[T]) Get(_ context.Context, _ string, limit uint64, offset uint64, _ []sqldataenums.Filter, _ []sqldataenums.Sorter) ([]*T, uint64, error) {
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

func (f *fakeBackupRepo[T]) Create(_ context.Context, _ string, model T) (uint64, error) {
	f.nextID++
	f.setID(&model, int64(f.nextID))
	cp := model
	f.rows = append(f.rows, &cp)
	return f.nextID, nil
}

func (f *fakeBackupRepo[T]) DeleteById(_ context.Context, _ string, id uint64) (uint64, error) {
	for i, r := range f.rows {
		if uint64(f.getID(r)) == id {
			f.rows = append(f.rows[:i], f.rows[i+1:]...)
			return 1, nil
		}
	}
	return 0, nil
}

func newRepo[T any](getID func(*T) int64, setID func(*T, int64)) *fakeBackupRepo[T] {
	return &fakeBackupRepo[T]{getID: getID, setID: setID}
}

// backupHarness holds the repos so a test can inspect what a restore actually wrote.
type backupHarness struct {
	roles       *fakeBackupRepo[sharedentities.AccessRole]
	permissions *fakeBackupRepo[sharedentities.AccessRolePermission]
	users       *fakeBackupRepo[sharedentities.UserLogin]
	groups      *fakeBackupRepo[sharedentities.UserGroup]
	avatars     *fakeBackupRepo[myidsanentities.UserAvatar]
	mfaFactors  *fakeBackupRepo[myidsanentities.UserMfaFactor]
	mfaCodes    *fakeBackupRepo[myidsanentities.UserMfaRecoveryCode]
	appRegistry *fakeBackupRepo[sharedentities.AppRegistry]
	appAuth     *fakeBackupRepo[sharedentities.AppAuthConfig]
	appRedirect *fakeBackupRepo[sharedentities.AppRedirectUri]
	directory   *fakeBackupRepo[myidsanentities.DirectoryConfig]
	groupMaps   *fakeBackupRepo[myidsanentities.FederatedGroupMapping]
	ssoCa       *fakeBackupRepo[myidsanentities.SsoCa]
	svc         IBackupService
}

func newBackupHarness(t *testing.T, cipher *atrest.Cipher) *backupHarness {
	t.Helper()
	h := &backupHarness{
		roles:       newRepo(func(r *sharedentities.AccessRole) int64 { return r.Id }, func(r *sharedentities.AccessRole, id int64) { r.Id = id }),
		permissions: newRepo(func(p *sharedentities.AccessRolePermission) int64 { return p.Id }, func(p *sharedentities.AccessRolePermission, id int64) { p.Id = id }),
		users:       newRepo(func(u *sharedentities.UserLogin) int64 { return u.Id }, func(u *sharedentities.UserLogin, id int64) { u.Id = id }),
		groups:      newRepo(func(g *sharedentities.UserGroup) int64 { return g.Id }, func(g *sharedentities.UserGroup, id int64) { g.Id = id }),
		avatars:     newRepo(func(a *myidsanentities.UserAvatar) int64 { return a.Id }, func(a *myidsanentities.UserAvatar, id int64) { a.Id = id }),
		mfaFactors:  newRepo(func(f *myidsanentities.UserMfaFactor) int64 { return f.Id }, func(f *myidsanentities.UserMfaFactor, id int64) { f.Id = id }),
		mfaCodes:    newRepo(func(c *myidsanentities.UserMfaRecoveryCode) int64 { return c.Id }, func(c *myidsanentities.UserMfaRecoveryCode, id int64) { c.Id = id }),
		appRegistry: newRepo(func(a *sharedentities.AppRegistry) int64 { return a.Id }, func(a *sharedentities.AppRegistry, id int64) { a.Id = id }),
		appAuth:     newRepo(func(a *sharedentities.AppAuthConfig) int64 { return a.Id }, func(a *sharedentities.AppAuthConfig, id int64) { a.Id = id }),
		appRedirect: newRepo(func(u *sharedentities.AppRedirectUri) int64 { return u.Id }, func(u *sharedentities.AppRedirectUri, id int64) { u.Id = id }),
		directory:   newRepo(func(d *myidsanentities.DirectoryConfig) int64 { return d.Id }, func(d *myidsanentities.DirectoryConfig, id int64) { d.Id = id }),
		groupMaps:   newRepo(func(m *myidsanentities.FederatedGroupMapping) int64 { return m.Id }, func(m *myidsanentities.FederatedGroupMapping, id int64) { m.Id = id }),
		ssoCa:       newRepo(func(c *myidsanentities.SsoCa) int64 { return c.Id }, func(c *myidsanentities.SsoCa, id int64) { c.Id = id }),
	}
	h.svc = NewBackupService(
		h.roles, h.permissions, h.users, h.groups, h.avatars, h.mfaFactors, h.mfaCodes,
		h.appRegistry, h.appAuth, h.appRedirect, h.directory, h.groupMaps, h.ssoCa,
		cipher, nil, nil, "test-1.0.0",
	)
	return h
}

func testCipher(t *testing.T) *atrest.Cipher {
	t.Helper()
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	c, err := atrest.NewCipher(key)
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}
	return c
}

// seedSource fills a harness with one of everything, wired together by id so the restore
// side has real cross-references to remap.
func seedSource(t *testing.T, h *backupHarness, cipher *atrest.Cipher) {
	t.Helper()
	ctx := context.Background()

	if _, err := h.roles.Create(ctx, "", sharedentities.AccessRole{Name: "operators"}); err != nil {
		t.Fatal(err)
	}
	roleID := h.roles.rows[0].Id
	if _, err := h.permissions.Create(ctx, "", sharedentities.AccessRolePermission{RoleId: roleID, Path: "/api/user-credential", CanGet: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.users.Create(ctx, "", sharedentities.UserLogin{Email: "alice@corp.local", Userpwd: "$2a$10$hashedhashedhashed", UserRoleId: roleID, IsActive: true}); err != nil {
		t.Fatal(err)
	}
	userID := h.users.rows[0].Id

	sealed, err := encodeSecret(cipher, "JBSWY3DPEHPK3PXP")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.mfaFactors.Create(ctx, "", myidsanentities.UserMfaFactor{UserLoginId: userID, Kind: "totp", SecretEnc: sealed, ConfirmedAt: 123}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.mfaCodes.Create(ctx, "", myidsanentities.UserMfaRecoveryCode{UserLoginId: userID, CodeHash: "$2a$10$recoverycodehash"}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.avatars.Create(ctx, "", myidsanentities.UserAvatar{UserLoginId: userID, ContentType: "image/jpeg", DataB64: "AAAA"}); err != nil {
		t.Fatal(err)
	}

	if _, err := h.appRegistry.Create(ctx, "", sharedentities.AppRegistry{Code: "myseliasan", Audience: "myseliasan", IsActive: true}); err != nil {
		t.Fatal(err)
	}
	appID := h.appRegistry.rows[0].Id
	if _, err := h.appAuth.Create(ctx, "", sharedentities.AppAuthConfig{AppRegistryId: appID, ClientId: "myseliasan", ClientSecretHash: "abc", IsActive: true}); err != nil {
		t.Fatal(err)
	}
	authID := h.appAuth.rows[0].Id
	if _, err := h.appRedirect.Create(ctx, "", sharedentities.AppRedirectUri{AppAuthConfigId: authID, RedirectUri: "https://fleet.local/callback", IsActive: true}); err != nil {
		t.Fatal(err)
	}

	bindSealed, err := encodeSecret(cipher, "s3rvice-bind-pw")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.directory.Create(ctx, "", myidsanentities.DirectoryConfig{Name: "default", Host: "dc.corp.local", BindPassword: bindSealed}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.groupMaps.Create(ctx, "", myidsanentities.FederatedGroupMapping{Provider: "ldap", GroupName: "cn=eng,dc=corp", RoleId: roleID}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.ssoCa.Create(ctx, "", myidsanentities.SsoCa{Name: "default", CertPem: "CERT", KeyPem: "KEY"}); err != nil {
		t.Fatal(err)
	}
}

const testPassphrase = "a-sufficiently-long-passphrase"

// THE test for this feature. A backup restored onto a host with a DIFFERENT at-rest key
// must yield a usable TOTP secret and LDAP bind password. Carrying the sealed bytes would
// restore "successfully" and then fail every second-factor check — a failure only
// discovered when someone tries to log in, which is the worst time to discover it.
func TestBackupRoundTripReSealsSecretsForTheDestinationHost(t *testing.T) {
	ctx := context.Background()
	srcCipher := testCipher(t)
	src := newBackupHarness(t, srcCipher)
	seedSource(t, src, srcCipher)

	blob, err := src.svc.Export(ctx, BackupRequest{Passphrase: testPassphrase})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}

	// A different host: different at-rest key.
	dstKey := make([]byte, 32)
	for i := range dstKey {
		dstKey[i] = byte(200 - i)
	}
	dstCipher, err := atrest.NewCipher(dstKey)
	if err != nil {
		t.Fatal(err)
	}
	dst := newBackupHarness(t, dstCipher)

	if _, err := dst.svc.Restore(ctx, blob, RestoreRequest{Passphrase: testPassphrase, Mode: RestoreModeReplace}); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	if len(dst.mfaFactors.rows) != 1 {
		t.Fatalf("expected 1 restored MFA factor, got %d", len(dst.mfaFactors.rows))
	}
	got := decodeSecret(dstCipher, dst.mfaFactors.rows[0].SecretEnc)
	if got != "JBSWY3DPEHPK3PXP" {
		t.Errorf("TOTP secret unusable on the destination host: got %q", got)
	}
	// It must genuinely be re-sealed, not stored in the clear.
	if dst.mfaFactors.rows[0].SecretEnc == "JBSWY3DPEHPK3PXP" {
		t.Error("TOTP secret was written to the database in plaintext")
	}
	// And the source host's sealed bytes must not have survived verbatim.
	if dst.mfaFactors.rows[0].SecretEnc == src.mfaFactors.rows[0].SecretEnc {
		t.Error("sealed bytes were copied verbatim; they cannot be decrypted with the destination key")
	}

	if len(dst.directory.rows) != 1 {
		t.Fatalf("expected 1 restored directory config, got %d", len(dst.directory.rows))
	}
	if pw := decodeSecret(dstCipher, dst.directory.rows[0].BindPassword); pw != "s3rvice-bind-pw" {
		t.Errorf("LDAP bind password unusable on the destination host: got %q", pw)
	}
}

// Cross-references must be remapped, not carried. The destination assigns fresh ids, so a
// copied UserRoleId would point at whatever role happens to occupy that id.
func TestRestoreRemapsForeignKeys(t *testing.T) {
	ctx := context.Background()
	cipher := testCipher(t)
	src := newBackupHarness(t, cipher)
	seedSource(t, src, cipher)

	blob, err := src.svc.Export(ctx, BackupRequest{Passphrase: testPassphrase})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}

	dst := newBackupHarness(t, cipher)
	// Pre-load the destination so fresh ids do NOT line up with the source's.
	for i := 0; i < 5; i++ {
		if _, err := dst.roles.Create(ctx, "", sharedentities.AccessRole{Name: "filler"}); err != nil {
			t.Fatal(err)
		}
		if _, err := dst.users.Create(ctx, "", sharedentities.UserLogin{Email: "filler"}); err != nil {
			t.Fatal(err)
		}
		if _, err := dst.appRegistry.Create(ctx, "", sharedentities.AppRegistry{Code: "filler"}); err != nil {
			t.Fatal(err)
		}
		if _, err := dst.appAuth.Create(ctx, "", sharedentities.AppAuthConfig{ClientId: "filler"}); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := dst.svc.Restore(ctx, blob, RestoreRequest{Passphrase: testPassphrase, Mode: RestoreModeReplace}); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	if len(dst.roles.rows) != 1 || len(dst.users.rows) != 1 {
		t.Fatalf("replace mode should have wiped the filler rows: roles=%d users=%d", len(dst.roles.rows), len(dst.users.rows))
	}
	newRoleID := dst.roles.rows[0].Id
	if dst.users.rows[0].UserRoleId != newRoleID {
		t.Errorf("user role id = %d, want the remapped %d", dst.users.rows[0].UserRoleId, newRoleID)
	}
	if dst.permissions.rows[0].RoleId != newRoleID {
		t.Errorf("permission role id = %d, want %d", dst.permissions.rows[0].RoleId, newRoleID)
	}
	if dst.groupMaps.rows[0].RoleId != newRoleID {
		t.Errorf("group mapping role id = %d, want %d", dst.groupMaps.rows[0].RoleId, newRoleID)
	}

	newUserID := dst.users.rows[0].Id
	if dst.mfaFactors.rows[0].UserLoginId != newUserID {
		t.Errorf("mfa factor user id = %d, want %d", dst.mfaFactors.rows[0].UserLoginId, newUserID)
	}
	if dst.mfaCodes.rows[0].UserLoginId != newUserID {
		t.Errorf("recovery code user id = %d, want %d", dst.mfaCodes.rows[0].UserLoginId, newUserID)
	}
	if dst.avatars.rows[0].UserLoginId != newUserID {
		t.Errorf("avatar user id = %d, want %d", dst.avatars.rows[0].UserLoginId, newUserID)
	}

	if dst.appAuth.rows[0].AppRegistryId != dst.appRegistry.rows[0].Id {
		t.Error("app auth config was not remapped onto its restored registry row")
	}
	if dst.appRedirect.rows[0].AppAuthConfigId != dst.appAuth.rows[0].Id {
		t.Error("redirect URI was not remapped onto its restored auth config")
	}
}

// Password hashes must survive verbatim, or every restored user is locked out.
func TestRestorePreservesPasswordHashes(t *testing.T) {
	ctx := context.Background()
	cipher := testCipher(t)
	src := newBackupHarness(t, cipher)
	seedSource(t, src, cipher)

	blob, err := src.svc.Export(ctx, BackupRequest{Passphrase: testPassphrase})
	if err != nil {
		t.Fatal(err)
	}
	dst := newBackupHarness(t, cipher)
	if _, err := dst.svc.Restore(ctx, blob, RestoreRequest{Passphrase: testPassphrase}); err != nil {
		t.Fatal(err)
	}
	if got := dst.users.rows[0].Userpwd; got != "$2a$10$hashedhashedhashed" {
		t.Errorf("password hash = %q, want it carried verbatim", got)
	}
	if got := dst.mfaCodes.rows[0].CodeHash; got != "$2a$10$recoverycodehash" {
		t.Errorf("recovery code hash = %q, want it carried verbatim", got)
	}
	if got := dst.avatars.rows[0].DataB64; got != "AAAA" {
		t.Errorf("avatar payload = %q, want it carried (it is json:\"-\" on the entity)", got)
	}
	if got := dst.ssoCa.rows[0].KeyPem; got != "KEY" {
		t.Errorf("SSO CA private key = %q, want it carried", got)
	}
}

func TestExportRejectsEmptyPassphrase(t *testing.T) {
	cipher := testCipher(t)
	h := newBackupHarness(t, cipher)
	if _, err := h.svc.Export(context.Background(), BackupRequest{Passphrase: "  "}); err == nil {
		t.Fatal("an empty passphrase must be refused — it is the only protection on the file")
	}
}

func TestDecodeRejectsForeignAndCorruptFiles(t *testing.T) {
	ctx := context.Background()
	cipher := testCipher(t)
	h := newBackupHarness(t, cipher)
	seedSource(t, h, cipher)

	blob, err := h.svc.Export(ctx, BackupRequest{Passphrase: testPassphrase})
	if err != nil {
		t.Fatal(err)
	}

	// A mymatasan backup must be refused by magic before any passphrase work.
	foreign := append([]byte("MMBK"), blob[len(backupMagic):]...)
	if _, err := h.svc.Preview(ctx, foreign, testPassphrase); err == nil {
		t.Error("a non-myidsan backup file must be refused")
	} else if !strings.Contains(err.Error(), "not a myidsan backup") {
		t.Errorf("unexpected error for foreign file: %v", err)
	}

	if _, err := h.svc.Preview(ctx, blob, "wrong-passphrase-entirely"); err == nil {
		t.Error("a wrong passphrase must fail authentication")
	}

	tampered := append([]byte(nil), blob...)
	tampered[len(tampered)-1] ^= 0xFF
	if _, err := h.svc.Preview(ctx, tampered, testPassphrase); err == nil {
		t.Error("a tampered file must fail the AEAD check")
	}
}

func TestPreviewReportsManifestWithoutApplying(t *testing.T) {
	ctx := context.Background()
	cipher := testCipher(t)
	src := newBackupHarness(t, cipher)
	seedSource(t, src, cipher)

	blob, err := src.svc.Export(ctx, BackupRequest{Passphrase: testPassphrase})
	if err != nil {
		t.Fatal(err)
	}

	dst := newBackupHarness(t, cipher)
	manifest, err := dst.svc.Preview(ctx, blob, testPassphrase)
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if manifest.App != backupApp {
		t.Errorf("manifest app = %q", manifest.App)
	}
	if manifest.AppVersion != "test-1.0.0" {
		t.Errorf("manifest appVersion = %q", manifest.AppVersion)
	}
	if manifest.Counts[BackupSectionIdentity] != 1 {
		t.Errorf("identity count = %d want 1", manifest.Counts[BackupSectionIdentity])
	}
	if len(dst.users.rows) != 0 {
		t.Error("Preview must not write anything")
	}
}

// A partial restore must not leave dangling references. A redirect URI whose client was
// not restored is not merely useless — redirect URIs are the allow-list the authorize
// endpoint checks against.
func TestRestoreSkipsRowsWhoseParentIsAbsent(t *testing.T) {
	ctx := context.Background()
	cipher := testCipher(t)
	src := newBackupHarness(t, cipher)
	seedSource(t, src, cipher)

	blob, err := src.svc.Export(ctx, BackupRequest{Passphrase: testPassphrase})
	if err != nil {
		t.Fatal(err)
	}

	dst := newBackupHarness(t, cipher)
	// Restore MFA and federation WITHOUT identity or access — their parents are absent.
	res, err := dst.svc.Restore(ctx, blob, RestoreRequest{
		Passphrase: testPassphrase,
		Sections:   []string{BackupSectionMfa, BackupSectionFederation},
	})
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}

	if len(dst.mfaFactors.rows) != 0 {
		t.Error("an MFA factor was restored for a user that does not exist")
	}
	if len(dst.groupMaps.rows) != 0 {
		t.Error("a group mapping was restored pointing at a role that does not exist")
	}
	if res.Skipped[BackupSectionMfa] == 0 {
		t.Error("orphaned MFA rows should be reported as skipped, not silently dropped")
	}
	if res.Skipped[BackupSectionFederation] == 0 {
		t.Error("orphaned group mappings should be reported as skipped")
	}
}

func TestRestoreMergeModeKeepsExistingRows(t *testing.T) {
	ctx := context.Background()
	cipher := testCipher(t)
	src := newBackupHarness(t, cipher)
	seedSource(t, src, cipher)

	blob, err := src.svc.Export(ctx, BackupRequest{Passphrase: testPassphrase})
	if err != nil {
		t.Fatal(err)
	}

	dst := newBackupHarness(t, cipher)
	if _, err := dst.users.Create(ctx, "", sharedentities.UserLogin{Email: "keep-me@corp.local"}); err != nil {
		t.Fatal(err)
	}
	if _, err := dst.svc.Restore(ctx, blob, RestoreRequest{
		Passphrase: testPassphrase,
		Sections:   []string{BackupSectionAccess, BackupSectionIdentity},
		Mode:       RestoreModeMerge,
	}); err != nil {
		t.Fatal(err)
	}

	if len(dst.users.rows) != 2 {
		t.Fatalf("merge should append, got %d users", len(dst.users.rows))
	}
	var kept bool
	for _, u := range dst.users.rows {
		if u.Email == "keep-me@corp.local" {
			kept = true
		}
	}
	if !kept {
		t.Error("merge mode wiped a pre-existing account")
	}
}

// A nil cipher means encryption-at-rest is disabled; stored values are already plaintext
// and must pass through both directions untouched rather than erroring.
func TestBackupWorksWithEncryptionDisabled(t *testing.T) {
	ctx := context.Background()
	src := newBackupHarness(t, nil)
	seedSource(t, src, nil)

	blob, err := src.svc.Export(ctx, BackupRequest{Passphrase: testPassphrase})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	dst := newBackupHarness(t, nil)
	if _, err := dst.svc.Restore(ctx, blob, RestoreRequest{Passphrase: testPassphrase}); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if got := dst.mfaFactors.rows[0].SecretEnc; got != "JBSWY3DPEHPK3PXP" {
		t.Errorf("plaintext secret round-trip got %q", got)
	}
}
